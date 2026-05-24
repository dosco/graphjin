package tests_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDiscoveryDM(t *testing.T) (*core.GraphJin, *serv.DiscoveryManager) {
	t.Helper()
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	t.Cleanup(func() { gj.Close() })
	dm := serv.NewDiscoveryManager(gj)
	return gj, dm
}

func ensureCatalogStats(t *testing.T) {
	t.Helper()
	seededTables := []string{"users", "categories", "products", "purchases", "notifications", "comments"}
	switch dbType {
	case "postgres":
		_, _ = db.Exec("ANALYZE")
	case "sqlite":
		_, _ = db.Exec("ANALYZE")
	case "mysql", "mariadb":
		for _, tbl := range seededTables {
			_, _ = db.Exec("ANALYZE TABLE " + tbl)
		}
	case "oracle":
		for _, tbl := range seededTables {
			_, _ = db.Exec("ANALYZE TABLE " + tbl + " COMPUTE STATISTICS")
		}
	case "mssql":
		_, _ = db.Exec("EXEC sp_updatestats")
	}
}

// seededSchemaForDialect returns the namespace identifier where the test
// fixture's seeded tables live. It reads the actual Schema value loaded
// from core for the "users" table when available so we don't need to
// guess per-dialect defaults.
func seededSchemaForDialect(gj *core.GraphJin) string {
	dbName := gj.DefaultDatabase()
	for _, t := range gj.GetTablesForDatabase(dbName) {
		if t.Name == "users" {
			if t.Schema != "" {
				return t.Schema
			}
			break
		}
	}
	switch dbType {
	case "mysql", "mariadb":
		return dbName
	case "postgres":
		return "public"
	case "mssql":
		return "dbo"
	}
	return ""
}

