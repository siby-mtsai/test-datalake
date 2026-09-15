// Package parquetw writes exported rows to a local Parquet file before upload.
package parquetw

import (
	"time"

	"github.com/parquet-go/parquet-go"

	"mtsai-datalake-export/internal/model"
)

// tripEventRow mirrors model.TripEvent but stores EventDate as days-since-epoch (int32) instead
// of time.Time. parquet-go's reflection-based time.Time writer only correctly handles the
// Timestamp logical type (physical int64) - for Date (physical int32) it still encodes a raw
// nanosecond int64 value into the int32 column slot, producing garbage. Confirmed by writing
// EventDate as time.Time with a `,date` tag and reading the result back via Athena: it came back
// as "-1454296-02-29" instead of "2026-01-15". CreatedAt (a genuine Timestamp column, physical
// int64) round-tripped correctly, which is what narrowed this down to the Date-specific path.
type tripEventRow struct {
	TripID        int64     `parquet:"trip_id"`
	VehicleIDHash string    `parquet:"vehicle_id_hash"`
	CityCode      string    `parquet:"city_code"`
	EventDate     int32     `parquet:"event_date,date"`
	DistanceKM    float64   `parquet:"distance_km"`
	FareAmount    float64   `parquet:"fare_amount"`
	CreatedAt     time.Time `parquet:"created_at,timestamp"`
}

func daysSinceEpoch(t time.Time) int32 {
	return int32(t.UTC().Truncate(24*time.Hour).Unix() / 86400)
}

func WriteTripEvents(path string, rows []model.TripEvent) error {
	converted := make([]tripEventRow, len(rows))
	for i, r := range rows {
		converted[i] = tripEventRow{
			TripID:        r.TripID,
			VehicleIDHash: r.VehicleIDHash,
			CityCode:      r.CityCode,
			EventDate:     daysSinceEpoch(r.EventDate),
			DistanceKM:    r.DistanceKM,
			FareAmount:    r.FareAmount,
			CreatedAt:     r.CreatedAt,
		}
	}
	return parquet.WriteFile(path, converted)
}
