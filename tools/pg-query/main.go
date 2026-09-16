// Command pg-query runs one SQL statement (passed as argv[1]) against PG* env vars and prints
// the result as a simple text table. No psql client exists in this environment - this is a
// minimal stand-in, used for Phase 0 discovery queries against mtsai-api-sim.
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
	if len(os.Args) != 2 {
		log.Fatal("usage: pg-query \"<sql>\"")
	}

	ctx := context.Background()
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=require",
		os.Getenv("PGUSER"), os.Getenv("PGPASSWORD"), os.Getenv("PGHOST"), os.Getenv("PGPORT"), os.Getenv("PGDATABASE"))

	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		log.Fatalf("connecting: %v", err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, os.Args[1])
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
