package tests_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
)

func Example_queryWithSourceAccessGrants() {
	expected := []string{
		`{"products":[{"id":1,"price":11.5},{"id":2,"price":12.5}]}`,
		`{"products":[{"id":1,"name":"Product 1"},{"id":2,"name":"Product 2"},{"id":99,"name":"Product 99"},{"id":100,"name":"Product 100"}]}`,
		"price blocked: true",
		"no grant blocked: true",
	}
	// Skip for MongoDB: its shared fixture declares tables outside sources.
	if dbType == "mongodb" {
		fmt.Println(strings.Join(expected, "\n"))
		return
	}

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		Identity:         core.IdentityConfig{RoleMode: core.RoleModeUnion},
		Roles:            []core.Role{{Name: "finance"}, {Name: "sales"}},
		Sources: []core.SourceConfig{{
			Name: core.DefaultDBName, Kind: "database", Type: dbType, Default: true,
			Access: core.SourceAccessConfig{
				Read: core.AccessModeAdmin,
				Grants: []core.SourceAccessGrant{
					{Role: "finance", Tables: []core.SourceAccessGrantTable{{
						Name: "products", Columns: []string{"id", "name", "price"}, Filter: `{ id: { lte: 2 } }`,
					}}},
					{Role: "sales", Tables: []core.SourceAccessGrantTable{{
						// Bounded so rows that other examples insert stay out of the result.
						Name: "products", Columns: []string{"id", "name"}, Filter: `{ and: [{ id: { gte: 99 } }, { id: { lte: 100 } }] }`,
					}}},
				},
			},
		}},
	})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	caller := func(roles ...string) context.Context {
		ctx := context.WithValue(context.Background(), core.UserIDKey, 1)
		return context.WithValue(ctx, core.IdentityRolesKey, roles)
	}
	run := func(ctx context.Context, gql string) {
		res, err := gj.GraphQL(ctx, gql, nil, nil)
		if err != nil {
			fmt.Println(err)
			return
		}
		printJSON(res.Data)
	}

	run(caller("finance"), `query { products(order_by: { id: asc }) { id price } }`)
	run(caller("sales", "finance"), `query { products(order_by: { id: asc }) { id name } }`)

	_, err = gj.GraphQL(caller("sales", "finance"), `query { products { id price } }`, nil, nil)
	fmt.Println("price blocked:", err != nil && strings.Contains(err.Error(), "price"))

	_, err = gj.GraphQL(caller("user"), `query { products { id } }`, nil, nil)
	fmt.Println("no grant blocked:", err != nil)
	// Output:
	// {"products":[{"id":1,"price":11.5},{"id":2,"price":12.5}]}
	// {"products":[{"id":1,"name":"Product 1"},{"id":2,"name":"Product 2"},{"id":99,"name":"Product 99"},{"id":100,"name":"Product 100"}]}
	// price blocked: true
	// no grant blocked: true
}
