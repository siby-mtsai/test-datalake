// Package model holds the one table this v1 slice of the export job knows how to handle:
// trip_events, a synthetic fixture standing in for a real mtsai-api table until Phase 0
// discovery happens. Adding a second real table means adding its own typed struct here (and
// its own reader/writer wiring) - see export/README.md for why this isn't schema-generic yet.
//
// No parquet struct tags here - see internal/parquetw for why EventDate needs its own
// Parquet-specific representation and a separate struct.
package model

import "time"

type TripEvent struct {
	TripID        int64
	VehicleIDHash string
	CityCode      string
	EventDate     time.Time
	DistanceKM    float64
	FareAmount    float64
	CreatedAt     time.Time
}
