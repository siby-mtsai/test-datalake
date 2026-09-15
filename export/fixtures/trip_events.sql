-- Synthetic CITY_ZZ-style fixture data for local integration testing only. No real data.
CREATE TABLE trip_events (
    trip_id         BIGINT PRIMARY KEY,
    vehicle_id_hash TEXT NOT NULL,
    city_code       TEXT NOT NULL,
    event_date      DATE NOT NULL,
    distance_km     DOUBLE PRECISION NOT NULL,
    fare_amount     DOUBLE PRECISION NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT now()
);

-- All rows dated 2026-01-15 so a default (EXPORT_DATE unset) run against "yesterday" won't match
-- anything by accident - pass EXPORT_DATE=2026-01-15 explicitly when running the export job
-- against this fixture.
INSERT INTO trip_events (trip_id, vehicle_id_hash, city_code, event_date, distance_km, fare_amount, created_at) VALUES
    (1, 'hash_veh_aaa111', 'CITY_ZZ', '2026-01-15', 4.2,  120.50, '2026-01-15 08:12:00'),
    (2, 'hash_veh_bbb222', 'CITY_ZZ', '2026-01-15', 12.8, 340.00, '2026-01-15 09:45:00'),
    (3, 'hash_veh_ccc333', 'CITY_ZZ', '2026-01-15', 2.1,  60.00,  '2026-01-15 10:03:00'),
    (4, 'hash_veh_aaa111', 'CITY_ZZ', '2026-01-15', 7.5,  210.25, '2026-01-15 14:22:00'),
    (5, 'hash_veh_ddd444', 'CITY_ZZ', '2026-01-15', 18.3, 480.75, '2026-01-15 19:50:00'),
    -- a different date, to prove the WHERE event_date = $1 filter actually filters
    (6, 'hash_veh_eee555', 'CITY_ZZ', '2026-01-16', 5.0,  150.00, '2026-01-16 07:00:00');
