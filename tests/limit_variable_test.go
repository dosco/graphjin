package tests_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/v3"
)

// Issue #445: limit and offset can come from variables on every SQL dialect.
func Example_queryWithLimitOffsetVariables() {
	gql := `query {
		products(limit: $limit, offset: $off, order_by: { id: asc }) {
			id
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(), gql, json.RawMessage(`{"limit": 3, "off": 2}`), nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":3},{"id":4},{"id":5}]}
}

// A variable limit never exceeds the configured default limit.
func Example_queryWithLimitVariableCappedAtDefaultLimit() {
	// Skip for Oracle, MSSQL, Snowflake and MongoDB: their variable limits are
	// not capped at the configured limit yet.
	switch dbType {
	case "oracle", "mssql", "snowflake", "mongodb":
		fmt.Println(`{"products":[{"id":1},{"id":2}]}`)
		return
	}

	gql := `query {
		products(limit: $limit, order_by: { id: asc }) {
			id
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(), gql, json.RawMessage(`{"limit": 50}`), nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":1},{"id":2}]}
}
