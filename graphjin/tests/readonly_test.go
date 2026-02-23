package tests_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

// mutationGQLs returns the standard set of mutation queries used across
// read-only scenarios. Each entry is {label, gql, vars}.
func mutationGQLs() []struct {
	label string
	gql   string
	vars  json.RawMessage
} {
	return []struct {
		label string
		gql   string
		vars  json.RawMessage
	}{
		{
			label: "insert",
			gql: `mutation {
				users(insert: { id: $id, email: $email, full_name: $fullName, stripe_id: $stripeID, category_counts: $categoryCounts }) {
					id
				}
			}`,
			vars: json.RawMessage(`{
				"id": 9001,
				"email": "readonly@test.com",
				"fullName": "Read Only",
				"stripeID": "stripe_readonly",
				"categoryCounts": [{"category_id": 1, "count": 1}]
			}`),
		},
		{
			label: "update",
			gql: `mutation {
				users(update: { full_name: $fullName }, where: { id: { eq: 3 } }) {
					id
				}
			}`,
			vars: json.RawMessage(`{"fullName": "Updated Name"}`),
		},
		{
			label: "delete",
			gql: `mutation {
				users(delete: true, where: { id: { eq: 9001 } }) {
					id
				}
			}`,
		},
	}
}

// assertMutationBlocked runs every mutation in mutationGQLs and asserts each
// one returns an error whose message contains wantSubstr.
func assertMutationBlocked(t *testing.T, gj *core.GraphJin, ctx context.Context, wantSubstr string) {
	t.Helper()
	for _, m := range mutationGQLs() {
		t.Run(m.label+"_blocked", func(t *testing.T) {
			_, err := gj.GraphQL(ctx, m.gql, m.vars, nil)
			if err == nil {
				t.Fatalf("expected %s to be blocked on read-only database", m.label)
			}
			if !strings.Contains(err.Error(), wantSubstr) {
				t.Fatalf("expected error containing %q, got: %s", wantSubstr, err)
			}
		})
	}
}

// assertQueryAllowed runs a simple SELECT and asserts it succeeds.
func assertQueryAllowed(t *testing.T, gj *core.GraphJin, ctx context.Context) {
	t.Helper()
	t.Run("query_allowed", func(t *testing.T) {
		gql := `query {
			users(where: { id: { eq: 3 } }) {
				id
				email
			}
		}`
		res, err := gj.GraphQL(ctx, gql, nil, nil)
		if err != nil {
			t.Fatalf("expected query to succeed on read-only database, got: %s", err)
		}
		if len(res.Data) == 0 {
			t.Fatal("expected non-empty query result")
		}
	})
}

// ---------------------------------------------------------------------------
// Scenario 1 – read_only + explicit roles + explicit tables
// This is the "happy path" where NormalizeDatabases propagation works.
// The role-level block fires during compilation ("blocked").
// ---------------------------------------------------------------------------

func TestReadOnlyDB_WithRolesAndTables(t *testing.T) {
	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		Databases: map[string]core.DatabaseConfig{
			"default": {Type: dbType, ReadOnly: true},
		},
		Tables: []core.Table{
			{Name: "users"},
		},
		Roles: []core.Role{
			{
				Name:   "user",
				Tables: []core.RoleTable{{Name: "users"}},
			},
		},
	})

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	assertMutationBlocked(t, gj, ctx, "blocked")
	assertQueryAllowed(t, gj, ctx)
}

// ---------------------------------------------------------------------------
// Scenario 2 – read_only + NO roles, NO tables
// Regression case: MCP saves config with roles: [] and no tables.
// NormalizeDatabases has nothing to propagate to.  The absolute engine-level
// gate must catch this ("read-only").
// ---------------------------------------------------------------------------

func TestReadOnlyDB_NoRolesNoTables(t *testing.T) {
	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		Databases: map[string]core.DatabaseConfig{
			"default": {Type: dbType, ReadOnly: true},
		},
		// No Tables, no Roles — exactly what MCP's syncConfigToViper writes
	})

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	assertMutationBlocked(t, gj, ctx, "read-only")
	assertQueryAllowed(t, gj, ctx)
}

// ---------------------------------------------------------------------------
// Scenario 3 – read_only + roles but NO matching table config
// The role entry exists ("users") but conf.Tables is empty, so
// NormalizeDatabases can't match the role table to a database.
// ---------------------------------------------------------------------------

func TestReadOnlyDB_RolesButNoTableConfig(t *testing.T) {
	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		Databases: map[string]core.DatabaseConfig{
			"default": {Type: dbType, ReadOnly: true},
		},
		Roles: []core.Role{
			{
				Name:   "user",
				Tables: []core.RoleTable{{Name: "users"}},
			},
		},
		// Tables deliberately empty — propagation has no table→database mapping
	})

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	assertMutationBlocked(t, gj, ctx, "read-only")
	assertQueryAllowed(t, gj, ctx)
}

// ---------------------------------------------------------------------------
// Scenario 4 – read_only + anon role (no user ID in context)
// Ensures the gate works regardless of which role is active.
// ---------------------------------------------------------------------------

func TestReadOnlyDB_AnonRole(t *testing.T) {
	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		Databases: map[string]core.DatabaseConfig{
			"default": {Type: dbType, ReadOnly: true},
		},
	})

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	// No UserIDKey → anon role
	ctx := context.Background()
	assertMutationBlocked(t, gj, ctx, "read-only")
	assertQueryAllowed(t, gj, ctx)
}

// ---------------------------------------------------------------------------
// Scenario 5 – writable DB must NOT be affected
// Sanity check: a database without read_only still allows mutations.
// ---------------------------------------------------------------------------

func TestWritableDB_MutationsAllowed(t *testing.T) {
	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		Databases: map[string]core.DatabaseConfig{
			"default": {Type: dbType, ReadOnly: false},
		},
	})

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)

	// Insert should succeed on a writable database
	gql := `mutation {
		users(insert: { id: $id, email: $email, full_name: $fullName, stripe_id: $stripeID, category_counts: $categoryCounts }) {
			id
		}
	}`
	vars := json.RawMessage(`{
		"id": 9002,
		"email": "writable@test.com",
		"fullName": "Writable User",
		"stripeID": "stripe_writable",
		"categoryCounts": [{"category_id": 1, "count": 1}]
	}`)

	_, err = gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		t.Fatalf("expected insert to succeed on writable database, got: %s", err)
	}

	// Clean up
	delGQL := `mutation {
		users(delete: true, where: { id: { eq: $id } }) {
			id
		}
	}`
	delVars := json.RawMessage(`{"id": 9002}`)
	_, _ = gj.GraphQL(ctx, delGQL, delVars, nil)
}
