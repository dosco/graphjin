package tests_test

import (
	"context"
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

	t.Run("Sections", func(t *testing.T) {
		assert.NotEmpty(t, dm.CombinedSection("syntax"))
		assert.NotEmpty(t, dm.CombinedSection("tables"))
		assert.NotEmpty(t, dm.CombinedSection("insights"))
		assert.NotEmpty(t, dm.Combined())
	})

	t.Run("Caching", func(t *testing.T) {
		md1 := dm.Combined()
		md2 := dm.Combined()
		assert.Equal(t, md1, md2)

		doc := dm.Get(gj.DefaultDatabase())
		require.NotNil(t, doc)
		assert.NotEmpty(t, doc.Hash)

		all := dm.GetAll()
		assert.GreaterOrEqual(t, len(all), 1)
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
		case doc := <-ds.Result:
			require.NotNil(t, doc)
			assert.NotEmpty(t, doc.Tables)
		case <-time.After(5 * time.Second):
			t.Fatal("no initial document")
		}
	})

	t.Run("DatabaseNames", func(t *testing.T) {
		names := gj.DatabaseNames()
		assert.GreaterOrEqual(t, len(names), 1)
		assert.Contains(t, names, gj.DefaultDatabase())
	})
}
