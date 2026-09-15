package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
)

func unionRolesConfig(mode string) *core.Config {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	conf.Identity.RoleMode = mode
	if err := conf.AddRoleTable("finance", "products", core.Query{
		Filters: []string{`{ id: { lte: 2 } }`},
		Columns: []string{"id", "name", "price"},
	}); err != nil {
		panic(err)
	}
	if err := conf.AddRoleTable("sales", "products", core.Query{
		// Bounded so rows that other examples insert stay out of the result.
		Filters: []string{`{ and: [{ id: { gte: 99 } }, { id: { lte: 100 } }] }`},
		Columns: []string{"id", "name"},
	}); err != nil {
		panic(err)
	}
	return conf
}

func unionRolesContext() context.Context {
	ctx := context.WithValue(context.Background(), core.UserIDKey, 1)
	return context.WithValue(ctx, core.IdentityRolesKey, []string{"sales", "finance"})
}

func Example_queryUnionRolesMergesRules() {
	// Skip for MongoDB: role filters with column allowlists are SQL-only here.
	if dbType == "mongodb" {
		fmt.Println(`{"products":[{"id":1,"name":"Product 1"},{"id":2,"name":"Product 2"},{"id":99,"name":"Product 99"},{"id":100,"name":"Product 100"}]}`)
		fmt.Println("price blocked: true")
		return
	}

	gj, err := core.NewGraphJin(unionRolesConfig(core.RoleModeUnion), db)
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	ctx := unionRolesContext()
	res, err := gj.GraphQL(ctx, `query {
		products(order_by: { id: asc }) { id name }
	}`, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}

	// finance may read price on its own rows only, so the union must not
	// expose price on the sales rows.
	_, err = gj.GraphQL(ctx, `query {
		products(order_by: { id: asc }) { id price }
	}`, nil, nil)
	fmt.Println("price blocked:", err != nil && strings.Contains(err.Error(), "price"))
	// Output:
	// {"products":[{"id":1,"name":"Product 1"},{"id":2,"name":"Product 2"},{"id":99,"name":"Product 99"},{"id":100,"name":"Product 100"}]}
	// price blocked: true
}

func Example_queryUnionRolesFirstModeUnchanged() {
	// Skip for MongoDB: role filters with column allowlists are SQL-only here.
	if dbType == "mongodb" {
		fmt.Println(`{"products":[{"id":1,"price":11.5},{"id":2,"price":12.5}]}`)
		return
	}

	gj, err := core.NewGraphJin(unionRolesConfig(core.RoleModeFirst), db)
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	res, err := gj.GraphQL(unionRolesContext(), `query {
		products(order_by: { id: asc }) { id price }
	}`, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":1,"price":11.5},{"id":2,"price":12.5}]}
}

func Example_queryWithGroupsFilter() {
	// Skip for MongoDB: role filters with column allowlists are SQL-only here.
	if dbType == "mongodb" {
		fmt.Println(`{"users":[{"email":"user1@test.com","id":1},{"email":"user3@test.com","id":3}]}`)
		fmt.Println(`{"users":[]}`)
		return
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	if err := conf.AddRoleTable("member", "users", core.Query{
		Filters: []string{`{ email: { in: $user_groups } }`},
		Columns: []string{"id", "email"},
	}); err != nil {
		panic(err)
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	gql := `query {
		users(order_by: { id: asc }) { id email }
	}`
	// A request variable named user_groups must not replace the trusted groups.
	vars := json.RawMessage(`{ "user_groups": ["user2@test.com"] }`)

	ctx := context.WithValue(context.Background(), core.UserIDKey, 1)
	ctx = context.WithValue(ctx, core.IdentityRolesKey, []string{"member"})
	withGroups := context.WithValue(ctx, core.IdentityVarsKey, map[string]interface{}{
		core.UserGroupsVar: []string{"user1@test.com", "user3@test.com"},
	})

	for _, c := range []context.Context{withGroups, ctx} {
		res, err := gj.GraphQL(c, gql, vars, nil)
		if err != nil {
			fmt.Println(err)
			continue
		}
		printJSON(res.Data)
	}
	// Output:
	// {"users":[{"email":"user1@test.com","id":1},{"email":"user3@test.com","id":3}]}
	// {"users":[]}
}

func Example_queryUnionRolesWithGraphQLRolesQuery() {
	// Skip for MongoDB: role filters with column allowlists are SQL-only here.
	if dbType == "mongodb" {
		fmt.Println(`{"products":[{"id":1,"name":"Product 1"},{"id":2,"name":"Product 2"},{"id":99,"name":"Product 99"},{"id":100,"name":"Product 100"}]}`)
		return
	}

	conf := unionRolesConfig(core.RoleModeUnion)
	conf.RolesQuery = `query {
		users(id: $user_id) {
			id
		}
	}`
	for i := range conf.Roles {
		conf.Roles[i].Match = "id = 1"
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	// No role claims: the roles query matches both finance and sales.
	ctx := context.WithValue(context.Background(), core.UserIDKey, 1)
	res, err := gj.GraphQL(ctx, `query {
		products(order_by: { id: asc }) { id name }
	}`, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":1,"name":"Product 1"},{"id":2,"name":"Product 2"},{"id":99,"name":"Product 99"},{"id":100,"name":"Product 100"}]}
}

func Example_queryUnionRolesWithSQLRolesQuery() {
	// Skip for MongoDB: a SQL roles_query needs a SQL database.
	if dbType == "mongodb" {
		fmt.Println(`{"products":[{"id":1,"name":"Product 1"},{"id":2,"name":"Product 2"},{"id":99,"name":"Product 99"},{"id":100,"name":"Product 100"}]}`)
		fmt.Println(`{"products":[{"id":99,"name":"Product 99"},{"id":100,"name":"Product 100"}]}`)
		return
	}

	conf := unionRolesConfig(core.RoleModeUnion)
	conf.RolesQuery = `SELECT * FROM users WHERE id = $user_id`
	for i := range conf.Roles {
		switch conf.Roles[i].Name {
		case "finance":
			conf.Roles[i].Match = "id = 1"
		case "sales":
			conf.Roles[i].Match = "id < 10"
		}
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	// User 1 matches finance and sales; user 5 matches sales only.
	for _, userID := range []int{1, 5} {
		ctx := context.WithValue(context.Background(), core.UserIDKey, userID)
		res, err := gj.GraphQL(ctx, `query {
			products(order_by: { id: asc }) { id name }
		}`, nil, nil)
		if err != nil {
			fmt.Println(err)
			continue
		}
		printJSON(res.Data)
	}
	// Output:
	// {"products":[{"id":1,"name":"Product 1"},{"id":2,"name":"Product 2"},{"id":99,"name":"Product 99"},{"id":100,"name":"Product 100"}]}
	// {"products":[{"id":99,"name":"Product 99"},{"id":100,"name":"Product 100"}]}
}
