# MTSAi Traffic Data Lake — Technical Brief and Implementation Plan

| Field | Value |
|---|---|
| Document ID | TDR-004 / DL-PLAN-001 |
| Version | 0.1 (draft for handover) |
| Date | 9 September 2026 |
| Author | Rafeeq Ebrahim, CTO |
| Owner / Implementer | Fizza |
| Status | Handover. Fizza owns delivery end to end; checkpoints listed in Section 12 |
| Classification | Internal, MTSAi |

Transcribed from `MTSAi-Data-Lake-Brief-v0.1.pdf` so this repository, not the standalone PDF, is
the source of truth for the work (per Section 1). Treat this document as the authority for any
Claude Code prompts written for this work — commit prompts to `docs/prompts/` on the feature
branch before running them.

## 1. Purpose of this document

This brief gives you everything needed to design, build and operate the first version of an MTSAi
traffic data lake on AWS without needing to come back to the CTO for day to day decisions. It
records the reasoning behind the decision, the target architecture, a phased delivery plan with
acceptance criteria, the governance rules that must hold, and the small number of checkpoints
where the CTO wants to be consulted.

Where this document says "you", it means the implementer. Where a value is shown in square
brackets, it is a placeholder to confirm against the live system and update in this document.

## 2. Background and problem statement

MTSAi holds a large and growing volume of traffic data in the platform Postgres database (RDS).
This includes ingested third-party feeds, camera and trip events produced during development and
testing, and derived aggregates. The database is a transactional store optimised for the
operational workload of `mtsai-api`, not for long-horizon analytics, model training, or multi-city
comparison.

Concerns that prompted this work:

- Analytical queries over long time ranges compete with the transactional workload and will get
  worse as data grows.
- Raw, append-only event data is expensive to keep in RDS storage and inflates backup and restore
  times.
- Several consumers need the same data — `mtsai-analytics`, the forecasting/AI workstream, city
  dashboards (GR Demo and Control Center), and auditors — and should not all read from the
  primary.
- Retention and erasure obligations (DPDPA in India, GDPR for Budapest, PIPEDA for Toronto) need a
  deliberate storage and deletion design rather than ad hoc table cleanup.

## 3. Decision

Build a minimal, incremental data lake on S3 rather than a full analytics platform. Establish the
storage layer, the export path and a serverless query capability, run it for a month against real
query patterns, and only then decide whether heavier components (a transformation layer, a
warehouse, a feature store) are justified.

### 3.1 Options considered

| Option | Description | Verdict |
|---|---|---|
| A. Optimise Postgres only | Partitioning, read replica, TimescaleDB. No lake. | Rejected as the sole answer. Solves query load but not cost, multi-consumer access, or retention design. Partitioning still recommended as a complementary step (Phase 4). |
| B. Minimal S3 lake with Athena | S3 + Parquet + Iceberg + Glue Catalog + Athena. Batch export from Postgres. | **Selected.** Lowest cost and operational burden, fully serverless, no new always-on infrastructure. |
| C. Streaming lake | Debezium CDC via MSK into S3 in near real time. | Deferred. Only worthwhile if near-real-time analytics are required. Design must not preclude it. |
| D. Redshift or Snowflake warehouse | Managed warehouse loaded from Postgres. | Deferred. Fixed cost and operational overhead not justified pre-deployment. |

### 3.2 Objectives

- All cold traffic data is stored durably in S3 in an open columnar format any engine can read.
- Data is queryable with standard SQL through Athena with no servers to manage.
- The design supports per-city, per-jurisdiction retention and row-level erasure.
- Postgres retains only the hot operational window and its storage footprint reduces.
- Everything is provisioned with Terraform, per environment, in the existing AWS Organization.

### 3.3 Non-goals for this phase

- Real-time or streaming ingestion.
- A BI tool rollout — Athena and existing dashboards are sufficient for now.
- Machine learning pipelines — the lake must be a good source for them, but building them is out
  of scope.
- Migrating any transactional workload off Postgres.

## 4. Target architecture

Deliberately simple. All components are AWS-managed and serverless except the export job, which
runs as a scheduled ECS Fargate task.

