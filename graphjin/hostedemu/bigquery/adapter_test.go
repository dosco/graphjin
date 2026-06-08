package bigquery

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/hostedemu"
)

func TestBigQuerySimulatorFileBackedPersistence(t *testing.T) {
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "seed.sql")
	if err := os.WriteFile(seedPath, []byte(`
CREATE TABLE roast_batches (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  batch_code STRING NOT NULL
);

INSERT INTO roast_batches (id, batch_code) VALUES
  (1001, 'RB-2026-0605-001'),
  (1002, 'RB-2026-0605-002'),
  (1003, 'RB-2026-0605-003');
`), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	dbPath := filepath.Join(dir, "warehouse.duckdb")

	open := func() *sql.DB {
		t.Helper()
		db := sql.OpenDB(hostedemu.NewConnector(hostedemu.Config{
			SeedPath: seedPath,
			DBPath:   dbPath,
			Backend:  hostedemu.BackendDuckDB,
			Fallback: hostedemu.FallbackStrict,
			TestName: "coffee-roastery",
		}, NewAdapter()))
		if err := db.Ping(); err != nil {
			db.Close() //nolint:errcheck
			t.Fatalf("ping simulator: %v", err)
		}
		return db
	}

	db := open()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM roast_batches").Scan(&count); err != nil {
		t.Fatalf("query first simulator: %v", err)
	}
	if count != 3 {
		t.Fatalf("first count = %d, want 3", count)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close first simulator: %v", err)
	}

	db = open()
	if err := db.QueryRow("SELECT COUNT(*) FROM roast_batches").Scan(&count); err != nil {
		t.Fatalf("query second simulator: %v", err)
	}
	if count != 3 {
		t.Fatalf("second count = %d, want 3", count)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close second simulator: %v", err)
	}
}

func TestBigQuerySimulatorDirectDDL(t *testing.T) {
	seedPath := filepath.Join(t.TempDir(), "seed.sql")
	if err := os.WriteFile(seedPath, []byte(`
CREATE TABLE users (
  id INT64 NOT NULL,
  name STRING,
  PRIMARY KEY (id) NOT ENFORCED
);
`), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	db := sql.OpenDB(hostedemu.NewConnector(hostedemu.Config{
		SeedPath: seedPath,
		Backend:  hostedemu.BackendDuckDB,
		Fallback: hostedemu.FallbackStrict,
		TestName: "bigquery-ddl",
	}, NewAdapter()))
	defer db.Close() //nolint:errcheck

	if err := db.Ping(); err != nil {
		t.Fatalf("ping simulator: %v", err)
	}

	ddl := `
CREATE TABLE ` + "`orders`" + ` (
  ` + "`id`" + ` INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  ` + "`user_id`" + ` INT64 REFERENCES ` + "`users`" + `(` + "`id`" + `) NOT ENFORCED,
  ` + "`tags`" + ` ARRAY<STRING>
) CLUSTER BY ` + "`id`" + `;
`
	lowered := lowerBigQueryDirect(ddl)
	for _, bad := range []string{"NOT ENFORCED", "INT64", "STRING", "ARRAY<"} {
		if strings.Contains(strings.ToUpper(lowered), bad) {
			t.Fatalf("lowered DDL still contains %q:\n%s", bad, lowered)
		}
	}

	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("exec BigQuery DDL through simulator: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM `orders`").Scan(&count); err != nil {
		t.Fatalf("query created table: %v", err)
	}
}
