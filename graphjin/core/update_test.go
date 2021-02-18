package core_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core"
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

	conf := &core.Config{DBType: dbType, DBSchema: dbSchema, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"products": {"id": 100, "name": "Updated Product 100"}}
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

	conf := &core.Config{DBType: dbType, DBSchema: dbSchema, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"purchases": {"product": {"description": "Updated product related to purchase 100"}, "customer": {"full_name": "Updated user related to purchase 100"}, "quantity": 6}}
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

	conf := &core.Config{DBType: dbType, DBSchema: dbSchema, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": {"products": [{"id": 99}], "full_name": "Updated user 100"}}
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

	conf := &core.Config{DBType: dbType, DBSchema: dbSchema, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": {"products": [{"id": 90}], "full_name": "Updated user 90"}}
}
