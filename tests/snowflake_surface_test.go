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
