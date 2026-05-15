package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogSnapshotIncludesSchemaAndLanguageCards(t *testing.T) {
	snapshot := BuildCatalogSnapshot(&MetadataSnapshot{
		Databases: []MetadataDatabase{{ID: "default", Name: "default", Type: "postgres", IsDefault: true}},
		Tables: []MetadataTable{{
			ID:           "default.public.orders",
			DatabaseName: "default",
			SchemaName:   "public",
			TableName:    "orders",
			ColumnCount:  3,
			PrimaryKey:   "id",
		}},
		Columns: []MetadataColumn{
			{ID: "default.public.orders.id", TableID: "default.public.orders", DatabaseName: "default", SchemaName: "public", TableName: "orders", ColumnName: "id", Type: "integer", PrimaryKey: true},
			{ID: "default.public.orders.total", TableID: "default.public.orders", DatabaseName: "default", SchemaName: "public", TableName: "orders", ColumnName: "total", Type: "numeric"},
			{ID: "default.public.orders.created_at", TableID: "default.public.orders", DatabaseName: "default", SchemaName: "public", TableName: "orders", ColumnName: "created_at", Type: "timestamp"},
		},
	}, &Config{})

	if len(snapshot.Query(CatalogQuery{Kind: "table", Table: "orders"})) != 1 {
		t.Fatalf("expected orders table card")
	}
	if len(snapshot.Query(CatalogQuery{Kind: "directive", Search: "@running"})) != 1 {
		t.Fatalf("expected @running directive card")
	}
	if len(snapshot.Query(CatalogQuery{Kind: "deprecated_feature", Search: "@window"})) != 1 {
		t.Fatalf("expected deprecated @window card")
	}
	var hasSampleProfile bool
	for _, detail := range snapshot.CardDetails("table:default.public.orders") {
		if detail.Section == "samples_profile" && strings.Contains(detail.DataJSON, "on_demand") {
			hasSampleProfile = true
		}
	}
	if !hasSampleProfile {
		t.Fatalf("expected table card to describe on-demand sample/profile availability")
	}
}

func TestCatalogConfigRedactsSensitiveValues(t *testing.T) {
	conf := &Config{
		SecretKey: "super-secret",
		Databases: map[string]DatabaseConfig{
			"default": {
				ConnString:    "postgres://user:pass@localhost/db",
				Password:      "pass",
				PrivateKeyPEM: "-----BEGIN PRIVATE KEY-----",
			},
		},
	}

	snapshot := BuildCatalogSnapshot(&MetadataSnapshot{}, conf)
	card, ok := snapshot.Card("config:core")
	if !ok {
		t.Fatal("expected config card")
	}
	if !card.Sensitive {
		t.Fatal("expected config card to be marked sensitive")
	}
	for _, detail := range snapshot.CardDetails("config:core") {
		if strings.Contains(detail.DataJSON, "super-secret") ||
			strings.Contains(detail.DataJSON, "postgres://user:pass") ||
			strings.Contains(detail.DataJSON, "-----BEGIN PRIVATE KEY-----") {
			t.Fatalf("sensitive value leaked in catalog detail: %s", detail.DataJSON)
		}
	}
}

func TestLanguageFeaturesIncludeCompilerDirectives(t *testing.T) {
	features := LanguageFeatures()
	have := map[string]bool{}
	for _, f := range features {
		have[f.Name] = true
	}
	for _, want := range []string{"@object", "@through", "@running", "@moving", "@previous", "@next", "@first", "@last", "@rank", "@denseRank", "@rowNumber"} {
		if !have[want] {
			t.Fatalf("language registry missing %s", want)
		}
	}
}

func TestCatalogConfigAutoMode(t *testing.T) {
	on := true
	off := false
	conf := &Config{Sources: []SourceConfig{{Name: "graphjin", Kind: "graphjin"}}}
	if !conf.CatalogEnabled() {
		t.Fatal("graphjin source should enable catalog by default")
	}

	conf.Sources[0].Catalog = &off
	if conf.CatalogEnabled() {
		t.Fatal("catalog false should disable catalog")
	}

	conf.Sources[0].Catalog = &on
	if !conf.CatalogEnabled() {
		t.Fatal("catalog true should enable catalog")
	}
	if !conf.CatalogAutoCodeRelationsEnabled() {
		t.Fatal("auto code relations should follow catalog enabled")
	}

	conf.Sources[0].Catalog = &off
	if conf.CatalogAutoCodeRelationsEnabled() {
		t.Fatal("auto code relations should follow disabled catalog")
	}
}

