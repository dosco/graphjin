package cassandradriver

import (
	"reflect"
	"testing"
)

func TestBuildSelect_Golden(t *testing.T) {
	n := postsNode()
	n.Keyspace = "app"
	n.Columns = []string{"user_id", "created_at", "id", "title"}
	n.Filters = []Filter{
		{Col: "user_id", Op: OpEq, Value: "u1"},
		{Col: "created_at", Op: OpGt, Value: int64(1000)},
	}
	n.OrderBy = []OrderBy{{Col: "created_at", Order: "desc"}}
	n.Limit = 25

	p, err := PlanRead(n)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	cql, args, err := BuildSelect(p)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := "SELECT user_id, created_at, id, title FROM app.posts WHERE user_id = ? AND created_at > ? ORDER BY created_at DESC LIMIT 25"
	if cql != want {
		t.Fatalf("CQL mismatch:\n got: %s\nwant: %s", cql, want)
	}
	if !reflect.DeepEqual(args, []any{"u1", int64(1000)}) {
		t.Fatalf("args mismatch: %#v", args)
	}
}

func TestBuildSelect_INandAllowFiltering(t *testing.T) {
	n := usersNode()
	n.AllowFiltering = true
	n.Filters = []Filter{{Col: "id", Op: OpIn, Value: []any{"a", "b", "c"}}}
	p, err := PlanRead(n)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	cql, args, err := BuildSelect(p)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := "SELECT id, name, email FROM users WHERE id IN (?, ?, ?) ALLOW FILTERING"
	if cql != want {
		t.Fatalf("CQL mismatch:\n got: %s\nwant: %s", cql, want)
	}
	if !reflect.DeepEqual(args, []any{"a", "b", "c"}) {
		t.Fatalf("args mismatch: %#v", args)
	}
}

func TestBuildInsert_Golden(t *testing.T) {
	m := &Mutation{
		Type:        OpInsert,
		Keyspace:    "app",
		Table:       "users",
		IfNotExists: true,
		Set: []Assignment{
			{Col: "id", Value: "u1"},
			{Col: "name", Value: "amit"},
		},
		PartitionKeys: []string{"id"},
	}
	cql, args, err := BuildInsert(m)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := "INSERT INTO app.users (id, name) VALUES (?, ?) IF NOT EXISTS"
	if cql != want {
		t.Fatalf("CQL mismatch:\n got: %s\nwant: %s", cql, want)
	}
	if !reflect.DeepEqual(args, []any{"u1", "amit"}) {
		t.Fatalf("args mismatch: %#v", args)
	}
}

func TestBuildUpdate_CounterAndWhereOrder(t *testing.T) {
	m := &Mutation{
		Type:           OpUpdate,
		Table:          "counts",
		PartitionKeys:  []string{"user_id"},
		ClusteringKeys: []string{"day"},
		CounterCols:    []string{"hits"},
		Set:            []Assignment{{Col: "hits", Counter: true, Value: int64(2)}},
		// WHERE given out of key order to prove deterministic ordering.
		Where: []Filter{
			{Col: "day", Op: OpEq, Value: "2026-06-04"},
			{Col: "user_id", Op: OpEq, Value: "u1"},
		},
	}
	cql, args, err := BuildUpdate(m)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := "UPDATE counts SET hits = hits + ? WHERE user_id = ? AND day = ?"
	if cql != want {
		t.Fatalf("CQL mismatch:\n got: %s\nwant: %s", cql, want)
	}
	if !reflect.DeepEqual(args, []any{int64(2), "u1", "2026-06-04"}) {
		t.Fatalf("args mismatch: %#v", args)
	}
}

func TestBuildDelete_Golden(t *testing.T) {
	m := &Mutation{
		Type:          OpDelete,
		Table:         "users",
		PartitionKeys: []string{"id"},
		IfExists:      true,
		Where:         []Filter{{Col: "id", Op: OpEq, Value: "u1"}},
	}
	cql, args, err := BuildDelete(m)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := "DELETE FROM users WHERE id = ? IF EXISTS"
	if cql != want {
		t.Fatalf("CQL mismatch:\n got: %s\nwant: %s", cql, want)
	}
	if !reflect.DeepEqual(args, []any{"u1"}) {
		t.Fatalf("args mismatch: %#v", args)
	}
}

// Read-after-write: the post-write SELECT-by-PK is the Returning node run through
// the normal planner/builder.
func TestReadAfterWrite_SelectByPK(t *testing.T) {
	ret := &Node{
		Keyspace:      "app",
		Table:         "users",
		Columns:       []string{"id", "name"},
		PartitionKeys: []string{"id"},
		Filters:       []Filter{{Col: "id", Op: OpEq, Value: "u1"}},
	}
	p, err := PlanRead(ret)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	cql, args, err := BuildSelect(p)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := "SELECT id, name FROM app.users WHERE id = ?"
	if cql != want {
		t.Fatalf("CQL mismatch:\n got: %s\nwant: %s", cql, want)
	}
	if !reflect.DeepEqual(args, []any{"u1"}) {
		t.Fatalf("args mismatch: %#v", args)
	}
}
