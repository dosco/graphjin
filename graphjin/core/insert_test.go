package core_test

/*
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
			"price": 2011.50,
			"tags": ["Tag 1", "Tag 2"],
			"category_ids": [1, 2, 3, 4, 5]
		}
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	conf.AddRoleTable("user", "products", core.Insert{
		Presets: map[string]string{"owner_id": "$user_id"},
	})

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
			"id": 1002
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
				"price": 2012.50,
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
	// Output: {"users": [{"id": 1002, "email": "user1002@test.com"}, {"id": 1003, "email": "user1003@test.com"}]})
}

func nestedInsertOneToMany(t *testing.T) {
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

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"email": "thedude@rug.com",
			"full_name": "The Dude",
			"created_at": "now",
			"updated_at": "now",
			"product": {
				"name": "Apple",
				"price": 1.25,
				"created_at": "now",
				"updated_at": "now"
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedInsertOneToOne(t *testing.T) {
	gql := `mutation {
		product(insert: $data) {
			id
			name
			user {
				id
				full_name
				email
			}
		}
	}`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"name": "Apple",
			"price": 1.25,
			"created_at": "now",
			"updated_at": "now",
			"user": {
				"email": "thedude@rug.com",
				"full_name": "The Dude",
				"created_at": "now",
				"updated_at": "now"
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedInsertOneToManyWithConnect(t *testing.T) {
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

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"email": "thedude@rug.com",
			"full_name": "The Dude",
			"created_at": "now",
			"updated_at": "now",
			"product": {
				"connect": { "id": 5 }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedInsertOneToOneWithConnect(t *testing.T) {
	gql := `mutation {
		product(insert: $data) {
			id
			name
			tags {
				id
				name
			}
			user {
				id
				full_name
				email
			}
		}
	}`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"name": "Apple",
			"price": 1.25,
			"created_at": "now",
			"updated_at": "now",
			"user": {
				"connect": { "id": 5 }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedInsertOneToOneWithConnectReverse(t *testing.T) {
	gql := `mutation {
		comment(insert: $data) {
			id
			product {
				id
				name
			}
		}
	}`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"body": "a comment",
			"created_at": "now",
			"updated_at": "now",
			"product": {
				"connect": { "id": 1 }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedInsertOneToOneWithConnectArray(t *testing.T) {
	gql := `mutation {
		product(insert: $data) {
			id
			name
			user {
				id
				full_name
				email
			}
		}
	}`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"name": "Apple",
			"price": 1.25,
			"created_at": "now",
			"updated_at": "now",
			"user": {
				"connect": { "id": [1,2] }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedInsertRecursive(t *testing.T) {
	gql := `mutation {
		comments(insert: $data) {
			id
			comments(find: "children") {
				id
				body
			}
		}
	}`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"id": 1002,
			"body": "hello 2",
			"created_at": "now",
			"updated_at": "now",
			"comment": {
				"find": "children",
				"connect":{ "id": 5 }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

*/
