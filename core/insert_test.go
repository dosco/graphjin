package core_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core"
)

func Example_insert() {
	gql := `mutation {
		user(insert: $data) {
			id
			email
		}
	}`

	vars := json.RawMessage(`{
		"data": {
			"id": 1001,
			"email": "user1001@test.com",
			"full_name": "User 1001",
			"stripe_id": "payment_id_1001",
			"category_counts": [{"category_id": 1, "count": 400},{"category_id": 2, "count": 600}]
		}
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"user": {"id": 1001, "email": "user1001@test.com"}}
}

func Example_insertWithPresets() {
	gql := `mutation {
		product(insert: $data) {
			id
			name
			user {
				id
				email
			}
		}
	}`

	vars := json.RawMessage(`{
		"data": {
			"id": 2001,
			"name": "Product 2001",
			"description": "Description for product 2001",
			"price": 2011.5,
			"tags": ["Tag 1", "Tag 2"],
			"category_ids": [1, 2, 3, 4, 5]
		}
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	err := conf.AddRoleTable("user", "products", core.Insert{
		Presets: map[string]string{"owner_id": "$user_id"},
	})

	if err != nil {
		panic(err)
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"product": {"id": 2001, "name": "Product 2001", "user": {"id": 3, "email": "user3@test.com"}}}
}

func Example_bulkInsert() {
	gql := `mutation {
		users(insert: $data) {
			id
			email
		}
	}`

	vars := json.RawMessage(`{
		"data": [{
			"id": 1002,
			"email": "user1002@test.com",
			"full_name": "User 1002",
			"stripe_id": "payment_id_1002",
			"category_counts": [{"category_id": 1, "count": 400},{"category_id": 2, "count": 600}]
		},
		{
			"id": 1003,
			"email": "user1003@test.com",
			"full_name": "User 1003",
			"stripe_id": "payment_id_1003",
			"category_counts": [{"category_id": 2, "count": 400},{"category_id": 3, "count": 600}]
		}]
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"id": 1002, "email": "user1002@test.com"}, {"id": 1003, "email": "user1003@test.com"}]}
}

func Example_insertIntoMultipleRelatedTables1() {
	gql := `mutation {
		purchase(insert: $data) {
			quantity
			customer {
				id
				full_name
				email
			}
			product {
				id
				name
				price
			}
		}
	}`

	vars := json.RawMessage(`{
		"data": {
			"id": 3001,
			"quantity": 5,
			"customer": {
				"id": 1004,
				"email": "user1004@test.com",
				"full_name": "User 1004",
				"stripe_id": "payment_id_1004",
				"category_counts": [{"category_id": 1, "count": 400},{"category_id": 2, "count": 600}]
			},
			"product": {
				"id": 2002,
				"name": "Product 2002",
				"description": "Description for product 2002",
				"price": 2012.5,
				"tags": ["Tag 1", "Tag 2"],
				"category_ids": [1, 2, 3, 4, 5],
				"owner_id": 3
			}
		}
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"purchase": {"product": {"id": 2002, "name": "Product 2002", "price": 2012.5}, "customer": {"id": 1004, "email": "user1004@test.com", "full_name": "User 1004"}, "quantity": 5}}
}

func Example_insertIntoMultipleRelatedTables2() {
	gql := `mutation {
		user(insert: $data) {
			id
			full_name
			email
			product {
				id
				name
				price
			}
		}
	}`

	vars := json.RawMessage(`{
		"data": {
			"id": 1005,
			"email": "user1005@test.com",
			"full_name": "User 1005",
			"stripe_id": "payment_id_1005",
			"category_counts": [{"category_id": 1, "count": 400},{"category_id": 2, "count": 600}],
			"product": {
				"id": 2003,
				"name": "Product 2003",
				"description": "Description for product 2003",
				"price": 2013.5,
				"tags": ["Tag 1", "Tag 2"],
				"category_ids": [1, 2, 3, 4, 5],
				"owner_id": 3
			}
		}
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"user": {"id": 1005, "email": "user1005@test.com", "product": {"id": 2003, "name": "Product 2003", "price": 2013.5}, "full_name": "User 1005"}}
}

func Example_insertIntoMultipleRelatedTables3() {
	gql := `mutation {
		product(insert: $data) {
			id
			name
			owner {
				id
				full_name
				email
			}
		}
	}`

	vars := json.RawMessage(`{
		"data": {
			"id": 2004,
			"name": "Product 2004",
			"description": "Description for product 2004",
			"price": 2014.5,
			"tags": ["Tag 1", "Tag 2"],
			"category_ids": [1, 2, 3, 4, 5],
			"owner": {
				"id": 1006,
				"email": "user1006@test.com",
				"full_name": "User 1006",
				"stripe_id": "payment_id_1006",
				"category_counts": [{"category_id": 1, "count": 400},{"category_id": 2, "count": 600}]
			}
		}
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"product": {"id": 2004, "name": "Product 2004", "owner": {"id": 1006, "email": "user1006@test.com", "full_name": "User 1006"}}}
}

func Example_insertIntoTableAndConnectToRelatedTables() {
	gql := `mutation {
		product(insert: $data) {
			id
			name
			owner {
				id
				full_name
				email
			}
		}
	}`

	vars := json.RawMessage(`{
		"data": {
			"id": 2005,
			"name": "Product 2005",
			"description": "Description for product 2005",
			"price": 2015.5,
			"tags": ["Tag 1", "Tag 2"],
			"category_ids": [1, 2, 3, 4, 5],
			"owner": {
				"connect": { "id": 6 }
			}
		}
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"product": {"id": 2005, "name": "Product 2005", "owner": {"id": 6, "email": "user6@test.com", "full_name": "User 6"}}}
}

func Example_insertIntoTableAndConnectToRelatedTableWithArrayColumn() {
	gql := `mutation {
		product(insert: $data) {
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

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	conf.Tables = []core.Table{
		{Name: "products", Columns: []core.Column{{Name: "category_ids", ForeignKey: "categories.id"}}},
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"product": {"id": 2006, "name": "Product 2006", "categories": [{"id": 1, "name": "Category 1"}, {"id": 2, "name": "Category 2"}, {"id": 3, "name": "Category 3"}, {"id": 4, "name": "Category 4"}, {"id": 5, "name": "Category 5"}]}}
}

func Example_insertIntoRecursiveRelationship() {
	gql := `mutation {
		comments(insert: $data, where: { id: { in: [5001, 5002] }}) {
			id
			reply_to_id
		}
	}`

	vars := json.RawMessage(`{
		"data": {
			"id": 5001,
			"body": "Comment body 5001",
			"created_at": "now",
			"comment": {
				"find": "children",
				"id": 5002,
				"body": "Comment body 5002",
				"created_at": "now"	
			}
		}
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"comments": [{"id": 5001, "reply_to_id": null}, {"id": 5002, "reply_to_id": 5001}]}
}

func Example_insertIntoRecursiveRelationshipAndConnectTable() {
	gql := `mutation {
		comments(insert: $data, where: { id: { in: [5, 5003] }}) {
			id
			reply_to_id
		}
	}`

	vars := json.RawMessage(`{
		"data": {
			"id": 5003,
			"body": "Comment body 5003",
			"created_at": "now",
			"comment": {
				"find": "children",
				"connect": { "id": 5 }
			}
		}
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"comments": [{"id": 5003, "reply_to_id": null}, {"id": 5, "reply_to_id": 5003}]}
}
