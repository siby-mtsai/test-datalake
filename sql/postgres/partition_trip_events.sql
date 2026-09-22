-- ============================================================================
-- One-time migration (brief Phase 4 step 1): make trip_events a native,
-- daily range-partitioned table on event_date, so old data can later be
-- dropped as whole partitions (cmd/trim) instead of row-by-row DELETEs.
--
-- Postgres can't ALTER a table with existing data into PARTITION BY in
-- place, so this creates a new partitioned table, copies the data across,
-- and swaps it in under the original name - same "run once, by hand"
-- precedent as sql/curated/trip_events_curated.sql. Run once, by hand, via
-- psql against mtsai-api-sim-test. Steps 1-4 are additive and re-runnable
-- (IF NOT EXISTS / ON CONFLICT DO NOTHING throughout) - do not run step 5
-- until step 3's verification query has been checked by hand and matches
-- exactly. See runbooks/postgres-partitioning.md for the full procedure
-- and rehearsal log.
--
-- Partitioning is daily (one partition per event_date), matching the
-- per-date granularity export-manifests/{event_date}/trip_events.json
-- already reconciles at - cmd/trim's drop-gate check is then a direct,
-- unambiguous per-date manifest lookup with no month-level aggregation.
-- ============================================================================

-- ---------------------------------------------------------------------------
-- Step 1: new partitioned parent table.
--
-- Postgres requires a partitioned table's primary key to include every
-- partition key column, so the PK becomes (trip_id, event_date) instead of
-- just (trip_id). trip_id itself stays globally unique in practice because
-- it's still generated from the one shared trip_events_trip_id_seq sequence
-- (reused below, never recreated) - this is a constraint-shape change only,
-- not a real relaxation of uniqueness.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS trip_events_partitioned (
    trip_id         BIGINT NOT NULL DEFAULT nextval('trip_events_trip_id_seq'),
    vehicle_id_hash TEXT NOT NULL,
    city_code       TEXT NOT NULL,
    event_date      DATE NOT NULL,
    distance_km     DOUBLE PRECISION NOT NULL,
    fare_amount     DOUBLE PRECISION NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (trip_id, event_date)
) PARTITION BY RANGE (event_date);

-- ---------------------------------------------------------------------------
-- Step 2: one partition per calendar date actually present in trip_events
-- today, generated dynamically (not hand-listed) so this script stays
-- correct regardless of exactly which dates were seeded, plus a DEFAULT
-- partition as a safety net for any date outside that range (e.g. a seed
-- run landing data before cmd/trim has had a chance to pre-create that
-- day's partition).
-- ---------------------------------------------------------------------------
DO $$
DECLARE
    d          DATE;
    part_name  TEXT;
BEGIN
    FOR d IN SELECT DISTINCT event_date FROM trip_events ORDER BY 1 LOOP
        part_name := 'trip_events_y' || to_char(d, 'YYYY') || '_m' || to_char(d, 'MM') || '_d' || to_char(d, 'DD');
        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF trip_events_partitioned FOR VALUES FROM (%L) TO (%L)',
            part_name, d, d + 1
        );
    END LOOP;
END $$;

CREATE TABLE IF NOT EXISTS trip_events_default PARTITION OF trip_events_partitioned DEFAULT;

-- ---------------------------------------------------------------------------
-- Step 3: copy the data across, then STOP and verify by hand before step 5:
--
--   SELECT count(*), sum(trip_id) FROM trip_events;
--   SELECT count(*), sum(trip_id) FROM trip_events_partitioned;
--
-- Both rows must match exactly. Record both in
-- runbooks/postgres-partitioning.md's rehearsal log.
-- ---------------------------------------------------------------------------
INSERT INTO trip_events_partitioned (trip_id, vehicle_id_hash, city_code, event_date, distance_km, fare_amount, created_at)
SELECT trip_id, vehicle_id_hash, city_code, event_date, distance_km, fare_amount, created_at
FROM trip_events
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Step 4: recreate the composite index at the parent level - Postgres
-- propagates it to every partition automatically.
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_trip_events_partitioned_city_date ON trip_events_partitioned (city_code, event_date);

-- ---------------------------------------------------------------------------
-- Step 5: swap the tables. DESTRUCTIVE POINT OF NO EASY RETURN - do not run
-- this block until step 3's verification above has been checked by hand.
-- ---------------------------------------------------------------------------
BEGIN;

-- Renaming a table does NOT rename its indexes/constraints - idx_trip_events_city_date and
-- trip_events_pkey stay attached to the (now-renamed-away) backup table under their original
-- names, so they have to be renamed out of the way first to free those names up for the new
-- partitioned table's own index/constraint below. Found the hard way: the first rehearsal of this
-- script failed here with "relation idx_trip_events_city_date already exists" and rolled back
-- cleanly (whole block is one transaction) - no partial state, just re-run from the top.
ALTER TABLE trip_events RENAME TO trip_events_pre_partition_backup;
ALTER INDEX idx_trip_events_city_date RENAME TO idx_trip_events_pre_partition_backup_city_date;
ALTER TABLE trip_events_pre_partition_backup RENAME CONSTRAINT trip_events_pkey TO trip_events_pre_partition_backup_pkey;

ALTER TABLE trip_events_partitioned RENAME TO trip_events;
ALTER INDEX idx_trip_events_partitioned_city_date RENAME TO idx_trip_events_city_date;
ALTER TABLE trip_events RENAME CONSTRAINT trip_events_partitioned_pkey TO trip_events_pkey;

-- Re-point the shared sequence's ownership at the new trip_events table so a
-- later `DROP TABLE trip_events_pre_partition_backup` (runbooks/postgres-
-- partitioning.md's cleanup step) can never CASCADE-drop it.
ALTER SEQUENCE trip_events_trip_id_seq OWNED BY trip_events.trip_id;
SELECT setval('trip_events_trip_id_seq', (SELECT COALESCE(MAX(trip_id), 1) FROM trip_events));

COMMIT;

-- ---------------------------------------------------------------------------
-- Post-migration verification (run by hand, not part of this script):
--
--   \d+ trip_events                                  -- confirms
--       "Partition key: RANGE (event_date)"
--   SELECT count(*), sum(trip_id) FROM trip_events;   -- must match the
--       pre-migration numbers recorded in runbooks/postgres-partitioning.md
--   SELECT * FROM trip_events WHERE event_date = '<a known date>' LIMIT 5;
--
-- trip_events_pre_partition_backup is left in place deliberately as a
-- rollback window - do not drop it here. See
-- runbooks/postgres-partitioning.md for the manual cleanup step.
-- ---------------------------------------------------------------------------
