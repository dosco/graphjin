//go:build !mysql

package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
)

func Example_insert() {
	gql := `mutation {
		users(insert: {
			id: $id,
			email: $email,
			full_name: $fullName,
			stripe_id: $stripeID,
			category_counts: $categoryCounts
		}) {
			id
			email
		}
	}`

	vars := json.RawMessage(`{
		"id": 1001,
		"email": "user1001@test.com",
		"fullName": "User 1001",
		"stripeID": "payment_id_1001",
		"categoryCounts": [{"category_id": 1, "count": 400},{"category_id": 2, "count": 600}]
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
	// Output: {"users":[{"email":"user1001@test.com","id":1001}]}
}

func Example_insertWithTransaction() {
	gql := `mutation {
		users(insert: {
			id: $id,
			email: $email,
			full_name: $fullName,
			stripe_id: $stripeID,
			category_counts: $categoryCounts
		}) {
			id
			email
		}
	}`

	vars := json.RawMessage(`{
		"id": 1007,
		"email": "user1007@test.com",
		"fullName": "User 1007",
		"stripeID": "payment_id_1007",
		"categoryCounts": [{"category_id": 1, "count": 400},{"category_id": 2, "count": 600}]
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	c := context.Background()
	tx, err := db.BeginTx(c, nil)
	if err != nil {
		panic(err)
	}
	defer tx.Rollback() //nolint:errcheck

	c = context.WithValue(c, core.UserIDKey, 3)
	res, err := gj.GraphQLTx(c, tx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := tx.Commit(); err != nil {
		panic(err)
	}
	printJSON(res.Data)
	// Output: {"users":[{"email":"user1007@test.com","id":1007}]}
}

func Example_insertInlineWithValidation() {
	gql := `mutation 
		@constraint(variable: "email", format: "email", min: 1, max: 100)
		@constraint(variable: "full_name", requiredIf: { id: 1007 } ) 
		@constraint(variable: "id", greaterThan:1006  ) 
		@constraint(variable: "id", lessThanOrEqualsField:id  ) {
		users(insert: { id: $id, email: $email, full_name: $full_name }) {
			id
			email
			full_name
		}
	}`

	vars := json.RawMessage(`{
		"id": 1007,
		"email": "not_an_email"
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
		for _, e := range res.Validation {
			fmt.Println(e.Constraint, e.FieldName)
		}
	} else {
		printJSON(res.Data)
	}
	// Ordered output:
	// validation failed
	// format email
	// min email
	// max email
	// requiredIf full_name
}

func Example_insertInlineBulk() {
	gql := `mutation {
		users(insert: [
			{id: $id1, email: $email1, full_name: $full_name1},
			{id:, $id2, email: $email2, full_name: $full_name2}]) {
			id
			email
		}
	}`

	vars := json.RawMessage(`{
		"id1": 1008,
		"email1": "one@test.com",
		"full_name1": "John One",
		"id2": 1009,
		"email2":  "two@test.com",
		"full_name2": "John Two"
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
	// Output: {"users":[{"email":"two@test.com","id":1009},{"email":"one@test.com","id":1008}]}
}

func Example_insertWithPresets() {
	gql := `mutation {
		products(insert: $data) {
			id
			name
			owner {
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

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":2001,"name":"Product 2001","owner":{"email":"user3@test.com","id":3}}]}
}

func Example_insertBulk() {
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
	// Output: {"users":[{"email":"user1002@test.com","id":1002},{"email":"user1003@test.com","id":1003}]}
}

func Example_insertIntoMultipleRelatedTables() {
	gql := `mutation {
		purchases(insert: $data) {
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
	// Output: {"purchases":[{"customer":{"email":"user1004@test.com","full_name":"User 1004","id":1004},"product":{"id":2002,"name":"Product 2002","price":2012.5},"quantity":5}]}
}

func Example_insertIntoTableAndRelatedTable1() {
	gql := `mutation {
		users(insert: $data) {
			id
			full_name
			email
			products {
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
			"products": {
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
	// Output: {"users":[{"email":"user1005@test.com","full_name":"User 1005","id":1005,"products":[{"id":2003,"name":"Product 2003","price":2013.5}]}]}
}

func Example_insertIntoTableAndRelatedTable2() {
	gql := `mutation {
		products(insert: $data) {
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
	// Output: {"products":[{"id":2004,"name":"Product 2004","owner":{"email":"user1006@test.com","full_name":"User 1006","id":1006}}]}
}

func Example_insertIntoTableBulkInsertIntoRelatedTable() {
	gql := `mutation {
		users(insert: $data) {
			id
			full_name
			email
			products {
				id
				name
				price
			}
		}
	}`

	vars := json.RawMessage(`{
		"data": {
			"id": 10051,
			"email": "user10051@test.com",
			"full_name": "User 10051",
			"stripe_id": "payment_id_10051",
			"category_counts": [
				{"category_id": 1, "count": 400},
				{"category_id": 2, "count": 600}
			],
			"products": [
				{
					"id": 20031,
					"name": "Product 20031",
					"description": "Description for product 20031",
					"price": 2013.5,
					"tags": ["Tag 1", "Tag 2"],
					"category_ids": [1, 2, 3, 4, 5],
					"owner_id": 3
				},
				{
					"id": 20032,
					"name": "Product 20032",
					"description": "Description for product 20032",
					"price": 2014.5,
					"tags": ["Tag 1", "Tag 2"],
					"category_ids": [1, 2, 3, 4, 5],
					"owner_id": 3
				}
			]
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

	// Output: {"users":[{"email":"user10051@test.com","full_name":"User 10051","id":10051,"products":[{"id":20031,"name":"Product 20031","price":2013.5},{"id":20032,"name":"Product 20032","price":2014.5}]}]}
}

func Example_insertIntoTableAndConnectToRelatedTables() {
	gql := `mutation {
		products(insert: $data) {
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
	// Output: {"products":[{"id":2005,"name":"Product 2005","owner":{"email":"user6@test.com","full_name":"User 6","id":6}}]}
}

func Example_insertWithCamelToSnakeCase() {
	gql := `mutation {
		products(insert: $data) {
			id
			name
			owner {
				id
				email
			}
		}
	}`

	vars := json.RawMessage(`{
		"data": {
			"id": 2007,
			"name": "Product 2007",
			"description": "Description for product 2007",
			"price": 2011.5,
			"tags": ["Tag 1", "Tag 2"],
			"categoryIds": [1, 2, 3, 4, 5]
		}
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, EnableCamelcase: true})
	err := conf.AddRoleTable("user", "products", core.Insert{
		Presets: map[string]string{"ownerId": "$user_id"},
	})
	if err != nil {
		panic(err)
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
	// Output: {"products":[{"id":2007,"name":"Product 2007","owner":{"email":"user3@test.com","id":3}}]}
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
			"comments": {
				"find": "children",
				"id": 5002,
				"body": "Comment body 5002",
				"created_at": "now"	
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
	// Output: {"comments":[{"id":5001,"reply_to_id":null},{"id":5002,"reply_to_id":5001}]}
}

func Example_insertIntoRecursiveRelationshipAndConnectTable1() {
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
			"comments": {
				"find": "children",
				"connect": { "id": 5 }
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
	// Output: {"comments":[{"id":5003,"reply_to_id":null},{"id":5,"reply_to_id":5003}]}
}

func Example_insertIntoRecursiveRelationshipAndConnectTable2() {
	gql := `mutation {
  	comments(insert: $data) @object {
			id
			product {
				id
			}
			commenter {
				id
			}
			comments(find: "children") {
				id
			}
  	}
  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	vars := json.RawMessage(`{
			"data": {
				"id":  5004,
				"body": "Comment body 5004",
				"created_at": "now",
				"comments": {
					"connect": { "id": 6 },
					"find": "children"
				},
				"product": {
					"connect": { "id": 26 }
				},
				"commenter":{
					"connect":{ "id": 3 }
				}
			}
		}`)

	ctx := context.WithValue(context.Background(), core.UserIDKey, 50)
	res, err := gj.GraphQL(ctx, gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"comments":{"commenter":{"id":3},"comments":[{"id":6}],"id":5004,"product":{"id":26}}}
}

func TestAllowListWithMutations(t *testing.T) {
	gql := `
	mutation getProducts {
		users(insert: $data) {
			id
		}
	}`

	dir, err := os.MkdirTemp("", "test")
	assert.NoError(t, err)
	defer os.RemoveAll(dir)

	fs := core.NewOsFS(dir)
	assert.NoError(t, err)

	conf1 := newConfig(&core.Config{DBType: dbType, DisableAllowList: false})
	gj1, err := core.NewGraphJin(conf1, db, core.OptionSetFS(fs))
	assert.NoError(t, err)

	vars1 := json.RawMessage(`{
		"data": {
			"id": 90011,
			"email": "user90011@test.com",
			"full_name": "User 90011"
		}
	}`)

	exp1 := `{"users": [{"id": 90011}]}`

	res1, err := gj1.GraphQL(context.Background(), gql, vars1, nil)
	assert.NoError(t, err)
	assert.Equal(t, exp1, string(res1.Data))

	conf2 := newConfig(&core.Config{DBType: dbType, Production: true})
	gj2, err := core.NewGraphJin(conf2, db, core.OptionSetFS(fs))
	assert.NoError(t, err)

	vars2 := json.RawMessage(`{
		"data": {
			"id": 90012,
			"email": "user90012@test.com",
			"full_name": "User 90012"
		}
	}`)

	exp2 := `{"users": [{"id": 90012}]}`

	res2, err := gj2.GraphQL(context.Background(), gql, vars2, nil)
	assert.NoError(t, err)
	assert.Equal(t, exp2, string(res2.Data))

	vars3 := json.RawMessage(`{
		"data": {
			"id": 90013,
			"email": "user90013@test.com",
			"full_name": "User 90013",
			"stripe_id": "payment_id_90013"
		}
	}`)

	exp3 := `{"users": [{"id": 90013}]}`

	res3, err := gj2.GraphQL(context.Background(), gql, vars3, nil)
	assert.NoError(t, err)
	assert.Equal(t, exp3, string(res3.Data))
}
