package bigquerylive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
	"go.uber.org/zap"
)

// TestServeOpensLiveBigQuery proves the path `deeporg serve` takes for a
// `type: bigquery` source against the real service: the server's own driver
// init, schema discovery, and a compiled GraphQL query with a parameter.
//
// It creates a dataset named gj_serve_live_<time>_<pid> in the project, and
// drops it when the test ends. It uses Application Default Credentials, the
// way the server does.
func TestServeOpensLiveBigQuery(t *testing.T) {
	project := strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_PROJECT"))
	if project == "" {
		t.Skip("live BigQuery test: set GRAPHJIN_BIGQUERY_PROJECT to a Google Cloud project it may create a temporary dataset in")
	}
	location := strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_LOCATION"))
	if location == "" {
		location = "US"
	}
	dataset := fmt.Sprintf("gj_serve_live_%d_%d", time.Now().Unix(), os.Getpid())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	svc, err := NewService(ctx)
	if err != nil {
		t.Fatalf("bigquery service for setup: %v", err)
	}
	if err := CreateDataset(ctx, svc, project, dataset, location); err != nil {
		t.Fatalf("create dataset %s: %v", dataset, err)
	}
	t.Cleanup(func() {
		dropCtx, done := context.WithTimeout(context.Background(), time.Minute)
		defer done()
		if err := DropDataset(dropCtx, svc, project, dataset); err != nil {
			t.Errorf("LEFT BEHIND: dataset %s.%s was not dropped: %v", project, dataset, err)
		}
	})

	seed := filepath.Join(t.TempDir(), "seed.sql")
	if err := os.WriteFile(seed, []byte(`
CREATE TABLE users (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  email STRING NOT NULL,
  zip STRING,
  created_at TIMESTAMP NOT NULL
);

INSERT INTO users (id, email, zip, created_at) VALUES
  (1, 'ada@example.com', '02134', TIMESTAMP '2026-01-02 03:04:05+00'),
  (2, 'grace@example.com', '94110', TIMESTAMP '2026-02-03 04:05:06+00'),
  (3, 'linus@example.com', NULL, TIMESTAMP '2026-03-04 05:06:07+00');
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SeedFile(ctx, svc, project, dataset, location, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// From here on, only what the server does. No location in the connection
	// string: the driver has to read it from the dataset.
	conf := &serv.Config{}
	conf.DB.Type = "bigquery"
	conf.DB.ConnString = fmt.Sprintf("bigquery://%s/%s", project, dataset)
	db, err := serv.NewDB(conf, true, zap.NewNop().Sugar(), core.NewOsFS(t.TempDir()))
	if err != nil {
		t.Fatalf("serv.NewDB: %v", err)
	}
	defer db.Close() //nolint:errcheck

	// A STRING of digits must come back as text, and a TIMESTAMP exactly.
	var zip string
	var createdAt time.Time
	if err := db.QueryRowContext(ctx, "SELECT zip, created_at FROM users WHERE id = ?", 1).Scan(&zip, &createdAt); err != nil {
		t.Fatalf("direct query: %v", err)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); zip != "02134" || !createdAt.Equal(want) {
		t.Errorf("zip, created_at = %q, %s; want 02134, %s", zip, createdAt, want)
	}

	gj, err := core.NewGraphJin(&core.Config{DBType: "bigquery", DisableAllowList: true}, db)
	if err != nil {
		t.Fatalf("core.NewGraphJin (schema discovery): %v", err)
	}
	defer gj.Close()

	users, err := gj.GetTableSchema("users")
	if err != nil {
		t.Fatalf("discovered tables do not include users: %v", err)
	}
	if users.PrimaryKey != "id" {
		t.Errorf("users primary key = %q, want id", users.PrimaryKey)
	}

	res, err := gj.GraphQL(ctx, `query {
		users(where: { id: { gt: $after } }, order_by: { id: asc }, limit: 2) { id email zip }
	}`, json.RawMessage(`{"after": 1}`), nil)
	if err != nil {
		t.Fatalf("graphql: %v\nSQL:\n%s", err, res.SQL())
	}
	var out struct {
		Users []struct {
			ID    int64   `json:"id"`
			Email string  `json:"email"`
			Zip   *string `json:"zip"`
		} `json:"users"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, res.Data)
	}
	if len(out.Users) != 2 || out.Users[0].ID != 2 || out.Users[0].Email != "grace@example.com" ||
		out.Users[1].ID != 3 || out.Users[1].Zip != nil {
		t.Errorf("unexpected result: %s", res.Data)
	}

	// Row counts come from a per-region view that lags by minutes and needs
	// its own permission, so only the query's validity is reported here.
	var n int64
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLE_STORAGE WHERE TABLE_TYPE = 'BASE TABLE' AND LOWER(TABLE_SCHEMA) = LOWER(?)`, dataset).Scan(&n)
	t.Logf("TABLE_STORAGE row-count lookup: tables=%d err=%v", n, err)
}
