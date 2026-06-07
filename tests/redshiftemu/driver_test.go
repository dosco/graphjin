package redshiftemu

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestConnectorSatisfiesRedshiftDiscovery(t *testing.T) {
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "redshift.sql")
	if err := os.WriteFile(seedPath, []byte(`
CREATE TABLE public.users (
  id BIGINT IDENTITY(1,1) PRIMARY KEY,
  email VARCHAR(255) ENCODE lzo NOT NULL,
  profile SUPER,
  created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
) DISTSTYLE EVEN COMPOUND SORTKEY(created_at);
`), 0644); err != nil {
		t.Fatal(err)
	}

	db := sql.OpenDB(NewConnector(Config{
		SeedPath: seedPath,
		Backend:  BackendDuckDB,
		Fallback: FallbackStrict,
		TestName: "redshift-wrapper",
		RunID:    "unit",
	}))
	defer db.Close()

	var tableName string
	if err := db.QueryRowContext(context.Background(),
		`SHOW TABLES FROM SCHEMA dev.public LIKE 'users'`,
	).Scan(new(string), new(string), &tableName, new(string), new(string), new(string), new(string), new(any), new(any), new(string), new(string)); err != nil {
		t.Fatal(err)
	}
	if tableName != "users" {
		t.Fatalf("SHOW TABLES table_name = %q, want users", tableName)
	}
}

func TestGraphJinRedshiftQuerySmoke(t *testing.T) {
	db := sql.OpenDB(NewConnector(Config{
		SeedPath: "../redshift.sql",
		Backend:  BackendDuckDB,
		Fallback: FallbackStrict,
		TestName: "redshift-query-smoke",
		RunID:    "unit",
	}))
	defer db.Close()

	gj, err := core.NewGraphJin(&core.Config{
		DBType:               "redshift",
		DisableAllowList:     true,
		DBSchemaPollDuration: -1,
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(), `
	query {
		users(limit: 1, order_by: { id: asc }) {
			id
			email
		}
	}`, nil, nil)
	if err != nil {
		t.Fatalf("%v\nSQL:\n%s", err, res.SQL())
	}
	if !bytes.Contains(res.Data, []byte(`"users"`)) || !bytes.Contains(res.Data, []byte(`"email":"ada@example.com"`)) {
		t.Fatalf("unexpected result: %s", res.Data)
	}
}
