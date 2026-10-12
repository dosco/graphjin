package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// Exercise the whole parser/compiler/execution path with events before, on,
// inside, and after the bounds. A dropped bound or an OR yields a wrong count.
func TestDateWindowMultipleOperatorsSQLite(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE usage_events (id INTEGER PRIMARY KEY, occurred_at TEXT NOT NULL);
INSERT INTO usage_events VALUES
(1, '2026-10-06T11:59:59Z'), (2, '2026-10-06T12:00:00Z'),
(3, '2026-10-08T12:00:00Z'), (4, '2026-10-11T12:00:00Z'),
(5, '2026-10-11T12:00:01Z');`)
	if err != nil {
		t.Fatal(err)
	}
	gj, err := NewGraphJin(&Config{DBType: "sqlite", DisableAllowList: true, DefaultLimit: 1}, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gj.Close)
	for _, tc := range []struct {
		name, query string
		vars        json.RawMessage
		want        int
	}{
		{"inclusive", `{ usage_events(where: { occurred_at: { gte: "2026-10-06T12:00:00Z", lte: "2026-10-11T12:00:00Z" } }) { count_id } }`, nil, 3},
		{"exclusive", `{ usage_events(where: { occurred_at: { gt: "2026-10-06T12:00:00Z", lt: "2026-10-11T12:00:00Z" } }) { count_id } }`, nil, 1},
		{"variables", `query($from: String!, $to: String!) { usage_events(where: { occurred_at: { _gte: $from, _lte: $to } }) { count_id } }`, json.RawMessage(`{"from":"2026-10-06T12:00:00Z","to":"2026-10-11T12:00:00Z"}`), 3},
		{"hasura", `{ usage_events_aggregate(where: { occurred_at: { gte: "2026-10-06T12:00:00Z", lte: "2026-10-11T12:00:00Z" } }) { aggregate { count } } }`, nil, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := gj.GraphQL(context.Background(), tc.query, tc.vars, nil)
			if err != nil {
				t.Fatal(err)
			}
			var data struct {
				Events []struct {
					Count int `json:"count_id"`
				} `json:"usage_events"`
				Compat struct {
					Aggregate struct {
						Count int `json:"count"`
					} `json:"aggregate"`
				} `json:"usage_events_aggregate"`
			}
			if err := json.Unmarshal(result.Data, &data); err != nil {
				t.Fatal(err)
			}
			got := data.Compat.Aggregate.Count
			if tc.name != "hasura" {
				if len(data.Events) != 1 {
					t.Fatalf("expected one aggregate row: %s", result.Data)
				}
				got = data.Events[0].Count
			}
			if got != tc.want {
				t.Fatalf("count = %d, want %d: %s\nSQL: %s", got, tc.want, result.Data, result.SQL())
			}
		})
	}
}
