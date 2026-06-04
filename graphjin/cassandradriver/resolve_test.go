package cassandradriver

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// fakeExecutor routes statements by table name to canned rows and records calls.
type fakeExecutor struct {
	mu      sync.Mutex
	queries []Statement
	execs   []Statement
	rows    map[string][]map[string]any // table -> rows
}

func (f *fakeExecutor) Query(_ context.Context, stmt Statement) (ResultSet, error) {
	f.mu.Lock()
	f.queries = append(f.queries, stmt)
	f.mu.Unlock()
	for tbl, rows := range f.rows {
		// Match both unqualified (FROM tbl) and keyspace-qualified (FROM ks.tbl).
		if strings.Contains(stmt.CQL, "FROM "+tbl) || strings.Contains(stmt.CQL, "."+tbl) {
			return ResultSet{Rows: rows}, nil
		}
	}
	return ResultSet{}, nil
}

func (f *fakeExecutor) Exec(_ context.Context, stmt Statement) (ResultSet, error) {
	f.mu.Lock()
	f.execs = append(f.execs, stmt)
	f.mu.Unlock()
	return ResultSet{}, nil
}

func (f *fakeExecutor) countQueries(tbl string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, q := range f.queries {
		if strings.Contains(q.CQL, "FROM "+tbl) {
			n++
		}
	}
	return n
}

func TestDeriveCardinality(t *testing.T) {
	if k, err := deriveCardinality("user_id", []string{"user_id"}, nil); err != nil || k != RelOneToOne {
		t.Fatalf("single-col PK == join col → one_to_one, got %v %v", k, err)
	}
	if k, err := deriveCardinality("user_id", []string{"user_id"}, []string{"id"}); err != nil || k != RelOneToMany {
		t.Fatalf("partition key + clustering → one_to_many, got %v %v", k, err)
	}
	if _, err := deriveCardinality("user_id", []string{"tenant", "user_id"}, nil); err == nil {
		t.Fatalf("composite partition key with non-prefix join column must reject")
	}
}

func TestResolve_OneToMany(t *testing.T) {
	root := &Node{
		Table:         "users",
		Columns:       []string{"id", "name"},
		PartitionKeys: []string{"id"},
		FieldName:     "users",
		Filters:       []Filter{{Col: "id", Op: OpEq, Value: "u1"}},
		Children: []*Node{{
			Table:          "posts",
			Columns:        []string{"user_id", "id", "title"},
			PartitionKeys:  []string{"user_id"},
			ClusteringKeys: []string{"id"},
			FieldName:      "posts",
			Rel:            &Rel{ParentCol: "id", ChildCol: "user_id"},
		}},
	}
	exec := &fakeExecutor{rows: map[string][]map[string]any{
		"users": {{"id": "u1", "name": "amit"}},
		"posts": {
			{"user_id": "u1", "id": "p1", "title": "first"},
			{"user_id": "u1", "id": "p2", "title": "second"},
		},
	}}
	r := &Resolver{Exec: exec}
	out, err := r.resolveRead(context.Background(), root)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	var res struct {
		Users []struct {
			ID    string `json:"id"`
			Posts []struct {
				Title string `json:"title"`
			} `json:"posts"`
		} `json:"users"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("unmarshal %s: %v", out, err)
	}
	if len(res.Users) != 1 || len(res.Users[0].Posts) != 2 {
		t.Fatalf("expected 1 user with 2 posts, got %s", out)
	}
}

func TestResolve_OneToOne(t *testing.T) {
	root := &Node{
		Table:         "users",
		Columns:       []string{"id", "name"},
		PartitionKeys: []string{"id"},
		FieldName:     "users",
		Singular:      true,
		Filters:       []Filter{{Col: "id", Op: OpEq, Value: "u1"}},
		Children: []*Node{{
			Table:         "profiles",
			Columns:       []string{"user_id", "bio"},
			PartitionKeys: []string{"user_id"},
			FieldName:     "profile",
			Singular:      true,
			Rel:           &Rel{ParentCol: "id", ChildCol: "user_id"}, // derived one_to_one
		}},
	}
	exec := &fakeExecutor{rows: map[string][]map[string]any{
		"users":    {{"id": "u1", "name": "amit"}},
		"profiles": {{"user_id": "u1", "bio": "hi"}},
	}}
	r := &Resolver{Exec: exec}
	out, err := r.resolveRead(context.Background(), root)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var res struct {
		Users struct {
			Profile struct {
				Bio string `json:"bio"`
			} `json:"profile"`
		} `json:"users"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("unmarshal %s: %v", out, err)
	}
	if res.Users.Profile.Bio != "hi" {
		t.Fatalf("expected nested profile, got %s", out)
	}
}

func TestResolve_ChunkedINFetch(t *testing.T) {
	users := make([]map[string]any, 5)
	for i := range users {
		users[i] = map[string]any{"id": string(rune('a' + i)), "name": "n"}
	}
	root := &Node{
		Table:         "users",
		Columns:       []string{"id", "name"},
		PartitionKeys: []string{"id"},
		FieldName:     "users",
		Filters:       []Filter{{Col: "id", Op: OpIn, Value: []any{"a", "b", "c", "d", "e"}}},
		Children: []*Node{{
			Table:          "posts",
			Columns:        []string{"user_id", "id"},
			PartitionKeys:  []string{"user_id"},
			ClusteringKeys: []string{"id"},
			FieldName:      "posts",
			Rel:            &Rel{ParentCol: "id", ChildCol: "user_id"},
		}},
	}
	exec := &fakeExecutor{rows: map[string][]map[string]any{
		"users": users,
		"posts": {},
	}}
	r := &Resolver{Exec: exec, ChunkSize: 2}
	if _, err := r.resolveRead(context.Background(), root); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// 5 distinct parent keys, chunk size 2 → ceil(5/2) = 3 child queries.
	if got := exec.countQueries("posts"); got != 3 {
		t.Fatalf("expected 3 chunked child queries, got %d", got)
	}
}

