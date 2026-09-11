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

| # | Query (redacted/summarized) | Calls | Total exec time | Mean exec time | Analytical or transactional? |
|---|---|---|---|---|---|
| 1 | | | | | |
