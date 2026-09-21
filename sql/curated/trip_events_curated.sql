-- One-time table setup for the curated zone's trip_events table (brief Phase 1 step 4 precedent:
-- "create one Iceberg table in raw by hand... verify a CTAS into curated works end to end" - this
-- is that CTAS, finally committed to the repo instead of being run ad hoc and lost the way the
-- earlier test_table_curated example was (see docs/STATUS.md; no SQL text for it survives
-- anywhere in version control).
--
-- v1 curated is a schema-stable passthrough copy of raw, not real cleaning/dedup logic yet -
-- WHERE 1=0 creates the table with the right schema/location but zero rows; cmd/curate populates
-- it per day afterward via DELETE + INSERT (internal/lake's idempotency pattern, applied to a
-- cross-database copy instead of a staging-table commit).
--
-- Run once, by hand, against Athena (database context: test_curated). Replace the bucket name if
-- running against a different environment.
CREATE TABLE trip_events_curated
WITH (
  table_type = 'ICEBERG',
  format = 'PARQUET',
  location = 's3://mtsai-datalake-test-690293068614-ap-south-1/curated/trip_events/',
  is_external = false
)
AS SELECT trip_id, vehicle_id_hash, city_code, event_date, distance_km, fare_amount, created_at
FROM test_raw.trip_events
WHERE 1 = 0;
