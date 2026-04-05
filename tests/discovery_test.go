package tests_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newDiscoveryDM creates a shared GraphJin instance and DiscoveryManager for discovery tests.
func newDiscoveryDM(t *testing.T) (*core.GraphJin, *serv.DiscoveryManager) {
	t.Helper()
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	t.Cleanup(func() { gj.Close() })
	dm := serv.NewDiscoveryManager(gj)
	return gj, dm
}

func TestDiscovery(t *testing.T) {
	gj, dm := newDiscoveryDM(t)

	t.Run("Generate", func(t *testing.T) {
		// Discovery is lazily generated on first access
		md := dm.Combined()
		require.NotEmpty(t, md, "Combined discovery should be generated on first access")

		// Verify key tables present
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

		// Should NOT contain full column table (that's in full_tables section)
		assert.NotContains(t, md, "| Column | Type | Nullable | Default | Key | FK | Index | Notes |")
	})

	t.Run("Sections", func(t *testing.T) {
		// Each section should be non-empty
		syntax := dm.CombinedSection("syntax")
		tables := dm.CombinedSection("tables")
		fullTables := dm.CombinedSection("full_tables")
		insights := dm.CombinedSection("insights")

		assert.NotEmpty(t, syntax, "syntax section should not be empty")
		assert.NotEmpty(t, tables, "tables section should not be empty")
		assert.NotEmpty(t, fullTables, "full_tables section should not be empty")
		assert.NotEmpty(t, insights, "insights section should not be empty")

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
	})

	t.Run("Layer3Enrichment", func(t *testing.T) {
		if dbType == "mongodb" {
			t.Skip("MongoDB enrichment queries use different syntax")
		}

		md := dm.Combined()
		require.NotEmpty(t, md)

		// Layer 3: Live data — row counts should be present in compact table index
		assert.Contains(t, md, "Rows:")

		// Live data profile, date ranges, sample rows are in the full tables section
		fullTables := dm.CombinedSection("full_tables")
		require.NotEmpty(t, fullTables)

		assert.Contains(t, fullTables, "#### Live Data Profile")
		assert.Contains(t, fullTables, "Date range")
		assert.Contains(t, fullTables, "Sample rows")
	})

	t.Run("QueryTemplates", func(t *testing.T) {
		md := dm.Combined()
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
	})

	t.Run("Caching", func(t *testing.T) {
		// Lazily generated on first access — then cached
		md1 := dm.Combined()
		require.NotEmpty(t, md1)

		// Second call returns same content (cached)
		md2 := dm.Combined()
		assert.Equal(t, md1, md2)

		// Per-database cache should also be populated
		dbName := gj.DefaultDatabase()
		doc := dm.Get(dbName)
		require.NotNil(t, doc)
		assert.NotEmpty(t, doc.Hash)

		// GetAll should return at least one
		all := dm.GetAll()
		assert.GreaterOrEqual(t, len(all), 1)
	})

	t.Run("SchemaChangeCallback", func(t *testing.T) {
		callbackFired := make(chan string, 1)
		gj.OnSchemaChange(func(dbName string, hash string) {
			select {
			case callbackFired <- hash:
			default:
			}
		})

		// Reload triggers schema change callbacks
		err := gj.Reload()
		require.NoError(t, err)

		select {
		case hash := <-callbackFired:
			assert.NotEmpty(t, hash)
		case <-time.After(5 * time.Second):
			t.Fatal("Schema change callback did not fire after Reload()")
		}
	})

	t.Run("Subscription", func(t *testing.T) {
		ctx := context.Background()
		dbName := gj.DefaultDatabase()

		ds, err := dm.Subscribe(ctx, dbName)
		require.NoError(t, err)
		defer ds.Unsubscribe()

		// Should receive initial document immediately
		select {
		case doc := <-ds.Result:
			require.NotNil(t, doc)
			assert.NotEmpty(t, doc.Hash)
			assert.NotEmpty(t, doc.Tables)
			assert.Contains(t, doc.Tables, "### users")
		case <-time.After(10 * time.Second):
			t.Fatal("Did not receive initial discovery document from subscription")
		}
	})

	t.Run("DataQuality", func(t *testing.T) {
		if dbType == "mongodb" {
			t.Skip("MongoDB does not have nullable column metadata")
		}

		md := dm.Combined()
		require.NotEmpty(t, md)

		// Data quality section should flag nullable columns
		assert.Contains(t, md, "## Data Quality")
		assert.Contains(t, md, "nullable")
	})

	t.Run("RelationshipPaths", func(t *testing.T) {
		if dbType == "mongodb" {
			t.Skip("MongoDB relationship paths work differently")
		}

		md := dm.Combined()
		require.NotEmpty(t, md)

		assert.Contains(t, md, "## Relationship Paths")
	})

	t.Run("DatabaseNames", func(t *testing.T) {
		names := gj.DatabaseNames()
		assert.GreaterOrEqual(t, len(names), 1)

		defaultDB := gj.DefaultDatabase()
		assert.Contains(t, names, defaultDB)
	})
}
