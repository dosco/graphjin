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

	// Columns table headers
	assert.Contains(t, md, "| Column | Type | Nullable | Default | Key | FK | Index | Notes |")

	// Verify key columns exist
	assert.Contains(t, md, "full_name")
	assert.Contains(t, md, "email")
	assert.Contains(t, md, "owner_id")

	// Relationships
	assert.Contains(t, md, "#### Relationships")
	assert.Contains(t, md, "users")

	// Aggregations
	assert.Contains(t, md, "#### Aggregations")
	assert.Contains(t, md, "count_")

	// Hash and timestamp in header
	assert.Contains(t, md, "Hash:")
	assert.Contains(t, md, "Generated:")

	t.Logf("Discovery document: %d bytes", len(md))
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

	// Layer 3: Live data — row counts should be present for populated tables
	assert.Contains(t, md, "Rows:")

	// Live data profile section should exist
	assert.Contains(t, md, "#### Live Data Profile")

	// Date ranges — users/products/purchases all have created_at
	assert.Contains(t, md, "Date range")

	// Sample rows should be present
	assert.Contains(t, md, "Sample rows")
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
