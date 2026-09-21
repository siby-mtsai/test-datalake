// Package config loads the export job's runtime configuration: the fixed environment contract
// injected by terraform/modules/export-task (env vars + the POSTGRES_CREDENTIALS secret), plus
// the human-edited table-list file (tables.yaml).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type ColumnType string

const (
	ColBigInt    ColumnType = "bigint"
	ColString    ColumnType = "string"
	ColDate      ColumnType = "date"
	ColDouble    ColumnType = "double"
	ColTimestamp ColumnType = "timestamp"
)

type Column struct {
	Name string     `yaml:"name"`
	Type ColumnType `yaml:"type"`
}

type Table struct {
	Name             string   `yaml:"name"`
	PartitionColumns []string `yaml:"partition_columns"`
	PrimaryKey       string   `yaml:"primary_key"`
	Columns          []Column `yaml:"columns"`
}

type tablesFile struct {
	Tables []Table `yaml:"tables"`
}

// PostgresCredentials mirrors the shape AWS Secrets Manager uses for an RDS-managed secret.
type PostgresCredentials struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DBName   string `json:"dbname"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Config struct {
	Environment string
	Bucket      string
	RawDatabase string
	Region      string
	WorkGroup   string
	RunDate     time.Time
	RunDates    []time.Time // set instead of RunDate when EXPORT_START_DATE/EXPORT_END_DATE are used (backfill mode)
	Postgres    PostgresCredentials
	Tables      []Table
}

// Load reads the fixed env-var contract terraform/modules/export-task injects, plus the
// table-list YAML file. envLookup/fileRead are injected only so tests can avoid touching the
// real OS environment; production code should call LoadFromEnv.
func Load(getenv func(string) string, readFile func(string) ([]byte, error)) (*Config, error) {
	cfg := &Config{
		Environment: getenv("MTSAI_DATALAKE_ENV"),
		Bucket:      getenv("MTSAI_DATALAKE_BUCKET"),
		RawDatabase: getenv("MTSAI_DATALAKE_RAW_DATABASE"),
		Region:      getenv("AWS_REGION"),
		WorkGroup:   getenv("MTSAI_DATALAKE_WORKGROUP"),
	}
	if cfg.Environment == "" || cfg.Bucket == "" || cfg.RawDatabase == "" {
		return nil, fmt.Errorf("MTSAI_DATALAKE_ENV, MTSAI_DATALAKE_BUCKET, and MTSAI_DATALAKE_RAW_DATABASE are all required")
	}
	if cfg.Region == "" {
		cfg.Region = "ap-south-1"
	}

	rawCreds := getenv("POSTGRES_CREDENTIALS")
	if rawCreds == "" {
		return nil, fmt.Errorf("POSTGRES_CREDENTIALS is required")
	}
	if err := json.Unmarshal([]byte(rawCreds), &cfg.Postgres); err != nil {
		return nil, fmt.Errorf("parsing POSTGRES_CREDENTIALS: %w", err)
	}

	configPath := getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/etc/mtsai-datalake-export/tables.yaml"
	}
	raw, err := readFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading table config %s: %w", configPath, err)
	}
	var tf tablesFile
	if err := yaml.Unmarshal(raw, &tf); err != nil {
		return nil, fmt.Errorf("parsing table config %s: %w", configPath, err)
	}
	if len(tf.Tables) == 0 {
		return nil, fmt.Errorf("table config %s defines no tables", configPath)
	}
	cfg.Tables = tf.Tables

	runDate := getenv("EXPORT_DATE") // override for manual runs; defaults to yesterday UTC
	startDate := getenv("EXPORT_START_DATE")
	endDate := getenv("EXPORT_END_DATE")

	switch {
	case (startDate == "") != (endDate == ""):
		return nil, fmt.Errorf("EXPORT_START_DATE and EXPORT_END_DATE must both be set together")
	case runDate != "" && startDate != "":
		return nil, fmt.Errorf("EXPORT_DATE cannot be combined with EXPORT_START_DATE/EXPORT_END_DATE")
	case startDate != "":
		start, err := time.Parse("2006-01-02", startDate)
		if err != nil {
			return nil, fmt.Errorf("EXPORT_START_DATE must be YYYY-MM-DD, got %q: %w", startDate, err)
		}
		end, err := time.Parse("2006-01-02", endDate)
		if err != nil {
			return nil, fmt.Errorf("EXPORT_END_DATE must be YYYY-MM-DD, got %q: %w", endDate, err)
		}
		if end.Before(start) {
			return nil, fmt.Errorf("EXPORT_END_DATE (%s) is before EXPORT_START_DATE (%s)", endDate, startDate)
		}
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			cfg.RunDates = append(cfg.RunDates, d)
		}
	case runDate == "":
		cfg.RunDate = time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	default:
		d, err := time.Parse("2006-01-02", runDate)
		if err != nil {
			return nil, fmt.Errorf("EXPORT_DATE must be YYYY-MM-DD, got %q: %w", runDate, err)
		}
		cfg.RunDate = d
	}

	return cfg, nil
}

// LoadFromEnv is the production entrypoint: real OS env vars and filesystem.
func LoadFromEnv() (*Config, error) {
	return Load(os.Getenv, os.ReadFile)
}
