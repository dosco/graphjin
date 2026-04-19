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
