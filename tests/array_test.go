//go:build !mysql

package tests_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/v3"
)

func Example_queryParentAndChildrenViaArrayColumn() {
	gql := `
	query {
		products(limit: 2) {
			name
			price
			categories {
				id
				name
			}
		}
		categories {
			name
			products {
				name
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2})
	conf.Tables = []core.Table{
		{
			Name: "products",
			Columns: []core.Column{
				{Name: "category_ids", ForeignKey: "categories.id", Array: true},
			},
		},
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"categories":[{"name":"Category 1","products":[{"name":"Product 1"},{"name":"Product 2"}]},{"name":"Category 2","products":[{"name":"Product 1"},{"name":"Product 2"}]}],"products":[{"categories":[{"id":1,"name":"Category 1"},{"id":2,"name":"Category 2"}],"name":"Product 1","price":11.5},{"categories":[{"id":1,"name":"Category 1"},{"id":2,"name":"Category 2"}],"name":"Product 2","price":12.5}]}
}

func Example_insertIntoTableAndConnectToRelatedTableWithArrayColumn() {
	gql := `mutation {
		products(insert: $data) {
			id
			name
			categories {
				id
				name
			}
		}
	}`

	vars := json.RawMessage(`{
		"data": {
			"id": 2006,
			"name": "Product 2006",
			"description": "Description for product 2006",
			"price": 2016.5,
			"tags": ["Tag 1", "Tag 2"],
			"categories": {
				"connect": { "id": [1, 2, 3, 4, 5] }
			}
		}
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	conf.Tables = []core.Table{
		{Name: "products", Columns: []core.Column{{Name: "category_ids", ForeignKey: "categories.id"}}},
	}

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
	// Output: {"products":[{"categories":[{"id":1,"name":"Category 1"},{"id":2,"name":"Category 2"},{"id":3,"name":"Category 3"},{"id":4,"name":"Category 4"},{"id":5,"name":"Category 5"}],"id":2006,"name":"Product 2006"}]}
}

// TODO: Fix: Does not work in MYSQL
func Example_veryComplexQueryWithArrayColumns() {
	gql := `query {
		products(
			# returns only 1 items
			limit: 1,
	
			# starts from item 10, commented out for now
			# offset: 10,
	
			# orders the response items by highest price
			order_by: { price: desc },
	
			# only items with an id >= 30 and < 30 are returned
			where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) {
			id
			name
			price
			owner {
				full_name
				picture : avatar
				email
				category_counts(limit: 2) {
					count
					category {
						name
					}
				}
			}
			category(limit: 2) {
				id
				name
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	conf.Tables = []core.Table{
		{
			Name:  "category_counts",
			Table: "users",
			Type:  "json",
			Columns: []core.Column{
				{Name: "category_id", Type: "int", ForeignKey: "categories.id"},
				{Name: "count", Type: "int"},
			},
		},
		{
			Name:    "products",
			Columns: []core.Column{{Name: "category_ids", ForeignKey: "categories.id"}},
		},
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		fmt.Println(err)
		return
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	printJSON(res.Data)
	// Output: {"products":[{"category":[{"id":1,"name":"Category 1"},{"id":2,"name":"Category 2"}],"id":27,"name":"Product 27","owner":{"category_counts":[{"category":{"name":"Category 1"},"count":400},{"category":{"name":"Category 2"},"count":600}],"email":"user27@test.com","full_name":"User 27","picture":null},"price":37.5}]}
}
