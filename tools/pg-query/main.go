// Command pg-query runs one SQL statement (passed as argv[1]) against PG* env vars and prints
// the result as a simple text table. No psql client exists in this environment - this is a
// minimal stand-in, used for Phase 0 discovery queries against mtsai-api-sim, and for running the
// one-time migration scripts under sql/postgres/.
//
// `pg-query -f <path>` instead runs the whole file's contents as one multi-statement batch via
// Exec (Postgres's simple query protocol executes a semicolon-separated batch as one implicit
// transaction, or respects explicit BEGIN/COMMIT inside it) - needed for migrations like
// sql/postgres/partition_trip_events.sql's step 5 swap, which must be atomic.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: pg-query \"<sql>\"  |  pg-query -f <path>")
	}

	ctx := context.Background()
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=require",
		os.Getenv("PGUSER"), os.Getenv("PGPASSWORD"), os.Getenv("PGHOST"), os.Getenv("PGPORT"), os.Getenv("PGDATABASE"))

	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		log.Fatalf("connecting: %v", err)
	}
	defer conn.Close(ctx)

	if os.Args[1] == "-f" {
		if len(os.Args) != 3 {
			log.Fatal("usage: pg-query -f <path>")
		}
		runFile(ctx, conn, os.Args[2])
		return
	}

	runQuery(ctx, conn, os.Args[1])
}

func runFile(ctx context.Context, conn *pgx.Conn, path string) {
	body, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("reading %s: %v", path, err)
	}
	tag, err := conn.Exec(ctx, string(body))
	if err != nil {
		log.Fatalf("exec %s: %v", path, err)
	}
	fmt.Fprintf(os.Stderr, "OK: %s\n", tag.String())
}

func runQuery(ctx context.Context, conn *pgx.Conn, sql string) {
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		log.Fatalf("query: %v", err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	fmt.Println(strings.Join(names, "\t"))

	count := 0
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			log.Fatalf("scan: %v", err)
		}
		strs := make([]string, len(vals))
		for i, v := range vals {
			strs[i] = fmt.Sprintf("%v", v)
		}
		fmt.Println(strings.Join(strs, "\t"))
		count++
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("iterating: %v", err)
	}
	fmt.Fprintf(os.Stderr, "(%d rows)\n", count)
}
