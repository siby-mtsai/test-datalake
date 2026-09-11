# export

Phase 2. Go export job (module `mtsai-datalake-export`, Go 1.23, `golangci-lint` in CI):
`cmd/`, `internal/`, `Dockerfile`. Reads the table list/date ranges from config, streams rows from
Postgres with server-side cursors, writes Parquet, uploads to S3, commits to Iceberg via Athena
(`INSERT INTO` for v1). Idempotent per `(table, event_date)`. Integration tests run against a
Postgres container with `CITY_ZZ` fixtures (brief Sections 7 and 8).

Not yet implemented — blocked on Phase 0/1 completion and Postgres access
(see [`../docs/STATUS.md`](../docs/STATUS.md)).
