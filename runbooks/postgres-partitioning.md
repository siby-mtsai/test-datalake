# Postgres partitioning runbook

Per brief Section 7 Phase 4 steps 1-2: "introduce native partitioning on the largest event tables
in Postgres... so old data drops as partitions rather than row-by-row deletes" and "drop Postgres
partitions older than the retention window only after reconciliation for that range has passed and
the manifest is in S3." Scoped to `mtsai-api-sim-test`'s `trip_events` table (the only table this
project's export pipeline touches).

Two parts: a one-time migration (this document, step-by-step below) and an ongoing weekly job,
`cmd/trim` (`export/cmd/trim/main.go`), scheduled via EventBridge Scheduler
(`aws_scheduler_schedule.weekly_trim`, `cron(0 4 ? * SUN *)` UTC by default — one hour after
`weekly_compact`).

Partitioning is **daily** (one partition per `event_date`, named `trip_events_yYYYY_mMM_dDD`) —
matches the per-date granularity `export-manifests/{event_date}/trip_events.json` already
reconciles at, so the trim gate is a direct per-date manifest lookup. Retention is **90 days**,
matching `mtsai-api-sim`'s own "recent" seed window.

## Part 1: One-time migration

SQL: `sql/postgres/partition_trip_events.sql`. Run once, by hand, via `psql` against
`mtsai-api-sim-test`. Postgres can't `ALTER TABLE ... PARTITION BY` in place on a table with
existing data, so this creates a new partitioned table, copies the data across, and swaps it in
under the original name.

```bash
psql "postgresql://<user>:<password>@<endpoint>:5432/mtsai_api_sim?sslmode=require" \
  -f sql/postgres/partition_trip_events.sql
```

Credentials come from the `mtsai-api-sim-test-credentials` Secrets Manager secret:

```bash
aws secretsmanager get-secret-value --region ap-south-1 --secret-id mtsai-api-sim-test-credentials --query SecretString --output text
```

**The script is split into numbered steps by comment markers.** Steps 1-4 (new partitioned table,
per-date partitions generated dynamically from `SELECT DISTINCT event_date`, data copy, index) are
additive and re-runnable — nothing in `trip_events` itself is touched. **Stop after step 3** and
verify by hand before running step 5 (the actual swap):

```sql
SELECT count(*), sum(trip_id) FROM trip_events;
SELECT count(*), sum(trip_id) FROM trip_events_partitioned;
```

Both rows must match exactly. Only then run step 5, which renames `trip_events` to
`trip_events_pre_partition_backup` and renames `trip_events_partitioned` to `trip_events`, all in
one transaction.

### Post-migration verification

```sql
\d+ trip_events                                       -- confirms "Partition key: RANGE (event_date)"
SELECT count(*), sum(trip_id) FROM trip_events;       -- must match the pre-migration numbers below
SELECT * FROM trip_events WHERE event_date = '<a known date>' LIMIT 5;
```

### Manual cleanup (after confirming good — not automated, not part of the script)

`trip_events_pre_partition_backup` is left in place deliberately as a rollback window. Once the
migration is confirmed good and `cmd/trim` has run at least once successfully, drop it by hand:

```sql
DROP TABLE trip_events_pre_partition_backup;
```

## Part 2: `cmd/trim` — ongoing partition maintenance

Every run does two things:

1. **Ensures the next `TRIM_LOOKAHEAD_DAYS` (default 14) days have a partition** — `CREATE TABLE
   IF NOT EXISTS trip_events_yYYYY_mMM_dDD PARTITION OF trip_events FOR VALUES FROM (...) TO
   (...)`, so inserts never fall into the `trip_events_default` safety-net partition in normal
   operation.
2. **Drops partitions older than `TRIM_RETENTION_DAYS` (default 90)** — but only when
   `export-manifests/{event_date}/trip_events.json` exists in S3 **and** its `success` field is
   `true`. A missing manifest or `success: false` is logged as skipped, not dropped — left for a
   later run once export/backfill catches up.

Writes `export-manifests/trim/{run_date}/summary.json` every run (`manifest.TrimSummary`,
documented in `docs/manifest-format.md`) — partitions created, dropped (with row count captured
immediately before the drop), and skipped (with the reason).

### Running manually

