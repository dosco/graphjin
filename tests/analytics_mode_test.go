package tests_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runUsersQuery(t *testing.T, gj *core.GraphJin) int {
	t.Helper()
	res, err := gj.GraphQL(context.Background(), `{ users { id } }`, nil, nil)
	require.NoError(t, err)
	var out struct {
		Users []struct {
			ID int `json:"id"`
		} `json:"users"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &out))
	return len(out.Users)
}

func TestAnalyticsMode_OffReturns20(t *testing.T) {
	if dbType != "postgres" {
		t.Skip("gated to postgres")
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	t.Cleanup(func() { gj.Close() })

	assert.Equal(t, 20, runUsersQuery(t, gj), "OLTP default should cap at 20 rows")
}

func TestAnalyticsMode_OnReturnsAll(t *testing.T) {
	if dbType != "postgres" {
		t.Skip("gated to postgres")
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, AnalyticsMode: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	t.Cleanup(func() { gj.Close() })

	assert.Equal(t, 100, runUsersQuery(t, gj), "analytics_mode should return all 100 seeded users")
}

func TestAnalyticsMode_SingleAggregateQuery(t *testing.T) {
	if dbType != "postgres" {
		t.Skip("gated to postgres")
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, AnalyticsMode: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	t.Cleanup(func() { gj.Close() })

	res, err := gj.GraphQL(context.Background(),
		`{ users(distinct: [disabled]) { disabled count_id } }`, nil, nil)
	require.NoError(t, err)

	var out struct {
		Users []struct {
			Disabled bool `json:"disabled"`
			CountID  int  `json:"count_id"`
		} `json:"users"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &out))

	require.Len(t, out.Users, 2, "expected 2 rows (one per distinct disabled), got %d", len(out.Users))
	total := 0
	for _, r := range out.Users {
		total += r.CountID
	}
	assert.Equal(t, 100, total, "counts should sum to 100 seeded users (proves no default_limit truncation pre-group)")
}

func TestAnalyticsMode_PerDBOverride(t *testing.T) {
	if dbType != "postgres" {
		t.Skip("gated to postgres")
	}
	defaultName := "default"
	falseVal := false
	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		AnalyticsMode:    true,
		Databases: map[string]core.DatabaseConfig{
			defaultName: {
				Type:          dbType,
				AnalyticsMode: &falseVal,
			},
		},
	})
	assert.False(t, conf.EffectiveAnalyticsMode(defaultName),
		"per-DB override should beat server-level default")
	assert.True(t, conf.EffectiveAnalyticsMode("nonexistent"),
		"unknown DB should inherit server-level default")

	trueVal := true
	conf2 := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		Databases: map[string]core.DatabaseConfig{
			defaultName: {
				Type:          dbType,
				AnalyticsMode: &trueVal,
			},
		},
	})
	assert.True(t, conf2.EffectiveAnalyticsMode(defaultName),
		"per-DB override true should win over server-level false")
}
