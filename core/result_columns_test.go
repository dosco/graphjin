package core

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// Columns describes a result from the compiled query and the schema: keys,
// references, measures (aggregates included) and text, with database types.
func TestResultColumnsDescribeTheQuery(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE plans (id INTEGER PRIMARY KEY, name TEXT);
CREATE TABLE accounts (id INTEGER PRIMARY KEY, name TEXT, plan_id INTEGER REFERENCES plans(id), mrr_cents INTEGER, renews_at TIMESTAMP);
INSERT INTO plans VALUES (1, 'growth');
INSERT INTO accounts VALUES (1, 'Meridian', 1, 480000, '2026-12-01T00:00:00Z');`); err != nil {
		t.Fatal(err)
	}
	gj, err := NewGraphJin(&Config{DBType: "sqlite", DisableAllowList: true}, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gj.Close)
	res, err := gj.GraphQL(context.Background(), `{ accounts { id name plan_id mrr_cents renews_at plan { name } } }`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{}
	for _, c := range res.Columns() {
		if c.Root != "accounts" {
			t.Fatalf("column %+v has root %q", c, c.Root)
		}
		roles[c.Path] = c.Role
	}
	for path, want := range map[string]string{
		"accounts.id": "key", "accounts.name": "text", "accounts.plan_id": "reference",
		"accounts.mrr_cents": "measure", "accounts.renews_at": "time", "accounts.plan.name": "text",
	} {
		if roles[path] != want {
			t.Errorf("%s role = %q, want %q (all: %v)", path, roles[path], want, roles)
		}
	}
	agg, err := gj.GraphQL(context.Background(), `{ accounts { count_id sum_mrr_cents } }`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range agg.Columns() {
		if c.Role != "measure" || (c.Aggregate != "count" && c.Aggregate != "sum") {
			t.Errorf("aggregate column = %+v", c)
		}
	}
	if mut, err := gj.GraphQL(context.Background(), `mutation { plans(insert: { id: 2, name: "pro" }) { id } }`, nil, nil); err == nil && len(mut.Columns()) != 0 {
		t.Errorf("a mutation result is not described: %+v", mut.Columns())
	}
}