func TestDiscovery(t *testing.T) {
	gj, dm := newDiscoveryDM(t)

	t.Run("TableIndex", func(t *testing.T) {
		dbName := gj.DefaultDatabase()
		tables := dm.TableIndex(dbName)
		require.NotEmpty(t, tables, "expected at least one table in the index")
		for _, entry := range tables {
			assert.NotEmpty(t, entry.Name)
		}
	})

	t.Run("RowCountsForSeededTables", func(t *testing.T) {
		if dbType == "mongodb" {
			t.Skip("mongodb row counts are not supported via the SQL path")
		}
		ensureCatalogStats(t)
		dm.Invalidate()
		dbName := gj.DefaultDatabase()
		schema := seededSchemaForDialect(gj)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		counts := dm.RowCountsForNamespace(ctx, dbName, schema)
		require.NotEmpty(t, counts, "expected row counts for namespace %q (dialect=%s)", schema, dbType)
		for _, want := range []struct {
			name string
			min  int64
		}{
			{"users", 100},
			{"products", 1},
			{"categories", 1},
		} {
			n, ok := counts[want.name]
			if !ok {
				continue
			}
			assert.GreaterOrEqualf(t, n, want.min,
				"table %q: row_count_approx=%d expected >= %d (dialect=%s, schema=%q)",
				want.name, n, want.min, dbType, schema)
		}
	})

	t.Run("NamespaceRollup", func(t *testing.T) {
		dm.Invalidate()
		dbName := gj.DefaultDatabase()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rollup := dm.Namespaces(ctx, dbName)
		require.NotEmpty(t, rollup, "expected at least one namespace rollup entry (dialect=%s)", dbType)
		var totalTables int
		for _, n := range rollup {
			assert.NotEmptyf(t, n.Database, "namespace rollup entry missing database (dialect=%s)", dbType)
			totalTables += n.TableCount
		}
		assert.Greaterf(t, totalTables, 0, "expected non-zero table count across namespaces (dialect=%s)", dbType)
		if dbType == "mongodb" {
			for _, n := range rollup {
				assert.Falsef(t, n.RowCountAvailable, "mongodb rollup should report row counts unavailable (got %+v)", n)
			}
		}
	})

	t.Run("TableIndexHasRowCounts", func(t *testing.T) {
		if dbType == "mongodb" {
			t.Skip("mongodb row counts are not supported via the SQL path")
		}
		ensureCatalogStats(t)
		dm.Invalidate()
		dbName := gj.DefaultDatabase()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// No schema filter — populate happens in ensureTables for ALL
		// namespaces present in the loaded table list. Agents must never
		// see RowCountApprox=0 lying about a non-empty seeded table.
		page := dm.TableIndexPage(ctx, dbName, serv.TableListOptions{Limit: 500})
		var sawUsersWithRows bool
		for {
			require.NotEmpty(t, page.Tables, "expected tables (dialect=%s)", dbType)
			for _, e := range page.Tables {
				if e.Name == "users" && e.RowCountApprox != nil && *e.RowCountApprox >= 100 {
					sawUsersWithRows = true
				}
			}
			if sawUsersWithRows || dbType != "bigquery" {
				break
			}
			if ctx.Err() != nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
			page = dm.TableIndexPage(ctx, dbName, serv.TableListOptions{Limit: 500})
		}
		assert.Truef(t, sawUsersWithRows, "expected users.row_count_approx >= 100 on cheap path (dialect=%s)", dbType)
	})

	t.Run("EnrichmentForSeededTable", func(t *testing.T) {
		if dbType == "mongodb" {
			t.Skip("mongodb enrichment GraphQL is unverified on this driver")
		}
		dbName := gj.DefaultDatabase()
		payload := dm.FullPayload(dbName)
		require.NotNil(t, payload)

		type hit struct {
			table string
			col   string
			stats serv.NumericStats
			prof  *serv.TableProfile
		}
		var found *hit
		for i := range payload.Tables {
			entry := &payload.Tables[i]
			if entry.Profile == nil || entry.Profile.RowCountApprox == nil || *entry.Profile.RowCountApprox <= 1 {
				continue
			}
			for col, stats := range entry.Profile.NumericStats {
				if stats.Count > 1 {
					found = &hit{table: entry.Name, col: col, stats: stats, prof: entry.Profile}
					break
				}
			}
			if found != nil {
				break
			}
		}
		if found == nil {
			t.Skip("no multi-row seeded table with numeric columns found in this dialect's fixture")
		}
		t.Logf("enrichment hit: %s.%s count=%d min=%s max=%s avg=%s (dialect=%s)",
			found.table, found.col, found.stats.Count, found.stats.Min, found.stats.Max, found.stats.Avg, dbType)

		assert.NotEmptyf(t, found.prof.SampleRows,
			"expected sample rows on %q (dialect=%s)", found.table, dbType)
	})

	t.Run("JSONShapeRoundTrip", func(t *testing.T) {
		dbName := gj.DefaultDatabase()
		raw, err := json.Marshal(dm.FullPayload(dbName))
		require.NoError(t, err)

		var out serv.DiscoveryFullPayload
		require.NoError(t, json.Unmarshal(raw, &out), "unmarshal back into DiscoveryFullPayload")
		assert.Equal(t, dbName, out.Database)
		assert.NotEmpty(t, out.Tables, "round-tripped tables slice should not be empty (dialect=%s)", dbType)
		for _, entry := range out.Tables {
			assert.NotEmpty(t, entry.Name)
		}
	})

	t.Run("Payload", func(t *testing.T) {
		payload := dm.Payload(gj.DefaultDatabase())
		require.NotNil(t, payload)
		assert.NotEmpty(t, payload.Tables)
		assert.Equal(t, gj.DefaultDatabase(), payload.Database)
	})

	t.Run("Insights", func(t *testing.T) {
		insights := dm.Insights(gj.DefaultDatabase())
		assert.Equal(t, gj.DefaultDatabase(), insights.Database)
	})

	t.Run("Caching", func(t *testing.T) {
		a, _ := json.Marshal(dm.Payload(gj.DefaultDatabase()))
		b, _ := json.Marshal(dm.Payload(gj.DefaultDatabase()))
		assert.JSONEq(t, string(a), string(b))
	})

	t.Run("SchemaChange", func(t *testing.T) {
		fired := make(chan string, 1)
		gj.OnSchemaChange(func(dbName, hash string) {
			select {
			case fired <- hash:
			default:
			}
		})

		require.NoError(t, gj.Reload())

		select {
		case hash := <-fired:
			assert.NotEmpty(t, hash)
		case <-time.After(5 * time.Second):
			t.Fatal("callback did not fire")
		}
	})

	t.Run("Subscription", func(t *testing.T) {
		ds, err := dm.Subscribe(context.Background(), gj.DefaultDatabase())
		require.NoError(t, err)
		defer ds.Unsubscribe()

		select {
		case payload := <-ds.Result:
			require.NotNil(t, payload)
			assert.NotEmpty(t, payload.Tables)
		case <-time.After(5 * time.Second):
			t.Fatal("no initial payload")
		}
	})

	t.Run("DatabaseNames", func(t *testing.T) {
		names := gj.DatabaseNames()
		assert.GreaterOrEqual(t, len(names), 1)
		assert.Contains(t, names, gj.DefaultDatabase())
	})
}
