// Command trim is the mtsai-datalake weekly Postgres hygiene job (brief Section 7, Phase 4 steps
// 1-2): keeps trip_events' near-future daily partitions pre-created, and drops partitions older
// than the retention window once - and only once - S3 has a manifest confirming that exact date's
// export reconciled successfully. Never drops on missing or failing evidence; a date without a
// manifest, or with one that reports success=false, is skipped and logged, left for a later run.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mtsai-datalake-export/internal/config"
	"mtsai-datalake-export/internal/lake"
	"mtsai-datalake-export/internal/manifest"
	"mtsai-datalake-export/internal/source"
)

var partitionNamePattern = regexp.MustCompile(`^trip_events_y(\d{4})_m(\d{2})_d(\d{2})$`)

func partitionName(d time.Time) string {
	return fmt.Sprintf("trip_events_y%04d_m%02d_d%02d", d.Year(), int(d.Month()), d.Day())
}

// parsePartitionDate only matches the daily trip_events_yYYYY_mMM_dDD naming scheme this job and
// sql/postgres/partition_trip_events.sql both use - trip_events_default (and anything else) is
// deliberately not a match, so it's never a trim candidate.
func parsePartitionDate(name string) (time.Time, bool) {
	m := partitionNamePattern.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	year, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), true
}

type trimConfig struct {
	Bucket        string
	Postgres      config.PostgresCredentials
	Region        string
	RetentionDays int
	LookaheadDays int
}

func loadConfig() (*trimConfig, error) {
	cfg := &trimConfig{
		Bucket: os.Getenv("MTSAI_DATALAKE_BUCKET"),
		Region: os.Getenv("AWS_REGION"),
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("MTSAI_DATALAKE_BUCKET is required")
	}
	if cfg.Region == "" {
		cfg.Region = "ap-south-1"
	}

	rawCreds := os.Getenv("POSTGRES_CREDENTIALS")
	if rawCreds == "" {
		return nil, fmt.Errorf("POSTGRES_CREDENTIALS is required")
	}
	if err := json.Unmarshal([]byte(rawCreds), &cfg.Postgres); err != nil {
		return nil, fmt.Errorf("parsing POSTGRES_CREDENTIALS: %w", err)
	}

	cfg.RetentionDays = 90
	if v := os.Getenv("TRIM_RETENTION_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("TRIM_RETENTION_DAYS must be a positive integer, got %q", v)
		}
		cfg.RetentionDays = n
	}

	cfg.LookaheadDays = 14
	if v := os.Getenv("TRIM_LOOKAHEAD_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("TRIM_LOOKAHEAD_DAYS must be a positive integer, got %q", v)
		}
		cfg.LookaheadDays = n
	}

	return cfg, nil
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("trim failed: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	start := time.Now()
	runID := uuid.NewString()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	runDateStr := today.Format("2006-01-02")

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}
	s3Client := s3.NewFromConfig(awsCfg)

	pgConn, err := source.Connect(ctx, cfg.Postgres)
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer pgConn.Close(ctx)

	summary := manifest.TrimSummary{
		RunDate:       runDateStr,
		RunID:         runID,
		RetentionDays: cfg.RetentionDays,
		GeneratedAt:   time.Now().UTC(),
	}

	log.Printf("run %s: ensuring %d day(s) of future partitions, retention=%d day(s)", runID, cfg.LookaheadDays, cfg.RetentionDays)
	created, err := ensureFuturePartitions(ctx, pgConn, today, cfg.LookaheadDays)
	if err != nil {
		return finishWithError(ctx, s3Client, cfg.Bucket, &summary, start, fmt.Errorf("ensuring future partitions: %w", err))
	}
	summary.PartitionsCreated = created
	log.Printf("ensured future partitions: %d newly created", len(created))

	partitions, err := listPartitions(ctx, pgConn)
	if err != nil {
		return finishWithError(ctx, s3Client, cfg.Bucket, &summary, start, fmt.Errorf("listing partitions: %w", err))
	}

	cutoff := today.AddDate(0, 0, -cfg.RetentionDays)
	for _, name := range partitions {
		d, ok := parsePartitionDate(name)
		if !ok || !d.Before(cutoff) {
			continue // not one of our dated partitions, or not old enough yet
		}
		dateStr := d.Format("2006-01-02")

		body, found, err := lake.GetObjectBytes(ctx, s3Client, cfg.Bucket, fmt.Sprintf("export-manifests/%s/trip_events.json", dateStr))
		if err != nil {
			return finishWithError(ctx, s3Client, cfg.Bucket, &summary, start, fmt.Errorf("checking manifest for %s: %w", dateStr, err))
		}
		if !found {
			summary.PartitionsSkipped = append(summary.PartitionsSkipped, manifest.PartitionResult{Date: dateStr, Reason: "no export manifest found in S3"})
			log.Printf("skipping %s: no export manifest found in S3", dateStr)
			continue
		}
		var m manifest.Manifest
		if err := json.Unmarshal(body, &m); err != nil {
			summary.PartitionsSkipped = append(summary.PartitionsSkipped, manifest.PartitionResult{Date: dateStr, Reason: fmt.Sprintf("manifest parse error: %v", err)})
			log.Printf("skipping %s: manifest parse error: %v", dateStr, err)
			continue
		}
		if !m.Success {
			summary.PartitionsSkipped = append(summary.PartitionsSkipped, manifest.PartitionResult{Date: dateStr, Reason: "manifest reports success=false"})
			log.Printf("skipping %s: manifest reports success=false", dateStr)
			continue
		}

		rowCount, err := dropPartition(ctx, pgConn, name)
		if err != nil {
			return finishWithError(ctx, s3Client, cfg.Bucket, &summary, start, fmt.Errorf("dropping %s: %w", name, err))
		}
		summary.PartitionsDropped = append(summary.PartitionsDropped, manifest.PartitionResult{Date: dateStr, RowCount: rowCount})
		log.Printf("dropped %s: %d row(s), reconciled export manifest confirmed success", name, rowCount)
	}

	summary.DurationSeconds = time.Since(start).Seconds()
	summary.Success = true
	if err := manifest.UploadTrimSummary(ctx, s3Client, cfg.Bucket, summary); err != nil {
		log.Printf("warning: failed to upload trim summary: %v", err)
	}

	log.Printf("trim run succeeded: %d partition(s) created, %d dropped, %d skipped, duration=%.2fs",
		len(summary.PartitionsCreated), len(summary.PartitionsDropped), len(summary.PartitionsSkipped), summary.DurationSeconds)
	return nil
}

