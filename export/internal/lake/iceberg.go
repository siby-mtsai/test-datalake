package lake

import (
	"context"
	"fmt"
	"time"
)

// CommitInput describes one (table, event_date) batch to commit into its Iceberg table.
type CommitInput struct {
	Table         string    // e.g. "trip_events"
	RunID         string    // unique per run, used to name the throwaway staging table
	StagingS3Path string    // s3://bucket/raw/_staging/<table>/<run_id>/ - where the Parquet file was uploaded
	RunDate       time.Time // the single event_date this batch covers
	DDLColumns    string    // Hive DDL column list, e.g. "trip_id bigint, city_code string, ..."
}

// CommitToIceberg commits a batch of freshly-uploaded Parquet data into an existing Iceberg
// table via Athena (brief Section 7, Phase 2 step 1: "commit to the Iceberg table via Athena
// (INSERT INTO...) - prefer the Athena route for simplicity in v1"). It never touches Iceberg
// metadata JSON directly:
//
//  1. Register the staging Parquet as a throwaway Hive-style external table.
//  2. DELETE any existing rows for this event_date from the real Iceberg table - this is what
//     makes a re-run of the same (table, event_date) replace rather than duplicate (brief:
//     "re-running a completed key must replace, not duplicate... use Iceberg overwrite of the
//     partition"). Trino/Athena's Iceberg connector supports row-level DELETE, which is exactly
//     why Iceberg was chosen over plain Parquet in the first place.
//  3. INSERT INTO the real table, reading from the staging table - Athena's Iceberg connector
//     handles the actual manifest/snapshot commit.
//  4. Drop the staging table (data of record now lives in the Iceberg table; the staging Parquet
//     itself is left in S3 under raw/_staging/ for now - not yet wired into a lifecycle rule).
func CommitToIceberg(ctx context.Context, runner *AthenaRunner, in CommitInput) error {
	stagingTable := fmt.Sprintf("%s_staging_%s", in.Table, in.RunID)

	createStaging := fmt.Sprintf(
		`CREATE EXTERNAL TABLE IF NOT EXISTS %s (%s) STORED AS PARQUET LOCATION '%s'`,
		stagingTable, in.DDLColumns, in.StagingS3Path,
	)
	if _, err := runner.Run(ctx, createStaging); err != nil {
		return fmt.Errorf("create staging table: %w", err)
	}
	defer func() {
		// Best-effort cleanup with a fresh context - the run's own context may already be
		// past its deadline or cancelled by the time we get here.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = runner.Run(cleanupCtx, fmt.Sprintf("DROP TABLE IF EXISTS %s", stagingTable))
	}()

	dateLit := in.RunDate.Format("2006-01-02")

	del := fmt.Sprintf(`DELETE FROM %s WHERE event_date = DATE '%s'`, in.Table, dateLit)
	if _, err := runner.Run(ctx, del); err != nil {
		return fmt.Errorf("delete existing partition for idempotency: %w", err)
	}

	insert := fmt.Sprintf(`INSERT INTO %s SELECT * FROM %s`, in.Table, stagingTable)
	if _, err := runner.Run(ctx, insert); err != nil {
		return fmt.Errorf("insert into iceberg table: %w", err)
	}

	return nil
}
