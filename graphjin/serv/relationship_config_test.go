package serv

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func relationshipTestDB(t *testing.T, name string, stmts ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	return path
}

func TestConfiguredRelationshipsResolveWithSourcePrefixAndAlias(t *testing.T) {
	a := relationshipTestDB(t, "a.sqlite3",
		`CREATE TABLE categories (catid INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE items (id INTEGER PRIMARY KEY, categoryid INTEGER)`,
		`INSERT INTO categories VALUES (7, 'Cables')`,
		`INSERT INTO items VALUES (1, 7)`)
	b := relationshipTestDB(t, "b.sqlite3",
		`CREATE TABLE details (id INTEGER PRIMARY KEY, itemid INTEGER)`,
		`INSERT INTO details VALUES (100, 1)`)
	conf := &Config{Core: core.Config{
		Mode:             modeAgentic,
		DisableAllowList: true,
		Sources: []core.SourceConfig{
			{Name: "a", Kind: sourcecap.KindDatabase, Type: "sqlite", Path: a, Default: true, Access: core.SourceAccessConfig{Read: "public"}},
			{Name: "b", Kind: sourcecap.KindDatabase, Type: "sqlite", Path: b, Access: core.SourceAccessConfig{Read: "public"}},
		},
		Tables: []core.Table{
			{Name: "items", Schema: "main", Source: "a"},
			{Name: "categories", Schema: "main", Source: "a"},
			{Name: "details", Schema: "main", Source: "b"},
		},
		Relationships: []core.RelationshipConfig{
			{From: "a:main.items.categoryid", To: "a:main.categories.catid", As: "category"},
			{From: "b:main.details.itemid", To: "a:main.items.id", As: "item"},
		},
	}}
	svc, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatalf("init service: %v", err)
	}
	t.Cleanup(func() { closeTestService(svc) })

	for query, want := range map[string]string{
		`query { items { id categories { name } } }`: `"name":"Cables"`,
		`query { items { id category { name } } }`:   `"category":{"name":"Cables"}`,
		`query { details { id items { id } } }`:      `"items":`,
		`query { details { id item { id } } }`:       `"item":`,
	} {
		res, err := svc.gj.GraphQL(context.Background(), query, nil, &core.RequestConfig{})
		if err != nil {
			t.Errorf("%s: %v", query, err)
			continue
		}
		if !strings.Contains(string(res.Data), want) {
			t.Errorf("%s: data %s, want %s", query, res.Data, want)
		}
	}
}
