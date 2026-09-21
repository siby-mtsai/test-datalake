// Command curate is the mtsai-datalake curated-layer job (brief Section 7, Phase 2 step 6): runs
// after the nightly export, copying one day's rows from the raw Iceberg table into its curated
// counterpart via a straight Athena INSERT INTO ... SELECT ... FROM. Unlike cmd/export, no staging
// table is needed - both source and destination are already-registered Iceberg tables, not fresh
// Parquet files, so Athena does the copy server-side in one statement.
//
// v1 curated is a schema-stable passthrough copy of raw, not real cleaning/dedup logic yet - see
// sql/curated/trip_events_curated.sql for the one-time table setup this job depends on already
// existing.
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

	"mtsai-datalake-export/internal/lake"
	"mtsai-datalake-export/internal/manifest"
	"mtsai-datalake-export/internal/reconcile"
)

const (
	rawTable     = "trip_events"
	curatedTable = "trip_events_curated"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("curate failed: %v", err)
	}
}

type config struct {
	Bucket          string
	RawDatabase     string
	CuratedDatabase string
	Region          string
	RunDate         time.Time
}

func loadConfig() (*config, error) {
	cfg := &config{
		Bucket:          os.Getenv("MTSAI_DATALAKE_BUCKET"),
		RawDatabase:     os.Getenv("MTSAI_DATALAKE_RAW_DATABASE"),
		CuratedDatabase: os.Getenv("MTSAI_DATALAKE_CURATED_DATABASE"),
		Region:          os.Getenv("AWS_REGION"),
	}
	if cfg.Bucket == "" || cfg.RawDatabase == "" || cfg.CuratedDatabase == "" {
		return nil, fmt.Errorf("MTSAI_DATALAKE_BUCKET, MTSAI_DATALAKE_RAW_DATABASE, and MTSAI_DATALAKE_CURATED_DATABASE are all required")
	}
	if cfg.Region == "" {
		cfg.Region = "ap-south-1"
	}

	runDate := os.Getenv("CURATE_DATE") // override for manual runs; defaults to yesterday UTC, mirroring EXPORT_DATE
	if runDate == "" {
		cfg.RunDate = time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	} else {
		d, err := time.Parse("2006-01-02", runDate)
		if err != nil {
			return nil, fmt.Errorf("CURATE_DATE must be YYYY-MM-DD, got %q: %w", runDate, err)
		}
		cfg.RunDate = d
	}
	return cfg, nil
}

func run() error {
	ctx := context.Background()
	start := time.Now()

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	dateStr := cfg.RunDate.Format("2006-01-02")

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}
	s3Client := s3.NewFromConfig(awsCfg)
	athenaClient := athena.NewFromConfig(awsCfg)

	runID := uuid.NewString()
	log.Printf("run %s: table=%s date=%s bucket=%s raw_db=%s curated_db=%s",
		runID, curatedTable, dateStr, cfg.Bucket, cfg.RawDatabase, cfg.CuratedDatabase)

	runner := &lake.AthenaRunner{
		Client:         athenaClient,
		Database:       cfg.CuratedDatabase,
		OutputLocation: fmt.Sprintf("s3://%s/athena-results/export/", cfg.Bucket),
	}

	m := manifest.Manifest{
		Table:       curatedTable,
		RunDate:     dateStr,
		RunID:       runID,
		GeneratedAt: time.Now().UTC(),
	}

	// Raw's count/checksum first - the source of truth to reconcile the curated copy against.
	rawCount, rawSum, err := countAndChecksum(ctx, runner, fmt.Sprintf("%s.%s", cfg.RawDatabase, rawTable), dateStr)
	if err != nil {
		return fmt.Errorf("raw reconciliation query: %w", err)
	}
	log.Printf("raw has %d row(s) for %s", rawCount, dateStr)

	if commitErr := commitToCurated(ctx, runner, cfg.RawDatabase, dateStr); commitErr != nil {
		m.DurationSeconds = time.Since(start).Seconds()
		m.Success = false
		m.Error = commitErr.Error()
		if upErr := manifest.Upload(ctx, s3Client, cfg.Bucket, m); upErr != nil {
			log.Printf("warning: failed to upload failure manifest: %v", upErr)
		}
		return fmt.Errorf("commit to curated: %w", commitErr)
	}

	curatedCount, curatedSum, err := countAndChecksum(ctx, runner, curatedTable, dateStr)
	if err != nil {
		return fmt.Errorf("curated reconciliation query: %w", err)
	}

	// Reusing reconcile.Result's two-sided count+checksum shape for a raw-vs-curated compare, not
	// its original postgres-vs-lake meaning - the field names are a mismatch here, but adding a
	// parallel type for what's structurally the same comparison isn't worth it for one table.
	rec := reconcile.Result{
		PostgresRowCount: rawCount,
		PostgresChecksum: rawSum,
		LakeRowCount:     curatedCount,
		LakeChecksum:     curatedSum,
	}

	m.RowCount = curatedCount
	m.Checksum = curatedSum
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

	log.Printf("run %s succeeded: %d row(s), checksum=%d, duration=%.2fs", runID, curatedCount, curatedSum, m.DurationSeconds)
	return nil
}

func countAndChecksum(ctx context.Context, runner *lake.AthenaRunner, qualifiedTable, dateStr string) (count, checksum int64, err error) {
	countStr, err := runner.RunAndFetchSingleValue(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE event_date = DATE '%s'`, qualifiedTable, dateStr))
	if err != nil {
		return 0, 0, fmt.Errorf("count query: %w", err)
	}
	sumStr, err := runner.RunAndFetchSingleValue(ctx,
		fmt.Sprintf(`SELECT COALESCE(SUM(trip_id), 0) FROM %s WHERE event_date = DATE '%s'`, qualifiedTable, dateStr))
	if err != nil {
		return 0, 0, fmt.Errorf("checksum query: %w", err)
	}
	count, err = strconv.ParseInt(countStr, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing row count %q: %w", countStr, err)
	}
	checksum, err = strconv.ParseInt(sumStr, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing checksum %q: %w", sumStr, err)
	}
	return count, checksum, nil
}

// commitToCurated mirrors internal/lake.CommitToIceberg's idempotency pattern (DELETE existing
// partition, then INSERT) but without a staging table: both sides are already-registered Iceberg
// tables, so a cross-database INSERT INTO ... SELECT ... FROM does the copy in one statement.
func commitToCurated(ctx context.Context, runner *lake.AthenaRunner, rawDatabase, dateStr string) error {
	del := fmt.Sprintf(`DELETE FROM %s WHERE event_date = DATE '%s'`, curatedTable, dateStr)
	if _, err := runner.Run(ctx, del); err != nil {
		return fmt.Errorf("delete existing curated partition for idempotency: %w", err)
	}

	insert := fmt.Sprintf(
		`INSERT INTO %s SELECT trip_id, vehicle_id_hash, city_code, event_date, distance_km, fare_amount, created_at `+
			`FROM %s.%s WHERE event_date = DATE '%s'`,
		curatedTable, rawDatabase, rawTable, dateStr,
	)
	if _, err := runner.Run(ctx, insert); err != nil {
		return fmt.Errorf("insert into curated table: %w", err)
	}
	return nil
}
