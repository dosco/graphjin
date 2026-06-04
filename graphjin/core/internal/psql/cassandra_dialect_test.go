package psql_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// cassandraCompilers builds qcode + psql compilers over a small Cassandra-keyed
// schema: users(PK id) and posts(PK user_id, id) with posts.user_id → users.id.
func cassandraCompilers(t *testing.T) (*qcode.Compiler, *psql.Compiler) {
	t.Helper()
	cols := []sdata.DBColumn{
		{Schema: "app", Table: "users", Name: "id", Type: "uuid", NotNull: true, PrimaryKey: true, UniqueKey: true},
		{Schema: "app", Table: "users", Name: "name", Type: "text"},
		{Schema: "app", Table: "posts", Name: "user_id", Type: "uuid", NotNull: true, PrimaryKey: true, FKeySchema: "app", FKeyTable: "users", FKeyCol: "id"},
		{Schema: "app", Table: "posts", Name: "id", Type: "uuid", NotNull: true, PrimaryKey: true},
		{Schema: "app", Table: "posts", Name: "title", Type: "text"},
	}
	di := sdata.NewDBInfo("cassandra", 5, "app", "app", cols, nil, nil)
	for i := range di.Tables {
		switch di.Tables[i].Name {
		case "users":
			di.Tables[i].PartitionKeys = []string{"id"}
		case "posts":
			di.Tables[i].PartitionKeys = []string{"user_id"}
			di.Tables[i].ClusteringKeys = []string{"id"}
			di.Tables[i].ClusteringOrder = map[string]string{"id": "asc"}
		}
	}
	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatal(err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	pc := psql.NewCompiler(psql.Config{DBType: "cassandra"})
	return qc, pc
}

func compileCassandra(t *testing.T, gql string, vars string) (string, error) {
	t.Helper()
	qc, pc := cassandraCompilers(t)
	var v map[string]json.RawMessage
	if vars != "" {
		if err := json.Unmarshal([]byte(vars), &v); err != nil {
			t.Fatalf("vars: %v", err)
		}
	}
	reqQC, err := qc.Compile([]byte(gql), v, "user", "")
	if err != nil {
		return "", err
	}
	_, dsl, err := pc.CompileEx(reqQC)
	return string(dsl), err
}

func parseDSL(t *testing.T, dsl string) map[string]any {
	t.Helper()
	// GraphJin prepends a /* ... */ metadata comment to every compiled query.
	if i := strings.Index(dsl, "*/"); strings.HasPrefix(strings.TrimSpace(dsl), "/*") && i >= 0 {
		dsl = dsl[i+2:]
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(dsl), &m); err != nil {
		t.Fatalf("DSL is not valid JSON: %v\n%s", err, dsl)
	}
	return m
}

func TestCassandra_FilteredRead(t *testing.T) {
	dsl, err := compileCassandra(t,
		`query { users(where: { id: { eq: $id } }) { id name } }`,
		`{"id":"u1"}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	m := parseDSL(t, dsl)
	if m["operation"] != "query" {
		t.Fatalf("operation: %v", m["operation"])
	}
	root := m["root"].(map[string]any)
	if root["table"] != "users" {
		t.Fatalf("table: %v", root["table"])
	}
	pks := root["partition_keys"].([]any)
	if len(pks) != 1 || pks[0] != "id" {
		t.Fatalf("partition_keys: %v", pks)
	}
	filters := root["filters"].([]any)
	f := filters[0].(map[string]any)
	if f["col"] != "id" || f["op"] != "eq" || f["param"] == "" {
		t.Fatalf("filter wrong: %v", f)
	}
}

func TestCassandra_NestedOneToMany(t *testing.T) {
	dsl, err := compileCassandra(t,
		`query { users(where: { id: { eq: $id } }) { id name posts { title } } }`,
		`{"id":"u1"}`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	m := parseDSL(t, dsl)
	root := m["root"].(map[string]any)
	children := root["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d:\n%s", len(children), dsl)
	}
	child := children[0].(map[string]any)
	if child["table"] != "posts" {
		t.Fatalf("child table: %v", child["table"])
	}
	rel := child["rel"].(map[string]any)
	if rel["parent_col"] != "id" || rel["child_col"] != "user_id" {
		t.Fatalf("rel wrong: %v", rel)
	}
	// Parent must select its join column for the N+1 fetch.
	pcols := toStringSet(root["columns"].([]any))
	if !pcols["id"] {
		t.Fatalf("parent must select join column id; got %v", root["columns"])
	}
}

func TestCassandra_RejectsUnservableOperator(t *testing.T) {
	_, err := compileCassandra(t,
		`query { users(where: { name: { ilike: $n } }) { id } }`,
		`{"n":"a%"}`)
	if err == nil {
		t.Fatal("expected compile error for ilike on Cassandra")
	}
	if !strings.Contains(err.Error(), "cassandra") {
		t.Fatalf("expected cassandra-specific rejection, got: %v", err)
	}
}

func TestCassandra_RejectsOr(t *testing.T) {
	_, err := compileCassandra(t,
		`query { users(where: { or: [{ id: { eq: $a } }, { id: { eq: $b } }] }) { id } }`,
		`{"a":"x","b":"y"}`)
	if err == nil {
		t.Fatal("expected compile error for OR on Cassandra")
	}
	if !strings.Contains(err.Error(), "OR") {
		t.Fatalf("expected OR rejection, got: %v", err)
	}
}

func TestCassandra_AllowFilteringEscapeHatch(t *testing.T) {
	// Same schema, but users opts into ALLOW FILTERING.
	cols := []sdata.DBColumn{
		{Schema: "app", Table: "users", Name: "id", Type: "uuid", NotNull: true, PrimaryKey: true, UniqueKey: true},
		{Schema: "app", Table: "users", Name: "name", Type: "text"},
	}
	di := sdata.NewDBInfo("cassandra", 5, "app", "app", cols, nil, nil)
	for i := range di.Tables {
		if di.Tables[i].Name == "users" {
			di.Tables[i].PartitionKeys = []string{"id"}
			di.Tables[i].AllowFiltering = true
		}
	}
	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatal(err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	pc := psql.NewCompiler(psql.Config{DBType: "cassandra"})

	v := map[string]json.RawMessage{"n": json.RawMessage(`"amit"`)}
	reqQC, err := qc.Compile([]byte(`query { users(where: { name: { eq: $n } }) { id name } }`), v, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, dsl, err := pc.CompileEx(reqQC)
	if err != nil {
		t.Fatalf("non-key filter should compile under allow_filtering: %v", err)
	}
	m := parseDSL(t, string(dsl))
	root := m["root"].(map[string]any)
	if root["allow_filtering"] != true {
		t.Fatalf("expected allow_filtering=true in DSL: %s", dsl)
	}
}

func toStringSet(xs []any) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		if s, ok := x.(string); ok {
			m[s] = true
		}
	}
	return m
}