| Layer | Component | Notes |
|---|---|---|
| Source | RDS Postgres (`mtsai-api` database) | Read from a read replica if one exists; otherwise a dedicated low-privilege role during the nightly low-traffic window. |
| Export | Scheduled ECS Fargate task (ARM64), Go | Reads closed partitions/date ranges, writes Parquet to S3, registers with the Iceberg table. Triggered by EventBridge Scheduler. |
| Storage | S3, one bucket per environment | Parquet organised as Apache Iceberg tables. Versioning on, KMS encryption, lifecycle to Intelligent Tiering then Glacier. |
| Table format | Apache Iceberg | Schema evolution, ACID commits, time travel, row-level deletes. Required for erasure requests. |
| Catalogue | AWS Glue Data Catalog | One database per zone (raw, curated). |
| Query | Amazon Athena (engine v3) | SQL over the catalogue. Workgroups per consumer with per-query data-scan limits. |
| Access control | IAM (Lake Formation if column-level control becomes necessary) | Per account, per role. No cross-account bucket policies without a documented reason. |
| Observability | CloudWatch, SNS alerts | Export success/failure, row counts reconciled against Postgres, Athena cost per workgroup. |

### 4.1 Zones

Two zones this phase — do not add a third until there's a concrete consumer.

- **raw**: faithful copy of source rows, one Iceberg table per source table, schema mirrors
  Postgres. Never modified except for erasure. Audit and replay layer.
- **curated**: cleaned, deduplicated, typed tables with stable column names. Populated by Athena
  CTAS/INSERT scheduled after export completes.

### 4.2 Storage layout

```
mtsai-datalake-{env}-{account-id}-ap-south-1
  raw/{source_system}/{table}/           Iceberg table root
  curated/{domain}/{table}/               Iceberg table root
  athena-results/{workgroup}/             query outputs, 7 day lifecycle
  export-manifests/{date}/                run manifests and reconciliation reports
```

Partition every event table by `city_code` and `event_date` (day). Iceberg hidden partitioning
means consumers don't need to know the partition columns, but choose them carefully — they can't
be changed cheaply once data is written at scale. If a table isn't naturally city-keyed, partition
by `event_date` only.

File targets: 128 MB–512 MB Parquet files. Run Iceberg compaction (`OPTIMIZE` in Athena) weekly on
tables receiving many small files.

### 4.3 Why Iceberg and not plain Parquet folders

- Row-level deletes needed for erasure requests — plain Parquet requires rewriting whole files.
- Safe schema evolution — adding/renaming a Postgres column doesn't break historic queries.
- Time travel reproduces what a model/report saw on a given date, for audit.
- Athena, Spark, Trino, DuckDB and most ML tooling read Iceberg natively — no lock-in.

## 5. Data classification and retention

See [`classification-table.md`](classification-table.md) — must be completed before writing any
code. Start by listing every table in the `mtsai-api` schema with row count and growth rate (see
[`phase0-inventory-template.md`](phase0-inventory-template.md)).

## 6. Security and governance

- Buckets private, block public access on, SSE-KMS with a customer-managed key per environment.
  Key policy allows only the export role, Athena workgroup roles, and the account break-glass
  role.
- Export task role has `SELECT` on the source schema only, via a dedicated Postgres role in
  Secrets Manager. No write access to Postgres.
- Consumers get an IAM role per workgroup (analytics, forecasting, audit), each with a per-query
  bytes-scanned limit and a monthly cost alarm.
- S3 access logging and CloudTrail data events enabled on the lake bucket in pre-prod and prod.
- Every export run writes a manifest (tables, date ranges, row counts, checksums, duration) to
  `export-manifests/` — the evidence trail for the audit service.
- An erasure runbook exists and is rehearsed in Dev before any personal data lands in a prod lake
  (Section 9 / [`runbooks/erasure.md`](../runbooks/erasure.md)).
- Cross-account access is a checkpoint decision, not something to add on the fly.

## 7. Delivery plan

Four phases (after Phase 0 discovery). Each has a definition of done. Don't start a phase until
the previous is signed off, but Terraform for the next phase may be prepared in parallel. All work
happens first in the Dev account (`517293881120`, `miracletraffic-india-dev`) and promotes through
Test and pre-prod via feature → develop → main.

### Phase 0: Discovery (target: 3 working days)

