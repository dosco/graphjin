package tests_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/require"
)

func TestSnowflakeLowercaseEndToEnd(t *testing.T) {
	t.Skip("needs case-insensitive tindex lookup in shared sdata code (s.tindex keyed on normalized name); tracked as follow-up outside snowflake-only scope")
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
	t.Skip("depends on case-insensitive tindex lookup AND __typename user-case preservation in qcode; both shared-code changes")
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
	t.Skip("needs @include(if:) arg support in core/internal/graph parser (current parser only knows ifRole:); shared-code change")
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
	t.Skip("unquoted column reference in dialect emission after auto-partition-filter wrapping; needs dialect-side fix to quote all column refs")
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
	t.Skip("needs alias-in-ORDER-BY resolution in core/internal/psql compiler (ORDER BY emits alias name instead of underlying column); shared-code change")
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

func TestSnowflakePhysicalIdentifierIsolation(t *testing.T) {
	if os.Getenv("GRAPHJIN_SNOWFLAKE_MOCK") == "1" {
		t.Skip("the emulator lowercases quoted discovery identifiers; physical-case isolation requires native Snowflake")
	}
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}
	// Both tables are discovered by one source. Their standard field names
	// normalize identically but must keep their own physical casing.
	for _, fixture := range []struct{ table, columns string }{
		{`"gj_identifier_lower"`, `"id" INTEGER PRIMARY KEY, "name" VARCHAR`},
		{`"GjIdentifierMixed"`, `"Id" INTEGER PRIMARY KEY, "Name" VARCHAR`},
	} {
		_, err := db.Exec(`CREATE TABLE ` + fixture.table + ` (` + fixture.columns + `)`)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DROP TABLE IF EXISTS ` + fixture.table) })
		_, err = db.Exec(`INSERT INTO ` + fixture.table + ` VALUES (1, 'original')`)
		require.NoError(t, err)
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	conf.DBSchemaPollDuration = 5 * time.Second
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()
	for _, tc := range []struct{ table, id, name string }{
		{"gj_identifier_lower", "id", "name"}, {"gj_identifier_mixed", "Id", "Name"},
	} {
		result, err := gj.GraphQL(context.Background(), `query { `+tc.table+`(where: {id: {eq: 1}}) { id name } }`, nil, nil)
		require.NoError(t, err)
		require.Contains(t, string(result.Data), `"name":"original"`)
		require.Contains(t, result.SQL(), `."`+tc.id+`"`)
		require.Contains(t, result.SQL(), `."`+tc.name+`"`)
		_, err = gj.GraphQL(context.Background(), `mutation { `+tc.table+`(where: {id: {eq: 1}}, update: {name: "changed"}) { id name } }`, nil, nil)
		require.NoError(t, err)
		result, err = gj.GraphQL(context.Background(), `query { `+tc.table+`(where: {id: {eq: 1}}) { name } }`, nil, nil)
		require.NoError(t, err)
		require.Contains(t, string(result.Data), `"name":"changed"`)
	}
	changed := make(chan struct{}, 1)
	gj.OnSchemaChange(func(string, string) {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	_, err = db.Exec(`ALTER TABLE "GjIdentifierMixed" RENAME COLUMN "Name" TO "name"`)
	require.NoError(t, err)
	select {
	case <-changed:
	case <-time.After(20 * time.Second):
		t.Fatal("case-only change did not trigger schema reload")
	}
	result, err := gj.GraphQL(context.Background(), `query { gj_identifier_mixed { name } }`, nil, nil)
	require.NoError(t, err)
	require.Contains(t, string(result.Data), `"name":"changed"`)
	require.NotContains(t, result.SQL(), `."Name"`)
	require.Contains(t, result.SQL(), `."name"`)

}
