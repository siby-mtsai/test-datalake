// Package parquetw writes exported rows to a local Parquet file before upload.
package parquetw

import (
	"github.com/parquet-go/parquet-go"

	"mtsai-datalake-export/internal/model"
)

func WriteTripEvents(path string, rows []model.TripEvent) error {
	return parquet.WriteFile(path, rows)
}
