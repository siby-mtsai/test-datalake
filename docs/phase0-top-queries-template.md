# Phase 0 — Top 20 Query Baseline

Source: `pg_stat_statements` on the `mtsai-api` database. Requires the extension to be enabled:

```sql
SELECT query, calls, total_exec_time, mean_exec_time, rows
FROM pg_stat_statements
ORDER BY total_exec_time DESC
LIMIT 20;
```

This baseline is what Phase 2's "done when" measures against once the same queries are rewritten
against curated Iceberg tables (execution time and bytes scanned).

> **Run against `mtsai-api-sim`** (synthetic stand-in, see `docs/STATUS.md`), not the real
> `mtsai-api`. The query *shapes* below (point lookups vs. aggregates/joins) are representative of
> the kind of workload the brief describes, and `tools/seed-api-db`'s workload was deliberately
> designed as a mix of both — but the actual queries a re-run against the real database produces
> will differ. RDS's own internal bootstrap/housekeeping queries (`rds_heartbeat2`,
> `pg_rds_insert_creds`, role setup, etc.) and this session's own schema-creation/COPY statements
> were excluded below as setup noise, not recurring production traffic.

| # | Query (summarized) | Calls | Total exec time (ms) | Mean exec time (ms) | Analytical or transactional? |
|---|---|---|---|---|---|
| 1 | `SELECT t.city_code, count(*), count(DISTINCT vehicle_id_hash) FROM trip_events t JOIN anpr_camera_events a ON ... WHERE event_date >= $1 GROUP BY city_code` | 12 | 143.9 | 12.0 | **Analytical** — cross-table join + aggregate over a date range |
| 2 | `SELECT city_code, zone_id, avg(occupancy_pct) FROM zone_occupancy_hourly WHERE hour_ts >= now() - interval $1 GROUP BY city_code, zone_id` | 20 | 18.3 | 0.92 | **Analytical** — aggregate over a rolling window |
| 3 | `SELECT city_code, count(*), avg(fare_amount), sum(distance_km) FROM trip_events WHERE event_date >= $1 GROUP BY city_code` | 16 | 14.0 | 0.88 | **Analytical** — aggregate over a date range |
| 4 | `SELECT a.city_code, r.ledger_type, count(*), sum(r.amount) FROM reward_ledger r JOIN accounts a ON ... GROUP BY a.city_code, r.ledger_type` | 12 | 10.9 | 0.91 | **Analytical** — full-table join + aggregate |
| 5 | `SELECT * FROM trip_events WHERE trip_id = $1` | 92 | 2.3 | 0.025 | **Transactional** — primary-key point lookup |
| 6 | `SELECT * FROM accounts WHERE account_id = $1` | 68 | 1.5 | 0.021 | **Transactional** — primary-key point lookup |

Only 6 distinct application-shaped queries appear because the seeded workload
(`tools/seed-api-db`) intentionally ran a small, representative mix (2 transactional + 4
analytical patterns) rather than a full application's query surface — real discovery will surface
many more once run against the actual `mtsai-api` traffic.

**Pattern already visible even in this synthetic run**, and consistent with the brief's own
problem statement (Section 2): the analytical queries (#1-4) account for the overwhelming
majority of total execution time (187ms of ~191ms across these six, ~98%) despite being called far
less often than the transactional point lookups (#5-6, 160 calls combined vs. 60 for the
analytical ones). This is exactly the tension Section 2 describes — a small number of
long-running analytical queries competing with high-frequency transactional traffic — and is the
first real (if synthetic) evidence for it in this project.