func finishWithError(ctx context.Context, s3Client *s3.Client, bucket string, summary *manifest.TrimSummary, start time.Time, runErr error) error {
	summary.DurationSeconds = time.Since(start).Seconds()
	summary.Success = false
	summary.Error = runErr.Error()
	if upErr := manifest.UploadTrimSummary(ctx, s3Client, bucket, *summary); upErr != nil {
		log.Printf("warning: failed to upload failure trim summary: %v", upErr)
	}
	return runErr
}

// ensureFuturePartitions creates any missing daily partition for today..today+lookaheadDays-1, so
// mtsai-api-sim's own inserts never fall into the trip_events_default safety-net partition in
// normal operation. Existence is checked explicitly (rather than relying on IF NOT EXISTS alone)
// so the returned list only reports partitions genuinely created this run, for an accurate manifest.
func ensureFuturePartitions(ctx context.Context, conn *pgx.Conn, today time.Time, lookaheadDays int) ([]string, error) {
	var created []string
	for i := 0; i < lookaheadDays; i++ {
		d := today.AddDate(0, 0, i)
		name := partitionName(d)

		var exists bool
		if err := conn.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
				WHERE i.inhparent = 'trip_events'::regclass AND c.relname = $1
			)
		`, name).Scan(&exists); err != nil {
			return created, fmt.Errorf("checking partition %s: %w", name, err)
		}
		if exists {
			continue
		}

		next := d.AddDate(0, 0, 1)
		ddl := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF trip_events FOR VALUES FROM ('%s') TO ('%s')`,
			name, d.Format("2006-01-02"), next.Format("2006-01-02"),
		)
		if _, err := conn.Exec(ctx, ddl); err != nil {
			return created, fmt.Errorf("creating partition %s: %w", name, err)
		}
		created = append(created, name)
	}
	return created, nil
}

// listPartitions returns every child partition of trip_events, dated or not (trip_events_default
// included) - callers filter with parsePartitionDate, which only matches the dated naming scheme.
func listPartitions(ctx context.Context, conn *pgx.Conn) ([]string, error) {
	rows, err := conn.Query(ctx, `
		SELECT c.relname
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'trip_events'::regclass
		ORDER BY c.relname
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// dropPartition counts the partition's rows immediately before dropping it (for the manifest),
// then drops it - DROP TABLE on a partition child detaches it from trip_events and deletes its
// data in one step, which is exactly what a trim is meant to do.
func dropPartition(ctx context.Context, conn *pgx.Conn, name string) (int64, error) {
	var count int64
	if err := conn.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, name)).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting %s: %w", name, err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(`DROP TABLE %s`, name)); err != nil {
		return count, fmt.Errorf("dropping %s: %w", name, err)
	}
	return count, nil
}
