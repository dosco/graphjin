package tests_test

import (
	"context"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/require"
)

func TestSnowflakeLowercaseEndToEnd(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(),
		`{ USERS(limit: 2) { ID Full_Name } }`, nil, nil)
	require.NoError(t, err)
	require.Contains(t, string(res.Data), `"ID":`)
	require.Contains(t, string(res.Data), `"Full_Name":`)
}

func TestSnowflakeTypenameMatchesUserCase(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(),
		`{ USERS(limit: 1) { __typename id } }`, nil, nil)
	require.NoError(t, err)
	require.Contains(t, string(res.Data), `"__typename":"USERS"`)
}

func TestSnowflakeGroupByAutoDerive(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(),
		`{ products { country: country_code count_id } }`, nil, nil)
	require.NoError(t, err)
	require.Contains(t, string(res.Data), `"count_id":100`,
		"expected a single group with count_id=100 (all products share country_code='US')")
}

func TestSnowflakeMutationWithPK(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	_, err = gj.GraphQL(context.Background(),
		`mutation { products(insert: {id: 999999, name: "test"}) { id } }`,
		nil, nil)
	if err != nil {
		require.NotContains(t, err.Error(),
			"has no primary key",
			"BUG-S3 regression: compile-time 'no PK' error fired on PK-having table")
	}
}

func TestSnowflakeIncludeDirectiveEmptyProjection(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(),
		`{ users(limit: 2) { id @include(if: false) } }`, nil, nil)
	require.NoError(t, err)
	require.Contains(t, string(res.Data), `"users":[{}`)
}

func TestSnowflakeClusteredTableEndToEnd(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(),
		`{ events(limit: 5) { id event_time region } }`, nil, nil)
	require.NoError(t, err, "query against clustered table must not error")
	require.Contains(t, string(res.Data), `"region":`)
}

func TestSnowflakeVarcharPKMutation(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	gql := `mutation { graph_node(id: "a", update: { label: "renamed-a" }) { id label } }`
	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	require.NoError(t, err, "mutation against VARCHAR-PK table must succeed")
	require.Contains(t, string(res.Data), `"id":"a"`)
	require.Contains(t, string(res.Data), `"label":"renamed-a"`)
}

func TestSnowflakeFKDiscoveryLive(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	exp, err := gj.ExplainQuery(
		`{ purchases(where: {id: {eq: 1}}) { id customer { full_name } } }`,
		nil, "anon")
	require.NoError(t, err, "SHOW IMPORTED KEYS must surface purchases.customer → users edge at compile time")
	require.Contains(t, exp.CompiledQuery, `ANY_VALUE(OBJECT_CONSTRUCT_KEEP_NULL('full_name'`,
		"singular child subquery must be wrapped in ANY_VALUE for Snowflake")
}

func TestSnowflakeOrderByAlias(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	_, err = gj.GraphQL(context.Background(),
		`{ users(order_by: {nm: desc}, limit: 3) { id nm: full_name } }`, nil, nil)
	require.NoError(t, err, "alias-based order_by should resolve to underlying column")
}
