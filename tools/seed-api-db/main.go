// Command seed-api-db creates a schema and synthetic data set in the mtsai-api-sim RDS instance
// (terraform/modules/mtsai-api-sim), then runs a representative query workload so
// pg_stat_statements has something real to report. This is a one-off setup tool for unblocking
// Phase 0 discovery against a synthetic stand-in database - not part of the shipped export job.
//
// Connection details come from discrete PG* env vars (matching libpq convention), read from the
// mtsai-api-sim Secrets Manager secret:
//
//	aws secretsmanager get-secret-value --secret-id <arn> --query SecretString --output text
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

var cityCodes = []string{"BLR", "MUM", "DEL", "HYD", "PUN"}

const syntheticCityCode = "CITY_ZZ" // brief Section 5/6: test/synthetic fixture data, excluded from the real lake

func main() {
	if err := run(); err != nil {
		log.Fatalf("seed failed: %v", err)
	}
}

func run() error {
	ctx := context.Background()
	rng := rand.New(rand.NewSource(42)) // fixed seed - reproducible synthetic data across re-runs

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=require",
		os.Getenv("PGUSER"), os.Getenv("PGPASSWORD"), os.Getenv("PGHOST"), os.Getenv("PGPORT"), os.Getenv("PGDATABASE"))

	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer conn.Close(ctx)

	log.Println("connected, creating schema...")
	if err := createSchema(ctx, conn); err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}

	log.Println("seeding accounts...")
	accountIDs, err := seedAccounts(ctx, conn, rng, 200)
	if err != nil {
		return fmt.Errorf("seeding accounts: %w", err)
	}

	log.Println("seeding trip_events...")
	if err := seedTripEvents(ctx, conn, rng, 5000); err != nil {
		return fmt.Errorf("seeding trip_events: %w", err)
	}

	log.Println("seeding anpr_camera_events...")
	if err := seedANPREvents(ctx, conn, rng, 3000); err != nil {
		return fmt.Errorf("seeding anpr_camera_events: %w", err)
	}

	log.Println("seeding reward_ledger...")
	if err := seedRewardLedger(ctx, conn, rng, 1000, accountIDs); err != nil {
		return fmt.Errorf("seeding reward_ledger: %w", err)
	}

	log.Println("seeding reference_feed_cache...")
	if err := seedReferenceFeedCache(ctx, conn, rng, 500); err != nil {
		return fmt.Errorf("seeding reference_feed_cache: %w", err)
	}

	log.Println("seeding zone_occupancy_hourly...")
	if err := seedZoneOccupancy(ctx, conn, rng, 2000); err != nil {
		return fmt.Errorf("seeding zone_occupancy_hourly: %w", err)
	}

	log.Println("enabling pg_stat_statements...")
	if _, err := conn.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`); err != nil {
		return fmt.Errorf("creating pg_stat_statements extension: %w", err)
	}

	log.Println("running representative query workload for pg_stat_statements...")
	if err := runWorkload(ctx, conn, rng, accountIDs); err != nil {
		return fmt.Errorf("running workload: %w", err)
	}

	log.Println("done")
	return nil
}

func createSchema(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS accounts (
			account_id  BIGSERIAL PRIMARY KEY,
			email_hash  TEXT NOT NULL,
			city_code   TEXT NOT NULL,
			status      TEXT NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		);

		CREATE TABLE IF NOT EXISTS trip_events (
			trip_id         BIGSERIAL PRIMARY KEY,
			vehicle_id_hash TEXT NOT NULL,
			city_code       TEXT NOT NULL,
			event_date      DATE NOT NULL,
			distance_km     DOUBLE PRECISION NOT NULL,
			fare_amount     DOUBLE PRECISION NOT NULL,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
		);

		CREATE TABLE IF NOT EXISTS anpr_camera_events (
			event_id        BIGSERIAL PRIMARY KEY,
			vehicle_id_hash TEXT NOT NULL,
			camera_id       TEXT NOT NULL,
			city_code       TEXT NOT NULL,
			event_date      DATE NOT NULL,
			captured_at     TIMESTAMPTZ NOT NULL
		);

		CREATE TABLE IF NOT EXISTS reward_ledger (
			ledger_id   BIGSERIAL PRIMARY KEY,
			account_id  BIGINT NOT NULL REFERENCES accounts(account_id),
			city_code   TEXT NOT NULL,
			ledger_type TEXT NOT NULL,
			amount      NUMERIC(10,2) NOT NULL,
			event_date  DATE NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		);

		CREATE TABLE IF NOT EXISTS reference_feed_cache (
			cache_id        BIGSERIAL PRIMARY KEY,
			provider        TEXT NOT NULL,
			city_code       TEXT NOT NULL,
			fetched_at      TIMESTAMPTZ NOT NULL,
			payload_summary TEXT
		);

		CREATE TABLE IF NOT EXISTS zone_occupancy_hourly (
			occupancy_id  BIGSERIAL PRIMARY KEY,
			city_code     TEXT NOT NULL,
			zone_id       TEXT NOT NULL,
			hour_ts       TIMESTAMPTZ NOT NULL,
			occupancy_pct DOUBLE PRECISION NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_trip_events_city_date ON trip_events (city_code, event_date);
		CREATE INDEX IF NOT EXISTS idx_anpr_city_date ON anpr_camera_events (city_code, event_date);
	`)
	if err != nil {
		return err
	}

	// Idempotent re-runs: without this, running the tool twice silently doubles every row count
	// (CREATE TABLE IF NOT EXISTS doesn't clear existing data) - found exactly this way.
	_, err = conn.Exec(ctx, `TRUNCATE TABLE
		reward_ledger, anpr_camera_events, trip_events, zone_occupancy_hourly,
		reference_feed_cache, accounts
		RESTART IDENTITY CASCADE`)
	return err
}

