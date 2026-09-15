# export

Phase 2 (v1 slice) of the brief: reads one table for one `event_date` from Postgres, writes it to
S3 as Parquet, commits it into the matching Iceberg table via Athena, reconciles row
counts/checksums between Postgres and the lake, and writes a manifest. See
`docs/STATUS.md` for what's actually been run and verified.

**Handles exactly one table right now**: `trip_events`, a synthetic fixture (see
[`internal/model/tripevent.go`](internal/model/tripevent.go)) standing in for a real `mtsai-api`
table until Phase 0 discovery identifies one. `internal/config/tables.yaml` is still read and
validated as a genuine table-list config — adding a second real table means adding its own typed
struct + reader/writer wiring, not rewriting the config format.

## Running the integration test locally

No real Postgres access needed — this runs against a local, Docker-based Postgres seeded with
synthetic fixture data (matches the brief's Section 8 testing convention), while the AWS-side
calls (S3, Glue, Athena) go to whatever AWS account your current session is authenticated
against. Point it at Test, not Dev.

```powershell
# 1. start local Postgres with the fixture data
docker compose -f docker-compose.test.yml up -d

# 2. build
go build ./...

# 3. run - POSTGRES_CREDENTIALS matches the JSON shape terraform/modules/export-task injects
#    from Secrets Manager in production; here it's typed by hand for the local container.
$env:MTSAI_DATALAKE_ENV = "test"
$env:MTSAI_DATALAKE_BUCKET = "mtsai-datalake-test-690293068614-ap-south-1"
$env:MTSAI_DATALAKE_RAW_DATABASE = "test_raw"
$env:CONFIG_PATH = "internal/config/tables.yaml"
$env:EXPORT_DATE = "2026-01-15"  # matches the fixture data's event_date
$env:POSTGRES_CREDENTIALS = '{"host":"localhost","port":55432,"dbname":"mtsai_export_test","username":"export_test","password":"export_test"}'
./export.exe   # or `go run ./cmd/export`

# 4. tear down
docker compose -f docker-compose.test.yml down
```

Re-running step 3 with the same `EXPORT_DATE` should produce the same row count in the lake, not
double it (idempotency via `DELETE` + `INSERT` on that partition).

## Config contract with Terraform

`terraform/modules/export-task/main.tf` injects exactly these into the container — the job fails
fast at startup if any are missing:

| Env var | Source |
|---|---|
| `MTSAI_DATALAKE_ENV` | `var.environment` |
| `MTSAI_DATALAKE_BUCKET` | `var.bucket_name` |
| `MTSAI_DATALAKE_RAW_DATABASE` | `var.raw_database_name` |
| `POSTGRES_CREDENTIALS` (secret) | `var.postgres_secret_arn` (Secrets Manager, RDS-credential JSON shape) |

`CONFIG_PATH` defaults to `/etc/mtsai-datalake-export/tables.yaml`, baked into the container image
by the `Dockerfile`. `EXPORT_DATE` (optional, `YYYY-MM-DD`) overrides the default of "yesterday,
UTC" — used for manual/backfill runs.

## Explicitly out of scope for this slice

- Pushing the image to ECR / updating `export-task`'s `container_image` variable to a real image.
- The real nightly EventBridge schedule actually triggering it (still blocked on VPC subnet IDs).
- Backfill-mode / multi-date-range CLI.
- Curated-layer CTAS automation (brief Phase 2 step 6).
- Handling more than one table.
