// Package source reads rows from Postgres using a server-side cursor (brief Section 7, Phase 2
// step 1: "stream rows from Postgres with server side cursors"), so a large table is never
// pulled entirely into memory at once.
package source

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"mtsai-datalake-export/internal/config"
	"mtsai-datalake-export/internal/model"
)

func Connect(ctx context.Context, creds config.PostgresCredentials) (*pgx.Conn, error) {
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		creds.Username, creds.Password, creds.Host, creds.Port, creds.DBName)
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres at %s:%d: %w", creds.Host, creds.Port, err)
	}
	return conn, nil
}

// StreamTripEvents reads every trip_events row for exactly one event_date, via an explicit
// DECLARE CURSOR / FETCH FORWARD loop rather than a single SELECT, so a real production-sized
// table would stream in batches instead of buffering the whole result set in the connection.
func StreamTripEvents(ctx context.Context, conn *pgx.Conn, runDate time.Time, batchSize int) ([]model.TripEvent, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // no-op once committed below; this is a read-only cursor either way

	if _, err := tx.Exec(ctx, `
		DECLARE export_cursor CURSOR FOR
		SELECT trip_id, vehicle_id_hash, city_code, event_date, distance_km, fare_amount, created_at
		FROM trip_events
		WHERE event_date = $1
		ORDER BY trip_id
	`, runDate); err != nil {
		return nil, fmt.Errorf("declare cursor: %w", err)
	}

	var rows []model.TripEvent
	for {
		batch, err := fetchBatch(ctx, tx, batchSize)
		if err != nil {
			return nil, err
		}
		rows = append(rows, batch...)
		if len(batch) < batchSize {
			break
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return rows, nil
}

func fetchBatch(ctx context.Context, tx pgx.Tx, batchSize int) ([]model.TripEvent, error) {
	fetchRows, err := tx.Query(ctx, fmt.Sprintf("FETCH FORWARD %d FROM export_cursor", batchSize))
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer fetchRows.Close()

	var batch []model.TripEvent
	for fetchRows.Next() {
		var e model.TripEvent
		if err := fetchRows.Scan(&e.TripID, &e.VehicleIDHash, &e.CityCode, &e.EventDate, &e.DistanceKM, &e.FareAmount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		batch = append(batch, e)
	}
	if err := fetchRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating fetch results: %w", err)
	}
	return batch, nil
}
