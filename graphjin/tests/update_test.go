//go:build !mysql

package tests_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/v3"
)

func Example_update() {
	gql := `mutation {
		products(id: $id, update: $data) {
			id
			name
		}
	}`

	vars := json.RawMessage(`{ 
		"id": 100,
		"data": { 
			"name": "Updated Product 100",
			"description": "Description for updated product 100"
		} 
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":{"id":100,"name":"Updated Product 100"}}
}

func Example_updateMultipleRelatedTables1() {
	gql := `mutation {
		purchases(id: $id, update: $data) {
			quantity
			customer {
				full_name
			}
			product {
				description
			}
		}
	}`

	vars := json.RawMessage(`{
		"id": 100,
		"data": {
			"quantity": 6,
			"customer": {
				"full_name": "Updated user related to purchase 100"
			},
			"product": {
				"description": "Updated product related to purchase 100"
			}
		}
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"purchases":{"customer":{"full_name":"Updated user related to purchase 100"},"product":{"description":"Updated product related to purchase 100"},"quantity":6}}
}

func Example_updateTableAndConnectToRelatedTables() {
	gql := `mutation {
		users(id: $id, update: $data) {
			full_name
			products {
				id
			}
		}
	}`

	vars := json.RawMessage(`{
		"id": 100,
		"data": {
			"full_name": "Updated user 100",
			"products": {
				"connect": { "id": 99 },
				"disconnect": { "id": 100 }
			}
		}
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"users":{"full_name":"Updated user 100","products":[{"id":99}]}}
}

func Example_updateTableAndRelatedTable() {
	gql := `mutation {
		users(id: $id, update: $data) {
			full_name
			products {
				id
			}
		}
	}`

	vars := json.RawMessage(`{
		"id": 90,
		"data": {
			"full_name": "Updated user 90",
			"products": {
				"where": { "id": { "gt": 1 } },
				"name": "Updated Product 90"
			}
		}
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"users":{"full_name":"Updated user 90","products":[{"id":90}]}}
}

func Example_setArrayColumnToValue() {
	gql := `mutation {
		products(where: { id: 100 }, update: { tags: ["super", "great", "wow"] }) {
			id
			tags
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}

	// Output: {"products":[{"id":100,"tags":["super","great","wow"]}]}
}

func Example_setArrayColumnToEmpty() {
	gql := `mutation {
		products(where: { id: 100 }, update: { tags: [] }) {
			id
			tags
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}

	// Output: {"products":[{"id":100,"tags":[]}]}
}
