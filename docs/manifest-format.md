# Manifest format

Per brief Section 6 ("every export run writes a manifest... this is the evidence trail the audit
service can reference") and Section 7 Phase 3 step 3 ("wire export manifests into what the audit
service expects, or document the manifest format for it"). No real audit service integration
point exists yet, so this document is the fallback the brief itself names.

All four manifest types are plain JSON objects written to S3 under `export-manifests/`, produced
by `export/internal/manifest/manifest.go` — that file is the source of truth if this doc and the
code ever disagree. Every field below is real and taken from a manifest actually produced this
project, not a hypothetical schema.

## Per-run manifest (`Manifest`)

One per `(table, event_date)` combination, written by `cmd/export` after every raw commit and by
`cmd/curate` after every curated commit — including a run inside a backfill's per-date loop.

**Path**: `export-manifests/{run_date}/{table}.json`

**Example** (`export-manifests/2026-07-07/trip_events.json`):

```json
{
  "table": "trip_events",
  "run_date": "2026-07-07",
  "run_id": "04e7c6e0-9744-40af-88c9-cdebbe52a2e5",
  "row_count": 76,
  "checksum": 177962,
  "duration_seconds": 13.797886907,
  "generated_at": "2026-09-21T08:31:04.776823748Z",
  "success": true
}
```

| Field | Type | Meaning |
|---|---|---|
| `table` | string | Table name (`trip_events` or `trip_events_curated`) |
| `run_date` | string (`YYYY-MM-DD`) | The `event_date` this run covers |
| `run_id` | string (UUID) | Unique per run, matches the staging-table suffix and CloudWatch Logs `run <run_id>: ...` lines |
| `row_count` | integer | Rows in the destination table for this date, after commit |
| `checksum` | integer | `SUM(trip_id)` over those rows — a cheap, deterministic cross-check, not a cryptographic hash (see `internal/reconcile/reconcile.go`'s own comment for why) |
| `duration_seconds` | float | Wall-clock time for this run |
| `generated_at` | string (RFC 3339, UTC) | When the manifest was written |
| `success` | boolean | Whether reconciliation (raw) or the raw-vs-curated count/checksum compare (curated) matched |
| `error` | string, omitted if empty | Present only on failure — the reconciliation mismatch or commit error |

A failed run still writes a manifest (`success: false`, `error` populated) — see
`cmd/export/main.go`'s `runOnce`, which uploads a manifest even when the Iceberg commit itself
fails, so a failure is always recorded, not just silently retried.

## Backfill summary (`BackfillSummary`)

Written once per backfill invocation (`EXPORT_START_DATE`/`EXPORT_END_DATE`), in addition to — not
instead of — each individual date's normal `Manifest` above.

**Path**: `export-manifests/backfill/{start_date}_{end_date}/summary.json`

**Example** (`export-manifests/backfill/2026-05-20_2026-06-18/summary.json`, truncated to two of
the thirty `dates` entries):

```json
{
  "start_date": "2026-05-20",
  "end_date": "2026-06-18",
  "run_id": "53a1ac2f-e99c-4124-b36f-c9dd46e753b7",
  "dates": [
    { "date": "2026-05-20", "row_count": 61, "checksum": 1189652, "duration_seconds": 8.91, "success": true },
    { "date": "2026-05-21", "row_count": 74, "checksum": 1497374, "duration_seconds": 9.34, "success": true }
  ],
  "total_rows": 3198,
  "total_duration_seconds": 258.41,
  "success_count": 30,
  "failure_count": 0,
  "generated_at": "2026-09-21T08:55:10Z"
}
```

| Field | Type | Meaning |
|---|---|---|
| `start_date` / `end_date` | string (`YYYY-MM-DD`) | The inclusive range requested |
| `run_id` | string (UUID) | Unique to the whole backfill invocation (distinct from each date's own `run_id`) |
| `dates` | array | One entry per date, same shape as a per-run manifest's core fields, plus `error` if that date failed |
| `total_rows` | integer | Sum of `row_count` across all dates |
| `total_duration_seconds` | float | Sum of each date's own duration — "record throughput" per the brief |
| `success_count` / `failure_count` | integer | One date's failure doesn't abort the rest of the range (see `runbooks/backfill.md`) |
| `generated_at` | string (RFC 3339, UTC) | When the summary was written |

## Erasure manifest (`ErasureManifest`)

Written twice per erasure request: once immediately on receipt (before any `DELETE` runs, per
`runbooks/erasure.md` step 1), and again on completion with full results.

**Path**: `export-manifests/erasure/{date}/{request_ref}.json` (`date` is the erasure's own start
date, not an `event_date` — an erasure isn't scoped to one date, it removes an identifier
everywhere it appears)

**Example** (`export-manifests/erasure/2026-09-21/rehearsal-2026-09-21-004.json`, a real rehearsal
— see `runbooks/erasure.md`'s rehearsal log):

```json
{
  "request_ref": "rehearsal-2026-09-21-004",
  "identifier": "hash_veh_000067",
  "jurisdiction": "IN",
  "tables": [
    {
      "database": "test_raw",
      "table": "trip_events",
      "rows_before": 10,
      "rows_after_delete": 0,
      "pre_erasure_snapshot_id": "3011481733055550922",
      "time_travel_blocked": true,
      "success": true
    },
    {
      "database": "test_curated",
      "table": "trip_events_curated",
      "rows_before": 1,
      "rows_after_delete": 0,
      "pre_erasure_snapshot_id": "2915178321408864835",
      "time_travel_blocked": true,
      "success": true
    }
  ],
  "started_at": "2026-09-21T09:45:09.1217318Z",
  "finished_at": "2026-09-21T09:45:37.1550889Z",
  "duration_seconds": 28.0333571,
  "success": true
}
```

| Field | Type | Meaning |
|---|---|---|
| `request_ref` | string | The identity service's request reference — also the filename |
| `identifier` | string | The blind index / hashed identifier being erased (never a plaintext identifier — brief Section 5) |
| `jurisdiction` | string, omitted if empty | Recorded as request metadata; not currently used to scope the `DELETE` itself — no `jurisdiction` column exists on any table yet (see `docs/classification-table.md`'s caveat that this rule is aspirational, not implemented) |
| `tables` | array | One entry per table erasure was attempted on |
| `tables[].rows_before` / `rows_after_delete` | integer | Row count for this identifier, before and after |
| `tables[].pre_erasure_snapshot_id` | string | The Iceberg snapshot ID captured immediately before the `DELETE`, used for the time-travel check |
| `tables[].time_travel_blocked` | boolean | Whether a query against `pre_erasure_snapshot_id` failed after `VACUUM` — `true` is the desired/correct outcome |
| `tables[].success` | boolean | All of: 0 rows remaining, time travel blocked, no errors |
| `started_at` / `finished_at` | string (RFC 3339, UTC) | `finished_at` is absent on the first (pre-DELETE) write of this manifest |
| `duration_seconds` | float | End-to-end time (brief step 1: "measure end-to-end erasure time for a single identifier") |
| `success` | boolean | AND of every table's `success` |

## Trim summary (`TrimSummary`)

Written once per `cmd/trim` run (brief Phase 4 step 2: "drop Postgres partitions older than the
retention window only after reconciliation for that range has passed and the manifest is in S3").
See `runbooks/postgres-partitioning.md` for the full mechanism.

**Path**: `export-manifests/trim/{run_date}/summary.json`

**Example**:

```json
{
  "run_date": "2026-09-28",
  "run_id": "8f2a5e6d-1b3c-4a9e-9d2f-6c7b8a9e0f1d",
  "retention_days": 90,
  "partitions_created": ["trip_events_y2026_m09_d29", "trip_events_y2026_m09_d30"],
  "partitions_dropped": [
    { "date": "2025-09-30", "row_count": 61 }
  ],
  "partitions_skipped": [
    { "date": "2025-10-01", "reason": "no export manifest found in S3" }
  ],
  "duration_seconds": 4.21,
  "generated_at": "2026-09-28T04:00:12Z",
  "success": true
}
```

| Field | Type | Meaning |
|---|---|---|
| `run_date` | string (`YYYY-MM-DD`) | The date this trim run executed, in UTC |
| `run_id` | string (UUID) | Unique per run |
| `retention_days` | integer | The retention window enforced this run (`TRIM_RETENTION_DAYS`) |
| `partitions_created` | array of strings | Future daily partitions newly created this run (already-existing ones aren't listed) |
| `partitions_dropped` | array | Partitions actually dropped — `date`, and `row_count` captured immediately before the `DROP TABLE` |
| `partitions_skipped` | array | Partitions old enough to be eligible but left alone — `date` and `reason` (`"no export manifest found in S3"`, `"manifest reports success=false"`, or a parse error) |
| `duration_seconds` | float | Wall-clock time for this run |
| `generated_at` | string (RFC 3339, UTC) | When the summary was written |
| `success` | boolean | `false` only if the run itself failed (e.g. couldn't connect to Postgres) — a normal run with skipped partitions is still `success: true` |
