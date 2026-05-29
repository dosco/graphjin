package core

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestGetTableSchema_AmbiguousAcrossDatabases(t *testing.T) {
	g := newGraphJinWithSchemas(t, map[string]*sdata.DBSchema{
		"primary":   mustTestSchema(t),
		"analytics": mustTestSchema(t),
	})

	_, err := g.GetTableSchema("users")
	if err == nil {
		t.Fatal("expected ambiguity error for users across multiple databases")
	}
	if !strings.Contains(err.Error(), "multiple databases") || !strings.Contains(err.Error(), "primary") || !strings.Contains(err.Error(), "analytics") {
		t.Fatalf("unexpected ambiguity error: %v", err)
	}

	schema, err := g.GetTableSchemaForDatabase("primary", "users")
	if err != nil {
		t.Fatalf("expected database-qualified schema lookup to succeed: %v", err)
	}
	if schema.Database != "primary" {
		t.Fatalf("expected schema database primary, got %q", schema.Database)
	}
}

func TestFindRelationshipPath_AmbiguousAcrossDatabases(t *testing.T) {
	g := newGraphJinWithSchemas(t, map[string]*sdata.DBSchema{
		"primary":   mustTestSchema(t),
		"analytics": mustTestSchema(t),
	})

	_, err := g.FindRelationshipPath("customers", "users")
	if err == nil {
		t.Fatal("expected ambiguity error for relationship path across multiple databases")
	}
	if !strings.Contains(err.Error(), "multiple databases") || !strings.Contains(err.Error(), "pass database") {
		t.Fatalf("unexpected ambiguity error: %v", err)
	}

	path, err := g.FindRelationshipPathForDatabase("primary", "customers", "users")
	if err != nil {
		t.Fatalf("expected database-qualified path lookup to succeed: %v", err)
	}
	if len(path) == 0 {
		t.Fatal("expected non-empty relationship path")
	}
}

func TestExploreRelationships_AmbiguousAcrossDatabases(t *testing.T) {
	g := newGraphJinWithSchemas(t, map[string]*sdata.DBSchema{
		"primary":   mustTestSchema(t),
		"analytics": mustTestSchema(t),
	})

	_, err := g.ExploreRelationships("users", 2)
	if err == nil {
		t.Fatal("expected ambiguity error for explore relationships across multiple databases")
	}
	if !strings.Contains(err.Error(), "multiple databases") || !strings.Contains(err.Error(), "pass database") {
		t.Fatalf("unexpected ambiguity error: %v", err)
	}

	graph, err := g.ExploreRelationshipsForDatabase("primary", "users", 2)
	if err != nil {
		t.Fatalf("expected database-qualified relationship exploration to succeed: %v", err)
	}
	if graph.CenterTable != "users" {
		t.Fatalf("expected center table users, got %q", graph.CenterTable)
	}
}

func TestUnqualifiedLookupStillSucceedsWhenTableIsUnique(t *testing.T) {
	secondary, err := sdata.NewDBSchema(sdata.GetTestDBInfoWithDatabase(), nil)
	if err != nil {
		t.Fatalf("create secondary schema: %v", err)
	}

	g := newGraphJinWithSchemas(t, map[string]*sdata.DBSchema{
		"primary":   mustTestSchema(t),
		"secondary": secondary,
	})

	schema, err := g.GetTableSchema("events")
	if err != nil {
		t.Fatalf("expected unique table lookup to succeed: %v", err)
	}
	if schema.Database != "secondary" {
		t.Fatalf("expected events to resolve to secondary, got %q", schema.Database)
	}

	path, err := g.FindRelationshipPath("events", "users")
	if err != nil {
		t.Fatalf("expected unique path lookup to succeed: %v", err)
	}
	if len(path) == 0 {
		t.Fatal("expected non-empty path for events -> users")
	}

	graph, err := g.ExploreRelationships("events", 2)
	if err != nil {
		t.Fatalf("expected unique relationship exploration to succeed: %v", err)
	}
	if graph.CenterTable != "events" {
		t.Fatalf("expected center table events, got %q", graph.CenterTable)
	}
}

func newGraphJinWithSchemas(t *testing.T, schemas map[string]*sdata.DBSchema) *GraphJin {
	t.Helper()

	engine := &graphjinEngine{
		databases: make(map[string]*dbContext, len(schemas)),
	}

	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		schema := schemas[name]
		engine.databases[name] = &dbContext{
			name:   name,
			schema: schema,
		}
		if engine.defaultDB == "" {
			engine.defaultDB = name
		}
	}

	g := &GraphJin{}
	g.Store(engine)
	return g
}

func mustTestSchema(t *testing.T) *sdata.DBSchema {
	t.Helper()

	schema, err := sdata.GetTestSchema()
	if err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	return schema
}

// TestDiscoverAllDatabasesConcurrent checks the bounded errgroup fan-out visits
// every database and preserves each one's dbinfo. n exceeds the concurrency
// limit so the queueing path is exercised; run with -race to catch fan-out
// races. True concurrent live discovery is covered by integration tests.
func TestDiscoverAllDatabasesConcurrent(t *testing.T) {
	const n = 16
	engine := &graphjinEngine{databases: make(map[string]*dbContext, n)}
	want := make(map[string]*sdata.DBInfo, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("db_%02d", i)
		info := sdata.GetTestDBInfo()
		engine.databases[name] = &dbContext{name: name, dbtype: "postgres", dbinfo: info}
		want[name] = info
	}
	engine.defaultDB = "db_00"

	if err := engine.discoverAllDatabases(); err != nil {
		t.Fatalf("discoverAllDatabases: %v", err)
	}
	for name, info := range want {
		if engine.databases[name].dbinfo != info {
			t.Fatalf("db %s: dbinfo dropped or replaced by concurrent discovery", name)
		}
	}
}
