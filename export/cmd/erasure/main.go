// Command erasure implements runbooks/erasure.md's five-step procedure (brief Section 9): for a
// verified erasure request, delete every row matching an identifier from every personal-data
// table, expire the Iceberg snapshots that would otherwise let time travel resurface them, and
// verify both. Unlike cmd/export/cmd/curate/cmd/compact, this is never scheduled - Section 9's
// trigger is an external verified request, not a clock, so it stays manual-invoke-only via
// `aws ecs run-task`.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"mtsai-datalake-export/internal/lake"
	"mtsai-datalake-export/internal/manifest"
)

// erasureTarget is one table to erase the identifier from. Hardcoded to the two tables actually
// built (trip_events, trip_events_curated) - the classification table also names accounts,
// anpr_camera_events, and reward_ledger as personal-data-bearing, but none of those exist in the
// lake yet, so there's nothing there to erase.
type erasureTarget struct {
	database string
	table    string
	idColumn string
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("erasure failed: %v", err)
	}
}

// Input now arrives from a UI (the mtsai-analytics Erase feature) via ecs:RunTask environment
// overrides, not just from an operator's CLI. The identifier is interpolated straight into Athena
// SQL and the request ref becomes an S3 key, so both are allow-listed rather than escaped.
var (
	identifierPattern   = regexp.MustCompile(`^[A-Za-z0-9_]{1,128}$`)
	requestRefPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]{3,64}$`)
	jurisdictionPattern = regexp.MustCompile(`^[A-Z]{2}$`)
)

type config struct {
	Bucket          string
	RawDatabase     string
	CuratedDatabase string
	Region          string
	WorkGroup       string
	RequestRef      string
	Identifier      string
	Jurisdiction    string
}

func loadConfig() (*config, error) {
	cfg := &config{
		Bucket:          os.Getenv("MTSAI_DATALAKE_BUCKET"),
		RawDatabase:     os.Getenv("MTSAI_DATALAKE_RAW_DATABASE"),
		CuratedDatabase: os.Getenv("MTSAI_DATALAKE_CURATED_DATABASE"),
		Region:          os.Getenv("AWS_REGION"),
		WorkGroup:       os.Getenv("MTSAI_DATALAKE_WORKGROUP"),
		RequestRef:      os.Getenv("ERASURE_REQUEST_REF"),
		Identifier:      os.Getenv("ERASURE_IDENTIFIER"),
		Jurisdiction:    os.Getenv("ERASURE_JURISDICTION"),
	}
	if cfg.Bucket == "" || cfg.RawDatabase == "" || cfg.CuratedDatabase == "" {
		return nil, fmt.Errorf("MTSAI_DATALAKE_BUCKET, MTSAI_DATALAKE_RAW_DATABASE, and MTSAI_DATALAKE_CURATED_DATABASE are all required")
	}
	// Deliberately no default for RequestRef/Identifier - unlike EXPORT_DATE this is a compliance
	// action; silently defaulting either would be exactly the wrong instinct.
	if cfg.RequestRef == "" || cfg.Identifier == "" {
		return nil, fmt.Errorf("ERASURE_REQUEST_REF and ERASURE_IDENTIFIER are both required")
	}
	if !identifierPattern.MatchString(cfg.Identifier) {
		return nil, fmt.Errorf("ERASURE_IDENTIFIER must match %s", identifierPattern)
	}
	if !requestRefPattern.MatchString(cfg.RequestRef) {
		return nil, fmt.Errorf("ERASURE_REQUEST_REF must match %s", requestRefPattern)
	}
	if cfg.Jurisdiction != "" && !jurisdictionPattern.MatchString(cfg.Jurisdiction) {
		return nil, fmt.Errorf("ERASURE_JURISDICTION must match %s when set", jurisdictionPattern)
	}
	if cfg.Region == "" {
		cfg.Region = "ap-south-1"
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

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}
	s3Client := s3.NewFromConfig(awsCfg)
	athenaClient := athena.NewFromConfig(awsCfg)

	m := manifest.ErasureManifest{
		RequestRef:   cfg.RequestRef,
		Identifier:   cfg.Identifier,
		Jurisdiction: cfg.Jurisdiction,
		StartedAt:    start.UTC(),
	}

	// Step 1: record the request reference before touching any data.
	if err := manifest.UploadErasureManifest(ctx, s3Client, cfg.Bucket, m); err != nil {
		return fmt.Errorf("recording erasure request: %w", err)
	}
	log.Printf("erasure %s: identifier=%s jurisdiction=%s recorded, starting", cfg.RequestRef, cfg.Identifier, cfg.Jurisdiction)

	targets := []erasureTarget{
		{database: cfg.RawDatabase, table: "trip_events", idColumn: "vehicle_id_hash"},
		{database: cfg.CuratedDatabase, table: "trip_events_curated", idColumn: "vehicle_id_hash"},
	}

	overallSuccess := true
	for _, t := range targets {
		res := eraseFromTable(ctx, athenaClient, cfg.Bucket, cfg.WorkGroup, cfg.Identifier, t)
		if !res.Success {
			overallSuccess = false
		}
		m.Tables = append(m.Tables, res)
	}

	m.FinishedAt = time.Now().UTC()
	m.DurationSeconds = time.Since(start).Seconds()
	m.Success = overallSuccess

	// Step 5: record final results against the request reference.
	if err := manifest.UploadErasureManifest(ctx, s3Client, cfg.Bucket, m); err != nil {
		log.Printf("warning: failed to upload final erasure manifest: %v", err)
	}

	if !overallSuccess {
		return fmt.Errorf("erasure %s did not fully succeed - see export-manifests/erasure/%s/%s.json",
			cfg.RequestRef, m.StartedAt.Format("2006-01-02"), cfg.RequestRef)
	}

	log.Printf("erasure %s succeeded: %d table(s), duration=%.2fs", cfg.RequestRef, len(targets), m.DurationSeconds)
	return nil
}

// eraseFromTable implements runbooks/erasure.md steps 2-4 for one table: DELETE, capture the
// pre-erasure snapshot, VACUUM with tight retention, then verify both that the identifier is gone
// from the live table and that the pre-erasure snapshot no longer resolves via time travel.
func eraseFromTable(ctx context.Context, athenaClient *athena.Client, bucket, workGroup, identifier string, t erasureTarget) manifest.TableErasureResult {
	res := manifest.TableErasureResult{Database: t.database, Table: t.table}

	runner := &lake.AthenaRunner{
		Client:         athenaClient,
		Database:       t.database,
		OutputLocation: fmt.Sprintf("s3://%s/athena-results/export/", bucket),
		WorkGroup:      workGroup,
	}

	rowsBefore, err := countByIdentifier(ctx, runner, t.table, t.idColumn, identifier)
	if err != nil {
		res.Error = fmt.Sprintf("counting rows before erasure: %v", err)
		return res
	}
	res.RowsBefore = rowsBefore
	log.Printf("%s.%s: %d row(s) for identifier %s before erasure", t.database, t.table, rowsBefore, identifier)

	// Nothing to erase here - a real, successful outcome (e.g. an identifier that only exists in
	// raw so far, not yet curated), not a failure. A DELETE matching zero rows doesn't create a
	// new Iceberg snapshot, so the time-travel check below would otherwise wrongly flag the
	// unchanged, still-resolving current snapshot as "not blocked" even though nothing was ever
	// deleted for it to hide. Found this exact case rehearsing against real backfilled data.
	if rowsBefore == 0 {
		res.Success = true
		log.Printf("%s.%s: no rows for identifier %s, nothing to erase", t.database, t.table, identifier)
		return res
	}

	// Capture the pre-erasure snapshot before DELETE changes it - this is what step 4's time
	// travel check attempts to query against afterward.
	snapshotID, err := latestSnapshotID(ctx, runner, t.table)
	if err != nil {
		res.Error = fmt.Sprintf("capturing pre-erasure snapshot: %v", err)
		return res
	}
	res.PreErasureSnapshotID = snapshotID

	// Step 2: delete.
	del := fmt.Sprintf(`DELETE FROM %s WHERE %s = '%s'`, t.table, t.idColumn, identifier)
	if _, err := runner.Run(ctx, del); err != nil {
		res.Error = fmt.Sprintf("delete: %v", err)
		return res
	}

	// Step 3: expire snapshots + remove orphan files so time travel can't resurface the deleted
	// rows ("retention of zero" per the runbook) - tighten the table's vacuum properties first,
	// since VACUUM's own aggressiveness is governed by these, not a per-statement argument.
	// vacuum_max_snapshot_age_seconds=0 is rejected by Athena ("must be in inclusive range [60,
	// ...]", confirmed empirically) - 60 is the practical floor, as close to zero as this property
	// allows.
	if _, err := runner.Run(ctx, fmt.Sprintf(
		`ALTER TABLE %s SET TBLPROPERTIES ('vacuum_min_snapshots_to_keep'='1', 'vacuum_max_snapshot_age_seconds'='60')`,
		t.table)); err != nil {
		res.Error = fmt.Sprintf("setting vacuum retention: %v", err)
		return res
	}
	if _, err := runner.Run(ctx, fmt.Sprintf("VACUUM %s", t.table)); err != nil {
		res.Error = fmt.Sprintf("vacuum: %v", err)
		return res
	}

	// Step 4a: verify the identifier is gone from the live table.
	rowsAfter, err := countByIdentifier(ctx, runner, t.table, t.idColumn, identifier)
	if err != nil {
		res.Error = fmt.Sprintf("counting rows after erasure: %v", err)
		return res
	}
	res.RowsAfterDelete = rowsAfter
	if rowsAfter != 0 {
		res.Error = fmt.Sprintf("expected 0 rows after delete, got %d", rowsAfter)
		return res
	}

	// Step 4b: verify the pre-erasure snapshot itself no longer resolves - a query against it
	// should fail now that VACUUM has expired it, not just return 0 matching rows (which would
	// only prove the DELETE worked, not that time travel is actually blocked).
	_, ttErr := runner.Run(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s FOR VERSION AS OF %s`, t.table, snapshotID))
	res.TimeTravelBlocked = ttErr != nil
	if ttErr != nil {
		log.Printf("%s.%s: pre-erasure snapshot %s no longer resolves (expected): %v", t.database, t.table, snapshotID, ttErr)
	} else {
		res.Error = fmt.Sprintf("pre-erasure snapshot %s still resolves after vacuum - time travel not blocked", snapshotID)
		return res
	}

	res.Success = true
	log.Printf("%s.%s: erased, verified 0 rows and blocked time travel", t.database, t.table)
	return res
}

func countByIdentifier(ctx context.Context, runner *lake.AthenaRunner, table, idColumn, identifier string) (int64, error) {
	countStr, err := runner.RunAndFetchSingleValue(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s = '%s'`, table, idColumn, identifier))
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(countStr, 10, 64)
}

func latestSnapshotID(ctx context.Context, runner *lake.AthenaRunner, table string) (string, error) {
	return runner.RunAndFetchSingleValue(ctx,
		fmt.Sprintf(`SELECT snapshot_id FROM "%s$snapshots" ORDER BY committed_at DESC LIMIT 1`, table))
}
