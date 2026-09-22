# sql/postgres

One-time, run-by-hand migrations against `mtsai-api-sim` itself (not Athena/Iceberg — see
`sql/raw/` and `sql/curated/` for those). Currently one file: `partition_trip_events.sql`, the
Phase 4 step 1 migration converting `trip_events` into a daily range-partitioned table. See
`runbooks/postgres-partitioning.md` for the full procedure and rehearsal log.