func TestCatalogSearchRanksAnalyticsCards(t *testing.T) {
	snapshot := BuildCatalogSnapshot(&MetadataSnapshot{}, &Config{})

	result, err := snapshot.QueryResult(CatalogQuery{
		Search:  "running total",
		Where:   map[string]any{"kind": map[string]any{"in": []any{"directive", "query_pattern", "deprecated_feature"}}},
		OrderBy: map[string]string{"score": "desc"},
		Limit:   5,
		Explain: true,
	})
	if err != nil {
		t.Fatalf("query catalog: %v", err)
	}
	if len(result.Cards) == 0 {
		t.Fatal("expected analytics search results")
	}
	if result.Cards[0].ID != "language:directive.running" {
		t.Fatalf("expected @running to rank first, got %s", result.Cards[0].ID)
	}
	match := result.Matches[result.Cards[0].ID]
	if match.Score <= 0 || len(match.MatchedFields) == 0 || match.Why == "" {
		t.Fatalf("expected explanation metadata, got %#v", match)
	}
}

func TestCatalogSearchRanksRelationshipsAboveTablesForJoinIntent(t *testing.T) {
	snapshot := BuildCatalogSnapshot(&MetadataSnapshot{
		Databases: []MetadataDatabase{{ID: "default", Name: "default", Type: "postgres", IsDefault: true}},
		Tables: []MetadataTable{
			{ID: "default.public.orders", DatabaseName: "default", SchemaName: "public", TableName: "orders", ColumnCount: 2, PrimaryKey: "id"},
			{ID: "default.public.customers", DatabaseName: "default", SchemaName: "public", TableName: "customers", ColumnCount: 2, PrimaryKey: "id"},
		},
		Columns: []MetadataColumn{
			{ID: "default.public.orders.id", TableID: "default.public.orders", DatabaseName: "default", SchemaName: "public", TableName: "orders", ColumnName: "id", Type: "integer", PrimaryKey: true},
			{ID: "default.public.orders.customer_id", TableID: "default.public.orders", DatabaseName: "default", SchemaName: "public", TableName: "orders", ColumnName: "customer_id", Type: "integer", Indexed: true},
			{ID: "default.public.customers.id", TableID: "default.public.customers", DatabaseName: "default", SchemaName: "public", TableName: "customers", ColumnName: "id", Type: "integer", PrimaryKey: true},
			{ID: "default.public.customers.name", TableID: "default.public.customers", DatabaseName: "default", SchemaName: "public", TableName: "customers", ColumnName: "name", Type: "text"},
		},
		Relationships: []MetadataRelationship{{
			ID:               "default.public.orders.customer_id-default.public.customers.id",
			FromDatabaseName: "default", FromSchemaName: "public", FromTableName: "orders", FromColumnName: "customer_id", FromColumnID: "default.public.orders.customer_id",
			ToDatabaseName: "default", ToSchemaName: "public", ToTableName: "customers", ToColumnName: "id", ToColumnID: "default.public.customers.id",
			RelType: "many_to_one", Source: "foreign_key",
		}},
	}, &Config{})

	result, err := snapshot.QueryResult(CatalogQuery{Search: "join orders customers", Limit: 5})
	if err != nil {
		t.Fatalf("query catalog: %v", err)
	}
	if len(result.Cards) == 0 {
		t.Fatal("expected relationship search results")
	}
	if result.Cards[0].Kind != "relationship" {
		t.Fatalf("expected relationship to rank first, got %s (%s)", result.Cards[0].Kind, result.Cards[0].ID)
	}
}

