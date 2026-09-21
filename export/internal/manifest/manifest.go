// Package manifest writes the per-run evidence trail the brief calls for (Section 6: "every
// export run writes a manifest... to export-manifests/. This is the evidence trail the audit
// service can reference.").
package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"mtsai-datalake-export/internal/lake"
)

type Manifest struct {
	Table           string    `json:"table"`
	RunDate         string    `json:"run_date"`
	RunID           string    `json:"run_id"`
	RowCount        int64     `json:"row_count"`
	Checksum        int64     `json:"checksum"`
	DurationSeconds float64   `json:"duration_seconds"`
	GeneratedAt     time.Time `json:"generated_at"`
	Success         bool      `json:"success"`
	Error           string    `json:"error,omitempty"`
}

func Upload(ctx context.Context, client *s3.Client, bucket string, m Manifest) error {
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	key := fmt.Sprintf("export-manifests/%s/%s.json", m.RunDate, m.Table)
	return lake.UploadBytes(ctx, client, bucket, key, body, "application/json")
}

// DateResult is one date's outcome within a BackfillSummary (brief Phase 2 step 5: "record
// throughput"). Each date also still gets its own normal Manifest via Upload above - this is the
// aggregate view layered on top, not a replacement for the per-date evidence trail.
type DateResult struct {
	Date            string  `json:"date"`
	RowCount        int64   `json:"row_count"`
	Checksum        int64   `json:"checksum"`
	DurationSeconds float64 `json:"duration_seconds"`
	Success         bool    `json:"success"`
	Error           string  `json:"error,omitempty"`
}

type BackfillSummary struct {
	StartDate            string       `json:"start_date"`
	EndDate              string       `json:"end_date"`
	RunID                string       `json:"run_id"`
	Dates                []DateResult `json:"dates"`
	TotalRows            int64        `json:"total_rows"`
	TotalDurationSeconds float64      `json:"total_duration_seconds"`
	SuccessCount         int          `json:"success_count"`
	FailureCount         int          `json:"failure_count"`
	GeneratedAt          time.Time    `json:"generated_at"`
}

func UploadBackfillSummary(ctx context.Context, client *s3.Client, bucket string, s BackfillSummary) error {
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling backfill summary: %w", err)
	}
	key := fmt.Sprintf("export-manifests/backfill/%s_%s/summary.json", s.StartDate, s.EndDate)
	return lake.UploadBytes(ctx, client, bucket, key, body, "application/json")
}
