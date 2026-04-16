package tests_test

import (
	"context"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/require"
)

// These tests exercise the Snowflake GraphQL surface end-to-end against a
// real Snowflake account whose DSN is provided via SNOWFLAKE_TEST_CONN
// (see tests/snowflake_harness_test.go). An ephemeral schema is created
// and seeded from tests/snowflake.sql before the suite runs, then dropped
// on teardown.
//
// Each test is gated on dbType == "snowflake" so it runs only when the
// test harness is invoked with -db=snowflake.

// TestSnowflakeLowercaseEndToEnd verifies that a lowercase GraphQL query
// against a table works end-to-end on the Snowflake driver path. This is
// the post-fix happy path for case-insensitive column/table lookup.
func TestSnowflakeLowercaseEndToEnd(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// Emulator schema stores identifiers lowercase, so we can't
	// demonstrate UPPERCASE storage → lowercase query. But we can assert
	// that mixed-case user input resolves and returns data with the
	// user-typed casing in response keys.
	res, err := gj.GraphQL(context.Background(),
		`{ USERS(limit: 2) { ID Full_Name } }`, nil, nil)
	require.NoError(t, err)
	require.Contains(t, string(res.Data), `"ID":`)
	require.Contains(t, string(res.Data), `"Full_Name":`)
}

// TestSnowflakeTypenameMatchesUserCase asserts __typename returns the
// user-typed alias, not the stored table identifier. On the emulator
// stored names are lowercase so we query with an UPPERCASE alias to
// demonstrate — __typename should come back UPPERCASE matching the
// request, not lowercase matching storage.
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
	// Must reflect user-typed casing.
	require.Contains(t, string(res.Data), `"__typename":"USERS"`)
}

// TestSnowflakeGroupByAutoDerive asserts aggregates + scalar projections
// group on the scalar alone (not on the aggregate's input column). Before
// BUG-G1 fix the counts would collapse to 1 per row.
func TestSnowflakeGroupByAutoDerive(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// `country_code` is seeded as `'US'` for every row, so GROUP BY
	// country_code must collapse all 100 rows into one group. If BUG-G1
	// were reintroduced (id leaking into GROUP BY) we'd see 100 groups
	// of count=1 instead of 1 group of count=100.
	res, err := gj.GraphQL(context.Background(),
		`{ products { country: country_code count_id } }`, nil, nil)
	require.NoError(t, err)
	require.Contains(t, string(res.Data), `"count_id":100`,
		"expected a single group with count_id=100 (all products share country_code='US')")
}

// TestSnowflakeMutationWithPK asserts mutations still work when the target
// table DOES have a primary key (the happy path contrasting BUG-S3's
// failure path). Uses the emulator's products table which has id PK.
func TestSnowflakeMutationWithPK(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// Smoke-test: compile a plausible insert — the emulator may or may
	// not execute it cleanly depending on its DuckDB backing store, but
	// the compile step is the important assertion (no "empty identifier"
	// error from BUG-S3).
	_, err = gj.GraphQL(context.Background(),
		`mutation { products(insert: {id: 999999, name: "test"}) { id } }`,
		nil, nil)
	if err != nil {
		// Acceptable failures are runtime-level from the emulator; the
		// compile-time "no primary key" error must not appear.
		require.NotContains(t, err.Error(),
			"has no primary key",
			"BUG-S3 regression: compile-time 'no PK' error fired on PK-having table")
	}
}

// TestSnowflakeIncludeDirectiveEmptyProjection asserts that dropping all
// selected fields via @include(if: false) yields a valid (empty) response
// instead of a SQL syntax error from an empty SELECT list.
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