func TestResolve_RejectsCrossPartitionRelationship(t *testing.T) {
	root := &Node{
		Table:         "users",
		Columns:       []string{"id"},
		PartitionKeys: []string{"id"},
		FieldName:     "users",
		Filters:       []Filter{{Col: "id", Op: OpEq, Value: "u1"}},
		Children: []*Node{{
			Table:         "events",
			Columns:       []string{"tenant", "user_id"},
			PartitionKeys: []string{"tenant", "user_id"}, // join col is not the full partition key
			FieldName:     "events",
			Rel:           &Rel{ParentCol: "id", ChildCol: "user_id"},
		}},
	}
	exec := &fakeExecutor{rows: map[string][]map[string]any{"users": {{"id": "u1"}}}}
	r := &Resolver{Exec: exec}
	if _, err := r.resolveRead(context.Background(), root); err == nil ||
		!strings.Contains(err.Error(), "cross-partition") {
		t.Fatalf("expected cross-partition rejection, got: %v", err)
	}
}

func TestResolve_InsertReadAfterWrite(t *testing.T) {
	dsl := &QueryDSL{
		Operation: OpInsert,
		Mutation: &Mutation{
			Type:          OpInsert,
			Table:         "users",
			PartitionKeys: []string{"id"},
			Set:           []Assignment{{Col: "id", Value: "u1"}, {Col: "name", Value: "amit"}},
			Returning: &Node{
				Table:         "users",
				Columns:       []string{"id", "name"},
				PartitionKeys: []string{"id"},
				FieldName:     "user",
				Singular:      true,
				Filters:       []Filter{{Col: "id", Op: OpEq, Value: "u1"}},
			},
		},
	}
	exec := &fakeExecutor{rows: map[string][]map[string]any{
		"users": {{"id": "u1", "name": "amit"}},
	}}
	r := &Resolver{Exec: exec}
	out, err := r.ResolveQuery(context.Background(), dsl)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(exec.execs) != 1 || !strings.HasPrefix(exec.execs[0].CQL, "INSERT INTO users") {
		t.Fatalf("expected one INSERT exec, got %#v", exec.execs)
	}
	var res struct {
		User struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("unmarshal %s: %v", out, err)
	}
	if res.User.Name != "amit" {
		t.Fatalf("read-after-write should return inserted row, got %s", out)
	}
}

func TestResolve_RawInputInsert(t *testing.T) {
	dsl := &QueryDSL{
		Operation: OpInsert,
		Mutation: &Mutation{
			Type:          OpInsert,
			Keyspace:      "app",
			Table:         "users",
			PartitionKeys: []string{"id"},
			ColumnTypes:   map[string]string{"id": "text", "name": "text"},
			RawInput:      "$1",
			Returning: &Node{
				Keyspace:      "app",
				Table:         "users",
				Columns:       []string{"id", "name"},
				PartitionKeys: []string{"id"},
				FieldName:     "user",
				Singular:      true,
			},
		},
	}
	if err := dsl.SubstituteParams([]any{json.RawMessage(`{"id":"u9","name":"zed"}`)}); err != nil {
		t.Fatalf("substitute: %v", err)
	}
	exec := &fakeExecutor{rows: map[string][]map[string]any{
		"users": {{"id": "u9", "name": "zed"}},
	}}
	out, err := (&Resolver{Exec: exec}).ResolveQuery(context.Background(), dsl)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(exec.execs) != 1 || !strings.HasPrefix(exec.execs[0].CQL, "INSERT INTO app.users (id, name)") {
		t.Fatalf("expected INSERT with id+name from raw doc, got %#v", exec.execs)
	}
	if !strings.Contains(string(out), "zed") {
		t.Fatalf("read-after-write should return inserted row: %s", out)
	}
}

func TestResolve_DeletePreRead(t *testing.T) {
	dsl := &QueryDSL{
		Operation: OpDelete,
		Mutation: &Mutation{
			Type:          OpDelete,
			Table:         "users",
			PartitionKeys: []string{"id"},
			Where:         []Filter{{Col: "id", Op: OpEq, Value: "u1"}},
			Returning: &Node{
				Table:         "users",
				Columns:       []string{"id", "name"},
				PartitionKeys: []string{"id"},
				FieldName:     "user",
				Singular:      true,
				Filters:       []Filter{{Col: "id", Op: OpEq, Value: "u1"}},
			},
		},
	}
	exec := &fakeExecutor{rows: map[string][]map[string]any{
		"users": {{"id": "u1", "name": "amit"}},
	}}
	r := &Resolver{Exec: exec}
	out, err := r.ResolveQuery(context.Background(), dsl)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// Pre-read SELECT must have happened before the DELETE.
	if exec.countQueries("users") != 1 {
		t.Fatalf("expected one pre-read SELECT, got %d", exec.countQueries("users"))
	}
	if len(exec.execs) != 1 || !strings.HasPrefix(exec.execs[0].CQL, "DELETE FROM users") {
		t.Fatalf("expected one DELETE exec, got %#v", exec.execs)
	}
	if !strings.Contains(string(out), "amit") {
		t.Fatalf("delete should return pre-image, got %s", out)
	}
}