```bash
aws ecs run-task \
  --region ap-south-1 \
  --cluster arn:aws:ecs:ap-south-1:690293068614:cluster/mtsai-datalake-test \
  --task-definition mtsai-datalake-test-trim \
  --launch-type FARGATE \
  --network-configuration '{"awsvpcConfiguration":{"subnets":["subnet-0266f942977861ddd","subnet-08064e40f903eceab","subnet-05639de4c9e0732b7"],"securityGroups":["sg-03adcaeb0b6470eb3"],"assignPublicIp":"ENABLED"}}'
```

Verify afterward independently — via `psql \dt trip_events*` (don't trust the job's own exit code)
and the summary manifest in S3.

## Rehearsal log

**2026-09-22, Test.** Ran the migration for real against `mtsai-api-sim-test` (no `psql` client
in this environment — used `tools/pg-query`, extended with a `-f <path>` mode for this, running
steps 1-4 as one file and step 5 as a second file).

**One real bug found rehearsing this**: the first attempt at step 5 failed —
`ERROR: relation "idx_trip_events_city_date" already exists (SQLSTATE 42P07)`. Renaming a table
does *not* rename its indexes/constraints — `idx_trip_events_city_date` and `trip_events_pkey`
stayed attached to the old table under their original names even after it was renamed to
`trip_events_pre_partition_backup`, so trying to rename the new partitioned table's own
index/constraint onto those same names collided. Because the whole step is one transaction, the
failure rolled back cleanly — independently confirmed `trip_events` was still the original,
unpartitioned table with all 35,000 rows intact, no partial state. Fixed by renaming the old
table's index and constraint out of the way first (`sql/postgres/partition_trip_events.sql` now
does this before the swap). Re-ran step 5 and it committed successfully.

**Verification** (all queried independently after the migration, not trusted from the script's
own output):

| Check | Before | After |
|---|---|---|
| Row count | 35,000 | 35,000 |
| Checksum (`sum(trip_id)`) | 612,517,500 | 612,517,500 |
| `pg_get_partkeydef('trip_events')` | n/a | `RANGE (event_date)` |
| Partition count | n/a | 362 (361 distinct dates + `trip_events_default`) |
| Spot check `event_date = '2026-07-07'` | — | 76 rows (matches the value already recorded in `docs/manifest-format.md`) |
| Sequence continuity | `last_value` 35,000 | `last_value` 35,000, matches `max(trip_id)` |
| Indexes on `trip_events` | — | `trip_events_pkey`, `idx_trip_events_city_date` (clean names, no leftover `_partitioned_` suffix) |
| `trip_events_pre_partition_backup` | — | present, 35,000 rows, own renamed index/constraint (`..._pre_partition_backup_pkey`, `idx_..._pre_partition_backup_city_date`) — not yet dropped |

**`cmd/trim`'s first real run, 2026-09-22, Test** (manually invoked against the real deployed task
after `terraform apply`): exit code 0, 4.77s. Result, independently confirmed against Postgres
afterward (not just the job's own log):

- **14 future partitions created** (`trip_events_y2026_m09_d22` through `_d30`, plus 5 more into
  October) — confirmed present via `pg_class`.
- **31 partitions dropped** — every date from the Phase 2 backfill rehearsal
  (`2026-05-20`–`2026-06-18`, 30 dates) plus the one earlier single-date manual run
  (`2026-01-15`), all of which had a `success: true` export manifest in S3. Confirmed gone via
  `pg_class` afterward (e.g. `trip_events_y2026_m01_d15` — 0 rows returned).
- **245 partitions skipped**, every one logged as `"no export manifest found in S3"` — every date
  in the `SEED_EXTEND_HISTORY` range that was never actually exported. None dropped on missing
  evidence, as designed.
- Row count dropped from 35,000 to 31,670 (partition count 362 → 345, exactly
  `362 - 31 dropped + 14 created`), both independently re-queried.
- `export-manifests/trim/2026-09-22/summary.json` landed in S3 with matching counts.

**One real deployment bug found getting here** (not in `cmd/trim` itself — in the container image
build): the first `docker build`/push produced an **OCI image index**
(`application/vnd.oci.image.index.v1+json`, from BuildKit's newer default of attaching
provenance/SBOM attestations even to a single-platform build), which Fargate failed to pull —
`CannotPullContainerError: ... does not contain descriptor matching platform 'linux/arm64 v8'` —
despite the underlying image genuinely being `arm64`. Fixed by rebuilding with
`docker build --provenance=false --sbom=false ...`, which produces a plain
`application/vnd.docker.distribution.manifest.v2+json` that Fargate pulls correctly. Worth
carrying into any future rebuild of this image.
