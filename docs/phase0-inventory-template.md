# Phase 0 — Postgres Table Inventory

Source: `mtsai-api` database (RDS Postgres). Populate using:

```sql
-- row counts & basic stats
SELECT schemaname, relname, n_live_tup, n_dead_tup
FROM pg_stat_user_tables
ORDER BY n_live_tup DESC;

-- size on disk
SELECT relname, pg_size_pretty(pg_total_relation_size(relid)) AS total_size
FROM pg_catalog.pg_statio_user_tables
ORDER BY pg_total_relation_size(relid) DESC;
```

Daily growth: sample `n_live_tup` / total size over a few days, or use `created_at`/`event_date`
columns where present to estimate rows-per-day.

| Table | Row count | Size on disk | Daily growth | Primary key | Personal data? | Natural partition column |
|---|---|---|---|---|---|---|
| | | | | | | |

Notes:
- "Personal data?" should flag any table carrying an account/vehicle identifier, per Section 5's
  rule that such tables need a stable, indexed identifier for single-DELETE erasure.
- "Natural partition column" feeds directly into the Section 4.2 storage layout decision
  (`city_code` + `event_date`, or `event_date` alone if not city-keyed).
