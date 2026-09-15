// Command export is the mtsai-datalake export job (brief Section 7, Phase 2): reads one table
// for one event_date from Postgres, writes it to S3 as Parquet, commits it into the matching
// Iceberg table via Athena, reconciles row counts/checksums, and writes a manifest.
//
// v1 slice: handles exactly one table (trip_events, a synthetic fixture - see
// internal/model/tripevent.go for why). Table-list config (internal/config/tables.yaml) is read
// and validated even though only trip_events is wired up end-to-end, so the shape is already
// right for a second real table once Phase 0 discovery identifies one.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	"mtsai-datalake-export/internal/config"
	"mtsai-datalake-export/internal/lake"
	"mtsai-datalake-export/internal/manifest"
	"mtsai-datalake-export/internal/parquetw"
	"mtsai-datalake-export/internal/reconcile"
	"mtsai-datalake-export/internal/source"
)

// Hive DDL types (this is CREATE EXTERNAL TABLE, not a Trino WITH-clause CTAS - see
// internal/lake/iceberg.go for why), confirmed against real Athena: "string", not "varchar".
const tripEventsDDLColumns = "trip_id bigint, vehicle_id_hash string, city_code string, " +
	"event_date date, distance_km double, fare_amount double, created_at timestamp"

func main() {
	if err := run(); err != nil {
		log.Fatalf("export failed: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	start := time.Now()

	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	log.Printf("loaded config: %d table(s) defined, exporting date %s", len(cfg.Tables), cfg.RunDate.Format("2006-01-02"))

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}
	s3Client := s3.NewFromConfig(awsCfg)
	athenaClient := athena.NewFromConfig(awsCfg)

	runID := uuid.NewString()
	dateStr := cfg.RunDate.Format("2006-01-02")

	log.Printf("run %s: table=trip_events date=%s bucket=%s raw_db=%s", runID, dateStr, cfg.Bucket, cfg.RawDatabase)

	pgConn, err := source.Connect(ctx, cfg.Postgres)
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer pgConn.Close(ctx)

	rows, err := source.StreamTripEvents(ctx, pgConn, cfg.RunDate, 1000)
	if err != nil {
		return fmt.Errorf("streaming trip_events: %w", err)
	}
	log.Printf("read %d row(s) from postgres for %s", len(rows), dateStr)

	var pgChecksum int64
	for _, r := range rows {
		pgChecksum += r.TripID
	}

	tmpFile, err := os.CreateTemp("", "trip_events-*.parquet")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if err := parquetw.WriteTripEvents(tmpPath, rows); err != nil {
		return fmt.Errorf("writing parquet: %w", err)
	}

	stagingKey := fmt.Sprintf("raw/_staging/trip_events/%s/data.parquet", runID)
	if err := lake.UploadFile(ctx, s3Client, cfg.Bucket, stagingKey, tmpPath); err != nil {
		return fmt.Errorf("uploading to s3: %w", err)
	}
	log.Printf("uploaded %d row(s) to s3://%s/%s", len(rows), cfg.Bucket, stagingKey)

	runner := &lake.AthenaRunner{
		Client:         athenaClient,
		Database:       cfg.RawDatabase,
		OutputLocation: fmt.Sprintf("s3://%s/athena-results/export/", cfg.Bucket),
	}

	m := manifest.Manifest{
		Table:       "trip_events",
		RunDate:     dateStr,
		RunID:       runID,
		GeneratedAt: time.Now().UTC(),
	}

	commitErr := lake.CommitToIceberg(ctx, runner, lake.CommitInput{
		Table:         "trip_events",
		RunID:         runID,
		StagingS3Path: fmt.Sprintf("s3://%s/raw/_staging/trip_events/%s/", cfg.Bucket, runID),
		RunDate:       cfg.RunDate,
		DDLColumns:    tripEventsDDLColumns,
	})
	if commitErr != nil {
		m.DurationSeconds = time.Since(start).Seconds()
		m.Success = false
		m.Error = commitErr.Error()
		if upErr := manifest.Upload(ctx, s3Client, cfg.Bucket, m); upErr != nil {
			log.Printf("warning: failed to upload failure manifest: %v", upErr)
		}
		return fmt.Errorf("commit to iceberg: %w", commitErr)
	}

	lakeCountStr, err := runner.RunAndFetchSingleValue(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM trip_events WHERE event_date = DATE '%s'`, dateStr))
	if err != nil {
		return fmt.Errorf("reconciliation count query: %w", err)
	}
	lakeSumStr, err := runner.RunAndFetchSingleValue(ctx,
		fmt.Sprintf(`SELECT COALESCE(SUM(trip_id), 0) FROM trip_events WHERE event_date = DATE '%s'`, dateStr))
	if err != nil {
		return fmt.Errorf("reconciliation checksum query: %w", err)
	}

	lakeCount, err := strconv.ParseInt(lakeCountStr, 10, 64)
	if err != nil {
		return fmt.Errorf("parsing lake row count %q: %w", lakeCountStr, err)
	}
	lakeSum, err := strconv.ParseInt(lakeSumStr, 10, 64)
	if err != nil {
		return fmt.Errorf("parsing lake checksum %q: %w", lakeSumStr, err)
	}

	rec := reconcile.Result{
		PostgresRowCount: int64(len(rows)),
		PostgresChecksum: pgChecksum,
		LakeRowCount:     lakeCount,
		LakeChecksum:     lakeSum,
	}

	m.RowCount = lakeCount
	m.Checksum = lakeSum
	m.DurationSeconds = time.Since(start).Seconds()
	m.Success = rec.Match()
	if recErr := rec.Error(); recErr != nil {
		m.Error = recErr.Error()
	}

	if err := manifest.Upload(ctx, s3Client, cfg.Bucket, m); err != nil {
		log.Printf("warning: failed to upload manifest: %v", err)
	}

	if err := rec.Error(); err != nil {
		return err
	}

	log.Printf("run %s succeeded: %d row(s), checksum=%d, duration=%.2fs", runID, lakeCount, lakeSum, m.DurationSeconds)
	return nil
}