1. Inventory every table in the source database: name, row count, size on disk, daily growth,
   primary key, personal data?, natural partition column (`pg_stat_user_tables`,
   `pg_total_relation_size`).
2. Identify the top 20 slowest/most expensive queries (`pg_stat_statements`), classify analytical
   vs transactional — this is the baseline for later improvement.
3. Complete the Section 5 classification table and circulate to the CTO and the Budapest/Toronto
   compliance owner.
4. Confirm whether a read replica exists and its lag; if not, decide the nightly export window
   with the mtsai-api team.

**Done when:** the inventory spreadsheet is committed under `docs/`, the classification table is
agreed, and the export window is fixed.

### Phase 1: Foundation (target: 5 working days)

1. Create the repository `mtsai-rnd/mtsai-datalake` with the Section 8 layout.
2. Terraform: S3 bucket (versioning, KMS, lifecycle, access logging), Glue databases (raw,
   curated), Athena workgroups with results locations and scan limits, IAM roles for export and
   each consumer, EventBridge schedule, CloudWatch alarms, SNS topic.
3. Apply in Dev via the GitHub Actions OIDC pipeline. Confirm `DeveloperPowerUser` has the IAM
   permissions needed (the v2 handoff added the inline IAM lifecycle policy; if `AccessDenied` on
   `iam:CreateRole`, stop and check the aws-org notes before widening anything).
4. Create one Iceberg table in raw by hand in Athena and verify a CTAS into curated works
   end-to-end.

**Done when:** `terraform plan` is clean, all resources tagged (`Project=datalake`, `Environment`,
`Owner`), and a sample Iceberg table is queryable from the analytics workgroup and not from any
other role.

### Phase 2: Export pipeline (target: 8 working days)

1. Write the export job in Go (module `mtsai-datalake-export`): read table list/date ranges from
   config, stream rows from Postgres with server-side cursors, write Parquet with a schema derived
   from the table definition, upload to S3, commit to Iceberg via Athena (prefer `INSERT INTO` for
   v1 simplicity).
2. Idempotency: keyed by `(table, event_date)`. Re-running a completed key replaces (Iceberg
   partition overwrite), never duplicates.
3. Reconciliation: after each table, compare row counts and a primary-key-set checksum between
   Postgres and Iceberg for the exported range. Mismatch fails the run and raises an alarm.
4. Package as an ARM64 container on ECS Fargate, scheduled nightly. Structured JSON logs to
   CloudWatch.
5. Backfill full history in date chunks; record throughput.
6. Build the curated layer: one scheduled Athena query per curated table, run after export
   completes (Step Functions or a second scheduled task waiting on the manifest).

**Done when:** the nightly job has run seven consecutive nights in Dev with zero reconciliation
failures, full history is backfilled, and the twenty baseline queries from Phase 0 are rewritten
against curated tables with recorded execution times and bytes scanned.

### Phase 3: Governance and erasure (target: 4 working days)

1. Write and rehearse the erasure runbook (Section 9) in Dev against synthetic accounts; measure
   end-to-end erasure time for a single identifier.
2. Implement lifecycle: Iceberg `expire_snapshots` and `remove_orphan_files` weekly; S3 lifecycle
   to Intelligent Tiering at 30 days, Glacier Instant Retrieval at [365] days for raw.
3. Wire export manifests into what the audit service expects, or document the manifest format for
   it.
4. Cost dashboard: S3 storage by prefix, Athena bytes scanned by workgroup, Fargate minutes.
   Monthly budget alarm.

**Done when:** an erasure has been executed and verified (the identifier returns zero rows in
every table, including time-travel snapshots after expiry), and the cost dashboard exists.

### Phase 4: Postgres trim and promotion (target: 3 working days plus observation)

1. Introduce native partitioning on the largest event tables in Postgres if not already present
   (`event_date`), so old data drops as partitions rather than row-by-row deletes.
2. Drop Postgres partitions older than the retention window only after reconciliation for that
   range has passed and the manifest is in S3.
3. Promote the whole stack to Test and pre-prod through the pipeline. Prod promotion is a
   checkpoint (Section 12).
4. Run for one month. Capture actual Athena usage, cost, and any unmet requests. Write a one-page
   review recommending whether Option C or D is now justified.

**Done when:** Postgres storage has reduced measurably, the stack is live in pre-prod, and the
one-month review is delivered.

