package cassandradriver

import (
	"context"
	"database/sql/driver"
	"io"
	"testing"
)

func drain(t *testing.T, rows driver.Rows) [][]any {
	t.Helper()
	cols := rows.Columns()
	var out [][]any
	for {
		dest := make([]driver.Value, len(cols))
		err := rows.Next(dest)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		row := make([]any, len(dest))
		for i, v := range dest {
			row[i] = v
		}
		out = append(out, row)
	}
	return out
}

func introspectExecutor() *fakeExecutor {
	return &fakeExecutor{rows: map[string][]map[string]any{
		"system.local": {{"release_version": "5.0.8"}},
		"system_schema.columns": {
			{"table_name": "users", "column_name": "id", "kind": "partition_key", "position": 0, "type": "uuid", "clustering_order": "none"},
			{"table_name": "users", "column_name": "name", "kind": "regular", "position": -1, "type": "text", "clustering_order": "none"},
			{"table_name": "posts", "column_name": "user_id", "kind": "partition_key", "position": 0, "type": "uuid", "clustering_order": "none"},
			{"table_name": "posts", "column_name": "created_at", "kind": "clustering", "position": 0, "type": "timestamp", "clustering_order": "desc"},
			{"table_name": "posts", "column_name": "tags", "kind": "regular", "position": -1, "type": "set<text>", "clustering_order": "none"},
		},
	}}
}

func TestIntrospectInfo(t *testing.T) {
	conn := &Conn{exec: introspectExecutor(), keyspace: "app"}
	rows, err := conn.introspect(context.Background(), &QueryDSL{Operation: OpIntrospectInfo})
	if err != nil {
		t.Fatalf("introspect info: %v", err)
	}
	data := drain(t, rows)
	if len(data) != 1 || data[0][0] != 5 || data[0][1] != "app" || data[0][2] != "app" {
		t.Fatalf("unexpected info: %#v", data)
	}
}

func TestIntrospectColumns(t *testing.T) {
	conn := &Conn{exec: introspectExecutor(), keyspace: "app"}
	rows, err := conn.introspect(context.Background(), &QueryDSL{Operation: OpIntrospectColumns})
	if err != nil {
		t.Fatalf("introspect columns: %v", err)
	}
	data := drain(t, rows)
	if len(data) != 5 {
		t.Fatalf("expected 5 column rows, got %d", len(data))
	}
	// Index by table.column for assertions.
	idx := map[string][]any{}
	for _, r := range data {
		idx[r[1].(string)+"."+r[2].(string)] = r
	}
	// users.id: uuid, primary key, not null.
	if r := idx["users.id"]; r[3] != "uuid" || r[5] != true || r[4] != true {
		t.Fatalf("users.id row wrong: %#v", r)
	}
	// users.name: not a key, nullable.
	if r := idx["users.name"]; r[5] != false || r[4] != false {
		t.Fatalf("users.name row wrong: %#v", r)
	}
	// posts.created_at: clustering key, timestamptz.
	if r := idx["posts.created_at"]; r[3] != "timestamptz" || r[5] != true {
		t.Fatalf("posts.created_at row wrong: %#v", r)
	}
	// posts.tags: set<text> → array.
	if r := idx["posts.tags"]; r[7] != true || r[3] != "text" {
		t.Fatalf("posts.tags row wrong: %#v", r)
	}
}

func TestIntrospectKeys(t *testing.T) {
	conn := &Conn{exec: introspectExecutor(), keyspace: "app"}
	rows, err := conn.introspect(context.Background(), &QueryDSL{Operation: OpIntrospectKeys})
	if err != nil {
		t.Fatalf("introspect keys: %v", err)
	}
	data := drain(t, rows)
	// Only key columns: users.id, posts.user_id, posts.created_at.
	if len(data) != 3 {
		t.Fatalf("expected 3 key rows, got %d: %#v", len(data), data)
	}
	// Ordered by table then key position: posts before users; within posts, partition (pos 0) before clustering (pos 0) is stable by source order.
	got := map[string]string{}
	for _, r := range data {
		got[r[0].(string)+"."+r[1].(string)] = r[2].(string)
	}
	if got["users.id"] != "partition_key" || got["posts.user_id"] != "partition_key" || got["posts.created_at"] != "clustering" {
		t.Fatalf("key roles wrong: %#v", got)
	}
}

func TestCQLTypeToSQL(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		isArray bool
	}{
		{"text", "text", false},
		{"uuid", "uuid", false},
		{"bigint", "bigint", false},
		{"counter", "bigint", false},
		{"decimal", "numeric", false},
		{"timestamp", "timestamptz", false},
		{"blob", "bytea", false},
		{"list<int>", "integer", true},
		{"set<text>", "text", true},
		{"map<text, int>", "json", false},
		{"frozen<list<text>>", "text", true},
	}
	for _, tc := range cases {
		st, arr := cqlTypeToSQL(tc.in)
		if st != tc.want || arr != tc.isArray {
			t.Fatalf("cqlTypeToSQL(%s) = (%s,%v), want (%s,%v)", tc.in, st, arr, tc.want, tc.isArray)
		}
	}
}