func TestCatalogSearchFuzzyTypoRecovery(t *testing.T) {
	snapshot := BuildCatalogSnapshot(&MetadataSnapshot{}, &Config{})

	result, err := snapshot.QueryResult(CatalogQuery{
		Search: "runing total",
		Where:  map[string]any{"kind": map[string]any{"eq": "directive"}},
		Limit:  3,
	})
	if err != nil {
		t.Fatalf("query catalog: %v", err)
	}
	if len(result.Cards) == 0 || result.Cards[0].ID != "language:directive.running" {
		t.Fatalf("expected fuzzy search to find @running, got %#v", result.Cards)
	}
}

func TestCatalogWhereOperators(t *testing.T) {
	snapshot := BuildCatalogSnapshot(&MetadataSnapshot{}, &Config{})

	result, err := snapshot.QueryResult(CatalogQuery{
		Where: map[string]any{
			"and": []any{
				map[string]any{"title": map[string]any{"ilike": "%running%"}},
				map[string]any{"kind": map[string]any{"in": []any{"directive", "deprecated_feature"}}},
				map[string]any{"not": map[string]any{"kind": map[string]any{"eq": "deprecated_feature"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("query catalog: %v", err)
	}
	if len(result.Cards) != 1 || result.Cards[0].ID != "language:directive.running" {
		t.Fatalf("expected only @running directive, got %#v", result.Cards)
	}

	result, err = snapshot.QueryResult(CatalogQuery{
		Where: map[string]any{
			"or": []any{
				map[string]any{"title": map[string]any{"regex": "^@dense"}},
				map[string]any{"title": map[string]any{"ilike": "%rownumber%"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("query catalog: %v", err)
	}
	if len(result.Cards) != 2 {
		t.Fatalf("expected regex/or filters to find two directives, got %d", len(result.Cards))
	}
}

func TestCatalogOrderByScoreIsDeterministic(t *testing.T) {
	snapshot := BuildCatalogSnapshot(&MetadataSnapshot{}, &Config{})
	query := CatalogQuery{
		Search:  "rank",
		Where:   map[string]any{"kind": map[string]any{"in": []any{"directive", "query_pattern"}}},
		OrderBy: map[string]string{"score": "desc"},
		Limit:   10,
	}

	first, err := snapshot.QueryResult(query)
	if err != nil {
		t.Fatalf("query catalog: %v", err)
	}
	second, err := snapshot.QueryResult(query)
	if err != nil {
		t.Fatalf("query catalog again: %v", err)
	}
	if len(first.Cards) != len(second.Cards) {
		t.Fatalf("result counts differ: %d vs %d", len(first.Cards), len(second.Cards))
	}
	for i := range first.Cards {
		if first.Cards[i].ID != second.Cards[i].ID {
			t.Fatalf("order differs at %d: %s vs %s", i, first.Cards[i].ID, second.Cards[i].ID)
		}
	}
}

func TestCatalogExplainDoesNotLeakSensitiveValues(t *testing.T) {
	conf := &Config{
		SecretKey: "super-secret",
		Databases: map[string]DatabaseConfig{
			"default": {Password: "database-password"},
		},
	}
	snapshot := BuildCatalogSnapshot(&MetadataSnapshot{}, conf)

	result, err := snapshot.QueryResult(CatalogQuery{
		Search:  "secret",
		Where:   map[string]any{"kind": map[string]any{"eq": "config"}},
		Explain: true,
	})
	if err != nil {
		t.Fatalf("query catalog: %v", err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(data), "super-secret") || strings.Contains(string(data), "database-password") {
		t.Fatalf("sensitive value leaked in query result: %s", data)
	}
	if len(result.Matches) == 0 {
		t.Fatal("expected explain metadata")
	}
}

func TestCatalogShorthandArgsStillWork(t *testing.T) {
	snapshot := BuildCatalogSnapshot(&MetadataSnapshot{
		Tables: []MetadataTable{{ID: "default.public.orders", DatabaseName: "default", SchemaName: "public", TableName: "orders", ColumnCount: 1}},
	}, &Config{})

	result, err := snapshot.QueryResult(CatalogQuery{Kind: "table", Table: "orders"})
	if err != nil {
		t.Fatalf("query catalog: %v", err)
	}
	if len(result.Cards) != 1 || result.Cards[0].ID != "table:default.public.orders" {
		t.Fatalf("expected shorthand filters to find orders table, got %#v", result.Cards)
	}
}
