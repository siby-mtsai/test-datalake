# ADR 0001: Minimal S3 lake with Athena over Postgres optimisation, streaming, or a warehouse

Status: Accepted (per DL-PLAN-001 v0.1, Section 3)

## Context

MTSAi holds a growing volume of traffic data in RDS Postgres, optimised for `mtsai-api`'s
transactional workload, not long-horizon analytics. Analytical queries compete with production
traffic, raw event data inflates RDS storage/backup costs, multiple consumers (analytics,
forecasting, dashboards, auditors) need the same data without hitting the primary, and
DPDPA/GDPR/PIPEDA retention and erasure obligations need a deliberate design.

## Decision

Build a minimal, incremental data lake: S3 + Parquet + Apache Iceberg + AWS Glue Data Catalog +
Amazon Athena, fed by a nightly batch export (ECS Fargate, Go) from Postgres. Run it for a month
against real query patterns before deciding whether heavier components are justified.

## Alternatives considered

| Option | Verdict |
|---|---|
| A. Optimise Postgres only (partitioning, read replica, TimescaleDB) | Rejected as sole answer — doesn't solve cost, multi-consumer access, or retention design. Partitioning kept as a complementary Phase 4 step. |
| C. Streaming lake (Debezium CDC via MSK) | Deferred — only worthwhile if near-real-time analytics are required; design must not preclude it later. |
| D. Redshift/Snowflake warehouse | Deferred — fixed cost and operational overhead not justified pre-deployment. |

## Consequences

- Apache Iceberg (not plain Parquet folders) is required for row-level deletes (erasure), safe
  schema evolution, and time travel — see brief Section 4.3.
- Two zones only for this phase: `raw` and `curated`. No transformation layer, warehouse, or
  feature store until the one-month review recommends one.
- Everything is provisioned via Terraform per environment; no always-on infrastructure beyond the
  scheduled export task.

See [`../DL-PLAN-001-brief.md`](../DL-PLAN-001-brief.md) for the full brief.