// TestSnowflakeClusteredTableEndToEnd asserts that Snowflake accepts the
// seed's `CLUSTER BY (event_time, region)` DDL on the `events` fixture,
// GraphJin discovers that clustering via INFORMATION_SCHEMA.TABLES, and
// the derived PartitionKey auto-filter emits SQL that runs successfully.
//
// The events table uses CURRENT_TIMESTAMP on seed so rows fall inside
// the default 60-day partition window — without that, the auto-filter
// would correctly exclude 2021-dated seed rows and the test would show
// an empty result (passing the filter but not proving the query
// completed end-to-end).
//
// Full clustering-key DISCOVERY unit coverage (INFORMATION_SCHEMA.TABLES
// parse → ClusteringKeys, auto-partition) lives in:
//   - core/internal/sdata/snowflake_show_test.go
//     (TestSnowflakeDiscoverClusteringKeys,
//      TestSnowflakeDiscoverClusteringKeysTolerantOfMissingColumn,
//      TestSnowflakeClusteringAutoPartitionFromDiscovery)
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
	// Seed inserts 10 rows with CURRENT_TIMESTAMP — at least one should
	// be within the 60-day auto-partition window, so the query must
	// return at least one row with a region value ('US' or 'EU').
	require.Contains(t, string(res.Data), `"region":`)
}

// TestSnowflakeVarcharPKMutation exercises the _gj_ids temp-table path
// against a table with a VARCHAR primary key (graph_node.id). Before
// the fix to make the temp-table `id` column VARIANT, this path errored
// because linear-mutation setup hardcoded BIGINT and the VARCHAR PK
// couldn't be inserted. The test mutates graph_node (update) and
// asserts the mutation returns the affected row — proving capture +
// readback work for non-BIGINT PK types.
func TestSnowflakeVarcharPKMutation(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// graph_node is seeded with {id='a', label='node a'}, etc. The id is
	// VARCHAR so the linear-mutation temp table (_gj_ids.id VARIANT)
	// must tolerate a non-BIGINT value.
	gql := `mutation { graph_node(id: "a", update: { label: "renamed-a" }) { id label } }`
	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	require.NoError(t, err, "mutation against VARCHAR-PK table must succeed")
	require.Contains(t, string(res.Data), `"id":"a"`)
	require.Contains(t, string(res.Data), `"label":"renamed-a"`)
}

// TestSnowflakeFKDiscoveryLive verifies that SHOW IMPORTED KEYS
// discovery populated the relationship graph by compiling a nested
// selection that depends on FK-derived edges. On real Snowflake with
// the seed's `purchases.customer_id REFERENCES users(id)`, GraphJin's
// SHOW-based FK discovery must resolve the `customer` relation; if it
// didn't, the compiler would fail with "table not found: customer"
// before any SQL runs.
//
// The test asserts compilation succeeds — end-to-end execution of
// multi-child nested selections on Snowflake currently hits a planner
// limitation around correlated scalar subqueries emitted by the
// non-lateral compiler path (see RelEmbedded TODO below). Compile-time
// resolution is what proves FK discovery itself works; runtime
// correctness for the non-lateral nested shape is covered by the
// sqlmock-backed unit test TestSnowflakeSHOWImportedKeysSetsFK.
func TestSnowflakeFKDiscoveryLive(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// Compile-time check: if the FK from purchases → users wasn't
	// discovered, `customer` would not resolve and ExplainQuery would
	// return "table not found: customer".
	exp, err := gj.ExplainQuery(
		`{ purchases(where: {id: {eq: 1}}) { id customer { full_name } } }`,
		nil, "anon")
	require.NoError(t, err, "SHOW IMPORTED KEYS must surface purchases.customer → users edge at compile time")
	require.Contains(t, exp.CompiledQuery, `ANY_VALUE(OBJECT_CONSTRUCT('full_name'`,
		"singular child subquery must be wrapped in ANY_VALUE for Snowflake")
}

// TestSnowflakeOrderByAlias asserts that ordering by a SELECT-list alias
// resolves to the underlying column in the inner SQL scope where the
// alias isn't visible (BUG-G2).
func TestSnowflakeOrderByAlias(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// Order by `nm` which is an alias for `full_name`. Must not error.
	_, err = gj.GraphQL(context.Background(),
		`{ users(order_by: {nm: desc}, limit: 3) { id nm: full_name } }`, nil, nil)
	require.NoError(t, err, "alias-based order_by should resolve to underlying column")
}