// cityForRow returns a synthetic CITY_ZZ code ~5% of the time, a real-ish city the rest - brief
// Section 5/6's "test and synthetic" class needs real examples to classify and filter against.
func cityForRow(rng *rand.Rand) string {
	if rng.Intn(20) == 0 {
		return syntheticCityCode
	}
	return cityCodes[rng.Intn(len(cityCodes))]
}

func randomPastDate(rng *rand.Rand, daysBack int) time.Time {
	d := rng.Intn(daysBack)
	return time.Now().UTC().AddDate(0, 0, -d).Truncate(24 * time.Hour)
}

func seedAccounts(ctx context.Context, conn *pgx.Conn, rng *rand.Rand, n int) ([]int64, error) {
	rows := make([][]any, n)
	statuses := []string{"active", "active", "active", "suspended"}
	for i := 0; i < n; i++ {
		rows[i] = []any{
			fmt.Sprintf("hash_acct_%06d", i),
			cityForRow(rng),
			statuses[rng.Intn(len(statuses))],
		}
	}
	_, err := conn.CopyFrom(ctx, pgx.Identifier{"accounts"}, []string{"email_hash", "city_code", "status"}, pgx.CopyFromRows(rows))
	if err != nil {
		return nil, err
	}

	idRows, err := conn.Query(ctx, `SELECT account_id FROM accounts ORDER BY account_id`)
	if err != nil {
		return nil, err
	}
	defer idRows.Close()
	var ids []int64
	for idRows.Next() {
		var id int64
		if err := idRows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, idRows.Err()
}

func seedTripEvents(ctx context.Context, conn *pgx.Conn, rng *rand.Rand, n int) error {
	rows := make([][]any, n)
	for i := 0; i < n; i++ {
		rows[i] = []any{
			fmt.Sprintf("hash_veh_%06d", rng.Intn(1500)),
			cityForRow(rng),
			randomPastDate(rng, 90),
			roundTo(rng.Float64()*25+0.5, 1),
			roundTo(rng.Float64()*600+30, 2),
		}
	}
	_, err := conn.CopyFrom(ctx, pgx.Identifier{"trip_events"},
		[]string{"vehicle_id_hash", "city_code", "event_date", "distance_km", "fare_amount"},
		pgx.CopyFromRows(rows))
	return err
}

func seedANPREvents(ctx context.Context, conn *pgx.Conn, rng *rand.Rand, n int) error {
	rows := make([][]any, n)
	for i := 0; i < n; i++ {
		d := randomPastDate(rng, 90)
		rows[i] = []any{
			fmt.Sprintf("hash_veh_%06d", rng.Intn(1500)),
			fmt.Sprintf("cam_%03d", rng.Intn(80)),
			cityForRow(rng),
			d,
			d.Add(time.Duration(rng.Intn(86400)) * time.Second),
		}
	}
	_, err := conn.CopyFrom(ctx, pgx.Identifier{"anpr_camera_events"},
		[]string{"vehicle_id_hash", "camera_id", "city_code", "event_date", "captured_at"},
		pgx.CopyFromRows(rows))
	return err
}

func seedRewardLedger(ctx context.Context, conn *pgx.Conn, rng *rand.Rand, n int, accountIDs []int64) error {
	types := []string{"reward", "reward", "penalty"}
	rows := make([][]any, n)
	for i := 0; i < n; i++ {
		rows[i] = []any{
			accountIDs[rng.Intn(len(accountIDs))],
			cityForRow(rng),
			types[rng.Intn(len(types))],
			roundTo(rng.Float64()*200-50, 2),
			randomPastDate(rng, 90),
		}
	}
	_, err := conn.CopyFrom(ctx, pgx.Identifier{"reward_ledger"},
		[]string{"account_id", "city_code", "ledger_type", "amount", "event_date"},
		pgx.CopyFromRows(rows))
	return err
}

func seedReferenceFeedCache(ctx context.Context, conn *pgx.Conn, rng *rand.Rand, n int) error {
	providers := []string{"mapmyindia", "tomtom", "google_routes", "weather"}
	rows := make([][]any, n)
	for i := 0; i < n; i++ {
		provider := providers[rng.Intn(len(providers))]
		rows[i] = []any{
			provider,
			cityCodes[rng.Intn(len(cityCodes))], // reference feeds aren't synthetic-city data
			randomPastDate(rng, 30),
			fmt.Sprintf("%s response summary #%d", provider, i),
		}
	}
	_, err := conn.CopyFrom(ctx, pgx.Identifier{"reference_feed_cache"},
		[]string{"provider", "city_code", "fetched_at", "payload_summary"},
		pgx.CopyFromRows(rows))
	return err
}

func seedZoneOccupancy(ctx context.Context, conn *pgx.Conn, rng *rand.Rand, n int) error {
	rows := make([][]any, n)
	for i := 0; i < n; i++ {
		rows[i] = []any{
			cityCodes[rng.Intn(len(cityCodes))],
			fmt.Sprintf("zone_%02d", rng.Intn(20)),
			randomPastDate(rng, 30).Add(time.Duration(rng.Intn(24)) * time.Hour),
			roundTo(rng.Float64()*100, 1),
		}
	}
	_, err := conn.CopyFrom(ctx, pgx.Identifier{"zone_occupancy_hourly"},
		[]string{"city_code", "zone_id", "hour_ts", "occupancy_pct"},
		pgx.CopyFromRows(rows))
	return err
}

func roundTo(v float64, places int) float64 {
	mult := 1.0
	for i := 0; i < places; i++ {
		mult *= 10
	}
	return float64(int(v*mult+0.5)) / mult
}

// runWorkload runs a mix of cheap point-lookup ("transactional") queries and heavier
// join/aggregate ("analytical") queries repeatedly, so pg_stat_statements has real, classifiable
// entries for Phase 0 step 2 rather than a single cold call per query shape.
func runWorkload(ctx context.Context, conn *pgx.Conn, rng *rand.Rand, accountIDs []int64) error {
	transactional := []func() (string, []any){
		func() (string, []any) {
			return `SELECT * FROM accounts WHERE account_id = $1`, []any{accountIDs[rng.Intn(len(accountIDs))]}
		},
		func() (string, []any) {
			return `SELECT * FROM trip_events WHERE trip_id = $1`, []any{rng.Intn(5000) + 1}
		},
	}

	analytical := []func() (string, []any){
		func() (string, []any) {
			return `SELECT city_code, count(*), avg(fare_amount), sum(distance_km)
			        FROM trip_events WHERE event_date >= $1 GROUP BY city_code ORDER BY city_code`,
				[]any{randomPastDate(rng, 30)}
		},
		func() (string, []any) {
			return `SELECT a.city_code, r.ledger_type, count(*), sum(r.amount)
			        FROM reward_ledger r JOIN accounts a ON a.account_id = r.account_id
			        GROUP BY a.city_code, r.ledger_type ORDER BY a.city_code, r.ledger_type`, nil
		},
		func() (string, []any) {
			return `SELECT city_code, zone_id, avg(occupancy_pct)
			        FROM zone_occupancy_hourly
			        WHERE hour_ts >= now() - interval '7 days'
			        GROUP BY city_code, zone_id ORDER BY city_code, zone_id`, nil
		},
		func() (string, []any) {
			return `SELECT t.city_code, count(*) trips, count(DISTINCT t.vehicle_id_hash) vehicles
			        FROM trip_events t
			        JOIN anpr_camera_events a ON a.vehicle_id_hash = t.vehicle_id_hash AND a.city_code = t.city_code
			        WHERE t.event_date >= $1
			        GROUP BY t.city_code ORDER BY t.city_code`,
				[]any{randomPastDate(rng, 30)}
		},
	}

	for i := 0; i < 40; i++ {
		q, args := transactional[rng.Intn(len(transactional))]()
		if _, err := conn.Exec(ctx, q, args...); err != nil {
			return fmt.Errorf("transactional query: %w", err)
		}
	}
	for i := 0; i < 15; i++ {
		q, args := analytical[rng.Intn(len(analytical))]()
		if _, err := conn.Exec(ctx, q, args...); err != nil {
			return fmt.Errorf("analytical query: %w", err)
		}
	}

	return nil
}
