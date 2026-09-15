package qcode_test

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// A role filter that does not compile must stop the role from loading.
// Dropping it would remove the row restriction it was written to enforce.
func TestAddRoleRejectsInvalidQueryFilters(t *testing.T) {
	cases := []struct {
		name   string
		filter string
	}{
		{name: "unknown column", filter: `{ no_such_column: { eq: 1 } }`},
		{name: "unknown nested table", filter: `{ no_such_table: { id: { eq: 1 } } }`},
		{name: "missing operator", filter: `{ id: { no_such_op: 1 } }`},
		{name: "empty object", filter: `{}`},
		{name: "empty and", filter: `{ and: [] }`},
		{name: "empty or", filter: `{ or: [] }`},
		{name: "empty nested object", filter: `{ id: {} }`},
		{name: "or of empty objects", filter: `{ or: [{}, {}] }`},
		{name: "and with an empty branch", filter: `{ and: [{ id: { eq: 1 } }, {}] }`},
		{name: "or with an empty branch", filter: `{ or: [{ id: { eq: 1 } }, {}] }`},
		{name: "empty list value", filter: `{ id: { in: [] } }`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			qc, err := qcode.NewCompiler(dbs, qcode.Config{})
			if err != nil {
				t.Fatal(err)
			}
			err = qc.AddRole("user", "public", "products", qcode.TRConfig{
				Query: qcode.QueryConfig{Filters: []string{tc.filter}},
			})
			if err == nil {
				t.Fatalf("AddRole accepted invalid filter %s", tc.filter)
			}
		})
	}
}

func TestAddRoleRejectsInvalidWriteFilters(t *testing.T) {
	bad := []string{`{ no_such_column: { eq: 1 } }`}

	configs := map[string]qcode.TRConfig{
		"update": {Update: qcode.UpdateConfig{Filters: bad}},
		"upsert": {Upsert: qcode.UpsertConfig{Filters: bad}},
		"delete": {Delete: qcode.DeleteConfig{Filters: bad}},
	}
	for name, trc := range configs {
		t.Run(name, func(t *testing.T) {
			qc, err := qcode.NewCompiler(dbs, qcode.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := qc.AddRole("user", "public", "products", trc); err == nil {
				t.Fatalf("AddRole accepted invalid %s filter", name)
			}
		})
	}
}

func TestAddRoleAcceptsValidFilters(t *testing.T) {
	valid := []string{
		`{ id: { eq: $user_id } }`,
		`{ or: [{ price: { gt: 10 } }, { name: { eq: "x" } }] }`,
		`false`,
	}
	for _, filter := range valid {
		qc, err := qcode.NewCompiler(dbs, qcode.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
			Query: qcode.QueryConfig{Filters: []string{filter}},
		}); err != nil {
			t.Fatalf("AddRole rejected valid filter %s: %v", filter, err)
		}
		if _, err := qc.Compile([]byte(`query { products { id } }`), nil, "user", ""); err != nil {
			t.Fatalf("compile with filter %s: %v", filter, err)
		}
	}
}