## 8. Repository layout and conventions

```
mtsai-datalake/
  docs/                    this document, ADRs, runbooks, Phase 0 inventory
  docs/prompts/            Claude Code prompts, committed before execution
  terraform/modules/       lake-bucket, glue-catalog, athena-workgroup, export-task
  terraform/envs/{dev,test,preprod,prod}/
  export/                  Go export job (cmd/, internal/, Dockerfile)
  sql/raw/                 Iceberg DDL per raw table
  sql/curated/             CTAS / INSERT statements per curated table
  runbooks/                erasure, backfill, re-run, compaction
  .github/workflows/       plan on PR, apply on merge, OIDC to the target account
```

Conventions: Terraform state per environment in the existing state bucket with DynamoDB locking;
no hard-coded account IDs (read from a variables file per env); every resource tagged; Go 1.23,
`golangci-lint` in CI; integration tests for the export job run against a Postgres container with
`CITY_ZZ` fixtures.

## 9. Erasure runbook (outline)

See [`runbooks/erasure.md`](../runbooks/erasure.md).

## 10. Cost guidance

| Item | Driver | Estimate method |
|---|---|---|
| S3 storage | Compressed Parquet typically 5–10x smaller than the Postgres heap | Postgres size on disk ÷ [7] × S3 Standard price for ap-south-1, then apply tiering |
| Athena | Bytes scanned per query | Baseline queries × expected daily runs × bytes scanned after partition pruning |
| Fargate export | Minutes per nightly run | Measured in Phase 2 backfill |
| Glue Catalog | Objects and requests | Negligible at this scale |
| KMS | Requests | Negligible; use bucket keys to reduce request volume |
| RDS saving | Storage reclaimed | GB dropped × RDS gp3 price, plus reduced backup storage |

## 11. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Export load on the primary | Latency on mtsai-api during export window | Read replica or off-peak window; server-side cursors; rate limit rows/sec; abort if replication lag exceeds threshold |
| Wrong partition choice | Expensive rewrite later | Decide from Phase 0 query analysis; validate with a subset before backfill |
| Many small files | Slow, expensive Athena queries | Target file sizes; weekly compaction; monitor file counts per partition |
| Personal data in the wrong place | Compliance breach | Classification table is a gate; export config lists tables explicitly, never `SELECT *` |
| Silent data drift between Postgres and lake | Wrong analytics | Mandatory reconciliation per run; alarm on failure |
| Schema change in mtsai-api breaks export | Nightly failure | Export derives schema at runtime; Iceberg handles additive change; CI test against a migrated schema |
| Cost creep from ad hoc Athena use | Budget | Workgroup scan limits and monthly alarms from Phase 1 |

## 12. Checkpoints where the CTO wants to be involved

Everything else is the implementer's to decide. Bring these with a written recommendation:

1. Sign off on the Section 5 classification table before Phase 1 (compliance consequences).
2. Any change to the partition strategy after backfill has started.
3. Any cross-account access or public endpoint.
4. Promotion to the prod account.
5. Any proposal to move to streaming (Option C) or a warehouse (Option D) — as a short TDR after
   the one-month review.
6. Anything requiring IAM permissions wider than the aws-org handoff defines.

## 13. Working rhythm and reporting

- Keep [`STATUS.md`](STATUS.md) updated at the end of each phase, plus a one-line Friday update to
  the CTO.
- Every Terraform change goes through a PR with a plan attached. Every export job change ships
  with a test.
- Prompts for Claude Code are committed to `docs/prompts/` before they're run; results are
  verified against live behaviour (a successful Athena query, a matching reconciliation) rather
  than green unit tests alone.
- If blocked more than half a day on an access or account issue, escalate rather than working
  around it.

## 14. Reference material

- MTSAi AWS Organization handoff notes (account layout, Identity Center, OIDC roles).
- LLD-003 Identity Service (identifier handling, blind indexes, crypto erasure).
- Congestion Zone Platform Feature Spec v1.2 (traffic feed sources).
- AWS documentation: Athena Iceberg tables, Glue Data Catalog, S3 lifecycle configuration, Lake
  Formation.
- Apache Iceberg specification and the Athena `OPTIMIZE` / `VACUUM` statements.
