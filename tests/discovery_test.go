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
		dbName := gj.DefaultDatabase()
		tables := dm.TableIndex(dbName)
		byName := map[string]serv.TableIndexEntry{}
		for _, t := range tables {
			byName[t.Name] = t
		}
		for _, want := range []struct {
			name string
			min  int64
		}{
			{"users", 100},
			{"products", 1},
			{"categories", 1},
		} {
			entry, ok := byName[want.name]
			if !ok {
				continue
			}
			assert.GreaterOrEqualf(t, entry.RowCountApprox, want.min,
				"table %q: row_count_approx=%d expected >= %d (dialect=%s)",
				want.name, entry.RowCountApprox, want.min, dbType)
		}
	})

	t.Run("EnrichmentForSeededTable", func(t *testing.T) {
		if dbType == "mongodb" {
			t.Skip("mongodb enrichment GraphQL is unverified on this driver")
		}
		dbName := gj.DefaultDatabase()
		payload := dm.FullPayload(dbName)
		require.NotNil(t, payload)

		var found *serv.TableDetailEntry
		for i := range payload.Tables {
			entry := &payload.Tables[i]
			if entry.Profile == nil || entry.Profile.RowCountApprox <= 0 {
				continue
			}
			if len(entry.Profile.NumericStats) == 0 {
				continue
			}
			found = entry
			break
		}
		if found == nil {
			t.Skip("no seeded table with numeric columns found in this dialect's fixture")
		}
		t.Logf("using table %q (row_count=%d) for enrichment assertions (dialect=%s)",
			found.Name, found.Profile.RowCountApprox, dbType)

		assert.NotEmptyf(t, found.Profile.SampleRows,
			"expected sample rows on %q (dialect=%s)", found.Name, dbType)

		wholeTableAggregateRan := false
		for col, stats := range found.Profile.NumericStats {
			if stats.Count > 1 {
				wholeTableAggregateRan = true
				t.Logf("numeric_stats[%s]: count=%d min=%s max=%s avg=%s (dialect=%s)",
					col, stats.Count, stats.Min, stats.Max, stats.Avg, dbType)
				break
			}
		}
		assert.Truef(t, wholeTableAggregateRan,
			"expected at least one NumericStats entry with Count > 1 on %q (dialect=%s, proves LIMIT 1 bug is fixed, stats=%+v)",
			found.Name, dbType, found.Profile.NumericStats)
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
