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

	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}
	s3Client := s3.NewFromConfig(awsCfg)
	athenaClient := athena.NewFromConfig(awsCfg)

	if len(cfg.RunDates) > 0 {
		return runBackfill(ctx, cfg, s3Client, athenaClient)
	}

	log.Printf("loaded config: %d table(s) defined, exporting date %s", len(cfg.Tables), cfg.RunDate.Format("2006-01-02"))
	_, err = runOnce(ctx, cfg, s3Client, athenaClient, cfg.RunDate)
	return err
}

// runOnce runs the full read -> parquet -> upload -> commit -> reconcile -> manifest pipeline
// for exactly one event_date. Used directly for a normal single-date run, and once per date in
// the loop for a backfill (EXPORT_START_DATE/EXPORT_END_DATE) run.
func runOnce(ctx context.Context, cfg *config.Config, s3Client *s3.Client, athenaClient *athena.Client, date time.Time) (manifest.Manifest, error) {
	start := time.Now()
	runID := uuid.NewString()
	dateStr := date.Format("2006-01-02")

	log.Printf("run %s: table=trip_events date=%s bucket=%s raw_db=%s", runID, dateStr, cfg.Bucket, cfg.RawDatabase)

	m := manifest.Manifest{
		Table:       "trip_events",
		RunDate:     dateStr,
		RunID:       runID,
		GeneratedAt: time.Now().UTC(),
	}

	pgConn, err := source.Connect(ctx, cfg.Postgres)
	if err != nil {
		return m, fmt.Errorf("connecting to postgres: %w", err)
	}
	defer pgConn.Close(ctx)

	rows, err := source.StreamTripEvents(ctx, pgConn, date, 1000)
	if err != nil {
		return m, fmt.Errorf("streaming trip_events: %w", err)
	}
	log.Printf("read %d row(s) from postgres for %s", len(rows), dateStr)

	var pgChecksum int64
	for _, r := range rows {
		pgChecksum += r.TripID
	}

	tmpFile, err := os.CreateTemp("", "trip_events-*.parquet")
	if err != nil {
		return m, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if err := parquetw.WriteTripEvents(tmpPath, rows); err != nil {
		return m, fmt.Errorf("writing parquet: %w", err)
	}

	stagingKey := fmt.Sprintf("raw/_staging/trip_events/%s/data.parquet", runID)
	if err := lake.UploadFile(ctx, s3Client, cfg.Bucket, stagingKey, tmpPath); err != nil {
		return m, fmt.Errorf("uploading to s3: %w", err)
	}
	log.Printf("uploaded %d row(s) to s3://%s/%s", len(rows), cfg.Bucket, stagingKey)

	runner := &lake.AthenaRunner{
		Client:         athenaClient,
		Database:       cfg.RawDatabase,
		OutputLocation: fmt.Sprintf("s3://%s/athena-results/export/", cfg.Bucket),
		WorkGroup:      cfg.WorkGroup,
	}

	commitErr := lake.CommitToIceberg(ctx, runner, lake.CommitInput{
		Table:         "trip_events",
		RunID:         runID,
		StagingS3Path: fmt.Sprintf("s3://%s/raw/_staging/trip_events/%s/", cfg.Bucket, runID),
		RunDate:       date,
		DDLColumns:    tripEventsDDLColumns,
	})
	if commitErr != nil {
		m.DurationSeconds = time.Since(start).Seconds()
		m.Success = false
		m.Error = commitErr.Error()
		if upErr := manifest.Upload(ctx, s3Client, cfg.Bucket, m); upErr != nil {
			log.Printf("warning: failed to upload failure manifest: %v", upErr)
		}
		return m, fmt.Errorf("commit to iceberg: %w", commitErr)
	}

	lakeCountStr, err := runner.RunAndFetchSingleValue(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM trip_events WHERE event_date = DATE '%s'`, dateStr))
	if err != nil {
		return m, fmt.Errorf("reconciliation count query: %w", err)
	}
	lakeSumStr, err := runner.RunAndFetchSingleValue(ctx,
		fmt.Sprintf(`SELECT COALESCE(SUM(trip_id), 0) FROM trip_events WHERE event_date = DATE '%s'`, dateStr))
	if err != nil {
		return m, fmt.Errorf("reconciliation checksum query: %w", err)
	}

	lakeCount, err := strconv.ParseInt(lakeCountStr, 10, 64)
	if err != nil {
		return m, fmt.Errorf("parsing lake row count %q: %w", lakeCountStr, err)
	}
	lakeSum, err := strconv.ParseInt(lakeSumStr, 10, 64)
	if err != nil {
		return m, fmt.Errorf("parsing lake checksum %q: %w", lakeSumStr, err)
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
		return m, err
	}

	log.Printf("run %s succeeded: %d row(s), checksum=%d, duration=%.2fs", runID, lakeCount, lakeSum, m.DurationSeconds)
	return m, nil
}

// runBackfill runs runOnce once per date in cfg.RunDates (brief Phase 2 step 5: "run the job over
// the full history in date chunks. Record throughput."). A single date's failure doesn't abort the
// rest of the range - the point of a backfill is to measure the whole range's throughput, and one
// bad day shouldn't hide how every other day behaved - but the overall run still fails (non-zero
// exit, so the scheduled task's failure alarm can fire) if any date failed.
func runBackfill(ctx context.Context, cfg *config.Config, s3Client *s3.Client, athenaClient *athena.Client) error {
	backfillStart := time.Now()
	startStr := cfg.RunDates[0].Format("2006-01-02")
	endStr := cfg.RunDates[len(cfg.RunDates)-1].Format("2006-01-02")
	log.Printf("loaded config: %d table(s) defined, backfilling %s to %s (%d day(s))",
		len(cfg.Tables), startStr, endStr, len(cfg.RunDates))

	summary := manifest.BackfillSummary{
		StartDate:   startStr,
		EndDate:     endStr,
		RunID:       uuid.NewString(),
		GeneratedAt: time.Now().UTC(),
	}

	for _, date := range cfg.RunDates {
		m, runErr := runOnce(ctx, cfg, s3Client, athenaClient, date)
		dr := manifest.DateResult{
			Date:            date.Format("2006-01-02"),
			RowCount:        m.RowCount,
			Checksum:        m.Checksum,
			DurationSeconds: m.DurationSeconds,
			Success:         runErr == nil,
		}
		if runErr != nil {
			dr.Error = runErr.Error()
			summary.FailureCount++
			log.Printf("backfill date %s failed: %v", dr.Date, runErr)
		} else {
			summary.SuccessCount++
		}
		summary.TotalRows += dr.RowCount
		summary.TotalDurationSeconds += dr.DurationSeconds
		summary.Dates = append(summary.Dates, dr)
	}

	if err := manifest.UploadBackfillSummary(ctx, s3Client, cfg.Bucket, summary); err != nil {
		log.Printf("warning: failed to upload backfill summary manifest: %v", err)
	}

	log.Printf("backfill %s to %s complete: %d succeeded, %d failed, %d total row(s), %.2fs total row-processing time, %.2fs wall clock",
		startStr, endStr, summary.SuccessCount, summary.FailureCount, summary.TotalRows,
		summary.TotalDurationSeconds, time.Since(backfillStart).Seconds())

	if summary.FailureCount > 0 {
		return fmt.Errorf("backfill %s to %s: %d of %d date(s) failed", startStr, endStr, summary.FailureCount, len(cfg.RunDates))
	}
	return nil
}
