// Package model holds the one table this v1 slice of the export job knows how to handle:
// trip_events, a synthetic fixture standing in for a real mtsai-api table until Phase 0
// discovery happens. Adding a second real table means adding its own typed struct here (and
// its own reader/writer wiring) - see export/README.md for why this isn't schema-generic yet.
package model

import "time"

type TripEvent struct {
	TripID        int64     `parquet:"trip_id"`
	VehicleIDHash string    `parquet:"vehicle_id_hash"`
	CityCode      string    `parquet:"city_code"`
	EventDate     time.Time `parquet:"event_date,date"`
	DistanceKM    float64   `parquet:"distance_km"`
	FareAmount    float64   `parquet:"fare_amount"`
	CreatedAt     time.Time `parquet:"created_at,timestamp"`
}
