package tests_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func analyticsPartitionConfig(analytics bool) *core.Config {
	return newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		AnalyticsMode:    analytics,
		Tables: []core.Table{{
			Name: "users",
			Partition: &core.PartitionConfig{
				Column:           "created_at",
				DefaultRangeDays: 30,
			},
		}},
	})
}

func TestAnalyticsMode_PartitionRequiredNoFilter(t *testing.T) {
	if dbType != "postgres" {
		t.Skip("gated to postgres")
	}
	gj, err := core.NewGraphJin(analyticsPartitionConfig(true), db)
	require.NoError(t, err)
	t.Cleanup(func() { gj.Close() })

	_, err = gj.GraphQL(context.Background(), `{ users { id } }`, nil, nil)
	require.Error(t, err, "OLAP mode must refuse queries missing the partition filter")

	msg := err.Error()
	for _, needle := range []string{"users", "created_at"} {
		assert.True(t, strings.Contains(msg, needle),
			"error should mention %q (actionable for callers), got: %s", needle, msg)
	}
}

func TestAnalyticsMode_PartitionSuppliedSucceeds(t *testing.T) {
	if dbType != "postgres" {
		t.Skip("gated to postgres")
	}
	gj, err := core.NewGraphJin(analyticsPartitionConfig(true), db)
	require.NoError(t, err)
	t.Cleanup(func() { gj.Close() })

	res, err := gj.GraphQL(context.Background(),
		`{ users(where: { created_at: { lt: "2100-01-01" } }) { id } }`,
		nil, nil)
	require.NoError(t, err, "OLAP mode with explicit partition filter must succeed")

	var out struct {
		Users []struct {
			ID int `json:"id"`
		} `json:"users"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &out))
	assert.Equal(t, 100, len(out.Users), "explicit filter must not apply default 30-day window; all 100 seeded users returned")
}

func TestAnalyticsMode_ImplicitPartitionFromCreatedAt(t *testing.T) {
	if dbType != "postgres" {
		t.Skip("gated to postgres")
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, AnalyticsMode: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	t.Cleanup(func() { gj.Close() })

	_, err = gj.GraphQL(context.Background(), `{ users { id } }`, nil, nil)
	require.Error(t, err, "analytics_mode must detect users.created_at and require a filter")
	msg := err.Error()
	for _, needle := range []string{"users", "created_at"} {
		assert.True(t, strings.Contains(msg, needle),
			"error should mention %q, got: %s", needle, msg)
	}
}

func TestAnalyticsMode_UnrestrictedBypassesImplicitTemporal(t *testing.T) {
	if dbType != "postgres" {
		t.Skip("gated to postgres")
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, AnalyticsMode: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	t.Cleanup(func() { gj.Close() })

	res, err := gj.GraphQL(context.Background(),
		`{ users(unrestricted: true) { id } }`, nil, nil)
	require.NoError(t, err, "unrestricted:true should bypass the implicit created_at gate")
	var out struct {
		Users []struct {
			ID int `json:"id"`
		} `json:"users"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &out))
	assert.Equal(t, 100, len(out.Users))
}

func TestAnalyticsMode_PartitionNoneConfigAllowsUnfiltered(t *testing.T) {
	if dbType != "postgres" {
		t.Skip("gated to postgres")
	}
	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		AnalyticsMode:    true,
		Tables: []core.Table{{
			Name:      "users",
			Partition: &core.PartitionConfig{None: true},
		}},
	})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	t.Cleanup(func() { gj.Close() })

	res, err := gj.GraphQL(context.Background(), `{ users { id } }`, nil, nil)
	require.NoError(t, err, "partition.none must whitelist the table")
	var out struct {
		Users []struct {
			ID int `json:"id"`
		} `json:"users"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &out))
	assert.Equal(t, 100, len(out.Users))
}

