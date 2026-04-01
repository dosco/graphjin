package tests_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryGenerate(t *testing.T) {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// Discovery is auto-generated at startup
	md := gj.GetCombinedDiscovery()
	require.NotEmpty(t, md, "Combined discovery should be auto-generated at startup")

	// Layer 1: Raw schema — verify key tables present
	assert.Contains(t, md, "# Schema Bible:")
	assert.Contains(t, md, "### users")
	assert.Contains(t, md, "### products")
	assert.Contains(t, md, "### purchases")
	assert.Contains(t, md, "### comments")

	// Compact table index format — column names, FKs, joins
	assert.Contains(t, md, "full_name")
	assert.Contains(t, md, "email")
	assert.Contains(t, md, "FKs:")
	assert.Contains(t, md, "Columns:")
	assert.Contains(t, md, "Joins:")

	// Hash and timestamp in header
	assert.Contains(t, md, "Hash:")
	assert.Contains(t, md, "Generated:")

	// Should NOT contain full column table (that's in full_tables section)
	assert.NotContains(t, md, "| Column | Type | Nullable | Default | Key | FK | Index | Notes |")

	t.Logf("Discovery document: %d bytes", len(md))
}

func TestDiscoveryTableOfContents(t *testing.T) {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	md := gj.GetCombinedDiscovery()
	require.NotEmpty(t, md)

	// TOC section exists
	assert.Contains(t, md, "## Table of Contents")

	// TOC has section links
	assert.Contains(t, md, "- [Query Syntax Reference](#query-syntax-reference)")
	assert.Contains(t, md, "- [Tables](#tables)")
	assert.Contains(t, md, "- [Relationship Paths](#relationship-paths)")
	assert.Contains(t, md, "- [Query Templates](#query-templates)")
	assert.Contains(t, md, "- [Data Quality](#data-quality)")

	// TOC includes individual table links
	assert.Contains(t, md, "  - [users](#users)")
	assert.Contains(t, md, "  - [products](#products)")

	// TOC appears before the Tables section
	tocIdx := strings.Index(md, "## Table of Contents")
	tablesIdx := strings.Index(md, "## Tables")
	assert.Greater(t, tablesIdx, tocIdx, "TOC should appear before Tables section")
}

func TestDiscoverySections(t *testing.T) {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	full := gj.GetCombinedDiscovery()
	require.NotEmpty(t, full)

	// Each section should be non-empty
	overview := gj.GetCombinedDiscoverySection("overview")
	syntax := gj.GetCombinedDiscoverySection("syntax")
	tables := gj.GetCombinedDiscoverySection("tables")
	fullTables := gj.GetCombinedDiscoverySection("full_tables")
	insights := gj.GetCombinedDiscoverySection("insights")

	assert.NotEmpty(t, overview, "overview section should not be empty")
	assert.NotEmpty(t, syntax, "syntax section should not be empty")
	assert.NotEmpty(t, tables, "tables section should not be empty")
	assert.NotEmpty(t, fullTables, "full_tables section should not be empty")
	assert.NotEmpty(t, insights, "insights section should not be empty")

	// Overview has header and TOC but not table definitions
	assert.Contains(t, overview, "# Schema Bible:")
	assert.Contains(t, overview, "## Table of Contents")
	assert.NotContains(t, overview, "## Tables")

	// Syntax has DSL reference with nested aggregation example
	assert.Contains(t, syntax, "## Query Syntax Reference")
	assert.Contains(t, syntax, "distinct")
	assert.Contains(t, syntax, "count_")
	assert.Contains(t, syntax, "Nested Aggregation")

	// Compact tables section has index entries
	assert.Contains(t, tables, "## Tables")
	assert.Contains(t, tables, "### users")
	assert.Contains(t, tables, "### products")
	assert.Contains(t, tables, "FKs:")
	assert.Contains(t, tables, "Columns:")
	assert.NotContains(t, tables, "| Column | Type | Nullable")

	// Full tables section has detailed column definitions
	assert.Contains(t, fullTables, "| Column | Type | Nullable | Default | Key | FK | Index | Notes |")
	assert.Contains(t, fullTables, "#### Relationships")
	assert.Contains(t, fullTables, "#### Aggregations")

	// Insights has templates and relationships
	assert.Contains(t, insights, "## Relationship Paths")
	assert.Contains(t, insights, "## Query Templates")
	assert.Contains(t, insights, "## Data Quality")

	// Compact tables should be much smaller than full tables
	assert.Greater(t, len(fullTables), len(tables)*2, "full tables should be significantly larger than compact index")

	t.Logf("Section sizes — overview: %d, syntax: %d, tables: %d, full_tables: %d, insights: %d, total md: %d",
		len(overview), len(syntax), len(tables), len(fullTables), len(insights), len(full))
}

