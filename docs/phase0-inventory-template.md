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

> **This table was run against `mtsai-api-sim`** (`terraform/modules/mtsai-api-sim`), a synthetic
> stand-in database provisioned in Test — there is still no access to the real `mtsai-api`
> database anywhere in this project. Row counts, sizes, and "daily growth" below are therefore
> synthetic seed-data artifacts, not real production figures. Table names, personal-data flags,
> and partition-column choices are genuine design decisions and should carry over; the numbers
> need re-running against the real database once access exists. See `docs/STATUS.md`.

| Table | Row count | Size on disk | Daily growth | Primary key | Personal data? | Natural partition column |
|---|---|---|---|---|---|---|
| `trip_events` | 5,000 | 672 kB | N/A (seeded once, not a live daily feed) | `trip_id` | Yes (`vehicle_id_hash`) | `city_code` + `event_date` |
| `anpr_camera_events` | 3,000 | 416 kB | N/A (seeded once) | `event_id` | Yes (`vehicle_id_hash`) | `city_code` + `event_date` |
| `zone_occupancy_hourly` | 2,000 | 240 kB | N/A (seeded once) | `occupancy_id` | No | `city_code` (hourly aggregate, no event_date column) |
| `reward_ledger` | 1,000 | 160 kB | N/A (seeded once) | `ledger_id` | Yes (`account_id` → `accounts`) | `city_code` + `event_date` |
| `reference_feed_cache` | 500 | 120 kB | N/A (seeded once) | `cache_id` | No | `city_code` only (no natural event date; cache entries) |
| `accounts` | 200 | 64 kB | N/A (seeded once) | `account_id` | Yes (`email_hash`) | Not an event table — hot operational, no partitioning needed (brief Section 5: "nightly snapshot to raw for reporting only") |

Notes:
- "Personal data?" should flag any table carrying an account/vehicle identifier, per Section 5's
  rule that such tables need a stable, indexed identifier for single-DELETE erasure. All three
  "warm events" tables above carry a hashed identifier already (`vehicle_id_hash`/`account_id`) —
  matches the brief's assumption that the real schema already uses blind indexes (Section 5:
  "the lake stores the blind index or ciphertext, never the plaintext identifier").
- "Natural partition column" feeds directly into the Section 4.2 storage layout decision
  (`city_code` + `event_date`, or `event_date` alone if not city-keyed). `reference_feed_cache`
  and `zone_occupancy_hourly` don't have a per-row event date the same way the event tables do —
  worth confirming against the real schema whether the actual equivalents do.
- ~5% of rows across the three event tables carry `city_code = 'CITY_ZZ'` (synthetic/test data,
  brief Section 5's "Test and synthetic" class) — confirms the "exclude synthetic data via
  `city_code`" filtering rule (Section 6) is checkable this way once real discovery happens.
