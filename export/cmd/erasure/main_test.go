package main

import (
	"strings"
	"testing"
)

// setBaseEnv sets the non-erasure settings loadConfig requires, so each case only varies the
// three ERASURE_* inputs that now come from a UI.
func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MTSAI_DATALAKE_BUCKET", "mtsai-datalake-test-690293068614-ap-south-1")
	t.Setenv("MTSAI_DATALAKE_RAW_DATABASE", "test_raw")
	t.Setenv("MTSAI_DATALAKE_CURATED_DATABASE", "test_curated")
	t.Setenv("MTSAI_DATALAKE_WORKGROUP", "mtsai-datalake-test-pipeline")
	t.Setenv("AWS_REGION", "")
}

func TestLoadConfigErasureInputs(t *testing.T) {
	cases := []struct {
		name         string
		identifier   string
		requestRef   string
		jurisdiction string
		wantErr      string // substring; empty means the config must load
	}{
		{name: "valid, hashed id, no jurisdiction", identifier: "hash_veh_000067", requestRef: "REQ-2026-0001"},
		{name: "valid, hex id, jurisdiction", identifier: "c51e9a0a77", requestRef: "req.2026_09.25", jurisdiction: "IN"},
		{name: "valid, max-length id", identifier: strings.Repeat("a", 128), requestRef: "abc"},

		{name: "missing identifier", identifier: "", requestRef: "REQ-1", wantErr: "both required"},
		{name: "missing request ref", identifier: "hash_veh_1", requestRef: "", wantErr: "both required"},
		{name: "SQL injection in identifier", identifier: "x' OR '1'='1", requestRef: "REQ-1", wantErr: "ERASURE_IDENTIFIER"},
		{name: "identifier with space", identifier: "hash veh", requestRef: "REQ-1", wantErr: "ERASURE_IDENTIFIER"},
		{name: "identifier with hyphen", identifier: "hash-veh", requestRef: "REQ-1", wantErr: "ERASURE_IDENTIFIER"},
		{name: "identifier too long", identifier: strings.Repeat("a", 129), requestRef: "REQ-1", wantErr: "ERASURE_IDENTIFIER"},
		{name: "request ref path traversal", identifier: "hash_veh_1", requestRef: "../../raw/x", wantErr: "ERASURE_REQUEST_REF"},
		{name: "request ref too short", identifier: "hash_veh_1", requestRef: "ab", wantErr: "ERASURE_REQUEST_REF"},
		{name: "request ref too long", identifier: "hash_veh_1", requestRef: strings.Repeat("r", 65), wantErr: "ERASURE_REQUEST_REF"},
		{name: "request ref with slash", identifier: "hash_veh_1", requestRef: "REQ/1", wantErr: "ERASURE_REQUEST_REF"},
		{name: "jurisdiction lowercase", identifier: "hash_veh_1", requestRef: "REQ-1", jurisdiction: "in", wantErr: "ERASURE_JURISDICTION"},
		{name: "jurisdiction three letters", identifier: "hash_veh_1", requestRef: "REQ-1", jurisdiction: "IND", wantErr: "ERASURE_JURISDICTION"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setBaseEnv(t)
			t.Setenv("ERASURE_IDENTIFIER", tc.identifier)
			t.Setenv("ERASURE_REQUEST_REF", tc.requestRef)
			t.Setenv("ERASURE_JURISDICTION", tc.jurisdiction)

			cfg, err := loadConfig()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected config to load, got %v", err)
				}
				if cfg.Identifier != tc.identifier || cfg.RequestRef != tc.requestRef || cfg.Jurisdiction != tc.jurisdiction {
					t.Fatalf("config fields not carried through: %+v", cfg)
				}
				if cfg.Region != "ap-south-1" {
					t.Fatalf("expected default region ap-south-1, got %q", cfg.Region)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil (cfg=%+v)", tc.wantErr, cfg)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestLoadConfigRequiresLakeSettings(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("MTSAI_DATALAKE_BUCKET", "")
	t.Setenv("ERASURE_IDENTIFIER", "hash_veh_1")
	t.Setenv("ERASURE_REQUEST_REF", "REQ-1")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected an error when MTSAI_DATALAKE_BUCKET is missing")
	}
}