func TestDiscoveryLayer3Enrichment(t *testing.T) {
	if dbType == "mongodb" {
		t.Skip("MongoDB enrichment queries use different syntax")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	md := gj.GetCombinedDiscovery()
	require.NotEmpty(t, md)

	// Layer 3: Live data — row counts should be present in compact table index
	assert.Contains(t, md, "Rows:")

	// Live data profile, date ranges, sample rows are in the full tables section
	fullTables := gj.GetCombinedDiscoverySection("full_tables")
	require.NotEmpty(t, fullTables)

	assert.Contains(t, fullTables, "#### Live Data Profile")
	assert.Contains(t, fullTables, "Date range")
	assert.Contains(t, fullTables, "Sample rows")
}

func TestDiscoveryQueryTemplates(t *testing.T) {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	md := gj.GetCombinedDiscovery()
	require.NotEmpty(t, md)

	// Query templates section should exist
	assert.Contains(t, md, "## Query Templates")

	// Should have graphql code blocks
	assert.Contains(t, md, "```graphql")

	// Should have at least one template type
	hasTemplate := strings.Contains(md, "### Time-series:") ||
		strings.Contains(md, "### Breakdown:") ||
		strings.Contains(md, "### Join:")
	assert.True(t, hasTemplate, "Expected at least one query template")
}

func TestDiscoveryCaching(t *testing.T) {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// Auto-generated at startup — should already be cached
	md1 := gj.GetCombinedDiscovery()
	require.NotEmpty(t, md1)

	// Second call returns same content (cached)
	md2 := gj.GetCombinedDiscovery()
	assert.Equal(t, md1, md2)

	// Per-database cache should also be populated
	dbName := gj.DefaultDatabase()
	doc := gj.GetDiscovery(dbName)
	require.NotNil(t, doc)
	assert.NotEmpty(t, doc.Hash)

	// GetAllDiscovery should return at least one
	all := gj.GetAllDiscovery()
	assert.GreaterOrEqual(t, len(all), 1)
}

func TestDiscoverySchemaChangeCallback(t *testing.T) {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	callbackFired := make(chan string, 1)
	gj.OnSchemaChange(func(dbName string, hash string) {
		select {
		case callbackFired <- hash:
		default:
		}
	})

	// Reload triggers schema change callbacks
	err = gj.Reload()
	require.NoError(t, err)

	select {
	case hash := <-callbackFired:
		assert.NotEmpty(t, hash)
	case <-time.After(5 * time.Second):
		t.Fatal("Schema change callback did not fire after Reload()")
	}
}

func TestDiscoverySubscription(t *testing.T) {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	ctx := context.Background()
	dbName := gj.DefaultDatabase()

	ds, err := gj.SubscribeDiscovery(ctx, dbName)
	require.NoError(t, err)
	defer ds.Unsubscribe()

	// Should receive initial document immediately
	select {
	case doc := <-ds.Result:
		require.NotNil(t, doc)
		assert.NotEmpty(t, doc.Markdown)
		assert.NotEmpty(t, doc.Hash)
		assert.Contains(t, doc.Markdown, "# Schema Bible:")
		assert.Contains(t, doc.Markdown, "### users")
	case <-time.After(10 * time.Second):
		t.Fatal("Did not receive initial discovery document from subscription")
	}
}

func TestDiscoveryInvalidDatabase(t *testing.T) {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	ctx := context.Background()
	_, err = gj.GenerateDiscovery(ctx, "nonexistent_db")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDiscoveryDataQuality(t *testing.T) {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	md := gj.GetCombinedDiscovery()
	require.NotEmpty(t, md)

	// Data quality section should flag nullable columns
	assert.Contains(t, md, "## Data Quality")
	assert.Contains(t, md, "nullable")
}

func TestDiscoveryRelationshipPaths(t *testing.T) {
	if dbType == "mongodb" {
		t.Skip("MongoDB relationship paths work differently")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	md := gj.GetCombinedDiscovery()
	require.NotEmpty(t, md)

	// The webshop has rich relationships
	assert.Contains(t, md, "## Relationship Paths")
}

func TestDiscoveryDatabaseNames(t *testing.T) {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	names := gj.DatabaseNames()
	assert.GreaterOrEqual(t, len(names), 1)

	defaultDB := gj.DefaultDatabase()
	assert.Contains(t, names, defaultDB)
}
