package tests_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func Example_query() {
	gql := `
	query {
		products(limit: 3, order_by: { id: asc }) {
			id
			count_likes,
			owner {
				id
				fullName: full_name
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
		if dbType == "mariadb" {
			fmt.Printf("DEBUG SQL:\n%s\n", res.SQL())
		}
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"count_likes":null,"id":1,"owner":{"fullName":"User 1","id":1}},{"count_likes":null,"id":2,"owner":{"fullName":"User 2","id":2}},{"count_likes":null,"id":3,"owner":{"fullName":"User 3","id":3}}]}
}

func Example_queryInTransaction() {
	gql := `
	query {
		products(limit: 3, order_by: { id: asc }) {
			id
			owner {
				id
				fullName: full_name
			}
		}
	}`

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

	res, err := gj.GraphQLTx(c, tx, gql, nil, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := tx.Commit(); err != nil {
		panic(err)
	}
	printJSON(res.Data)

	// Output: {"products":[{"id":1,"owner":{"fullName":"User 1","id":1}},{"id":2,"owner":{"fullName":"User 2","id":2}},{"id":3,"owner":{"fullName":"User 3","id":3}}]}
}

func Example_queryJSONPathOperations() {
	// Test case for issue #519: JSON path filtering on nested objects
	gql := `
	query {
		quotations(where: { validity_period: { issue_date: { lte: "2024-09-18T03:03:16+0000" } } }) {
			id
			validity_period
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"quotations":[{"id":1,"validity_period":{"expiry_date":"2024-10-15T03:03:16+0000","issue_date":"2024-09-15T03:03:16+0000","status":"active"}},{"id":3,"validity_period":{"expiry_date":"2024-10-10T03:03:16+0000","issue_date":"2024-09-10T03:03:16+0000","status":"expired"}}]}
}

func Example_queryJSONPathOperationsAlternativeSyntax() {
	// Test case for issue #519: Alternative syntax using JSON path operator
	// Using underscore syntax which gets transformed to JSON path
	gql := `
	query {
		products(limit: 10, where: { metadata_foo: { eq: true } }) {
			id
			metadata
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"products":[{"id":2,"metadata":{"foo":true}},{"id":4,"metadata":{"foo":true}},{"id":6,"metadata":{"foo":true}},{"id":8,"metadata":{"foo":true}},{"id":10,"metadata":{"foo":true}},{"id":12,"metadata":{"foo":true}},{"id":14,"metadata":{"foo":true}},{"id":16,"metadata":{"foo":true}},{"id":18,"metadata":{"foo":true}},{"id":20,"metadata":{"foo":true}}]}
}

func Example_queryWithUser() {
	gql := `
	query {
		products(where: { owner_id: { eq: $user_id } }) {
			id
			owner {
				id
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 31)
	res, err := gj.GraphQL(ctx, gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":31,"owner":{"id":31}}]}
}

func Example_queryWithDynamicOrderBy() {
	gql := `
	query getProducts {
		products(
			order_by: $order,
			where: { id: { lt: 6 } },
			limit: 5,
			before: $cursor) {
			id
			price
		}
	}`

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		Tables: []core.Table{{
			Name: "products",
			OrderBy: map[string][]string{
				"price_and_id": {"price desc", "id asc"},
				"just_id":      {"id asc"},
			},
		}},
	})

	type result struct {
		Products json.RawMessage `json:"products"`
		Cursor   string          `json:"products_cursor"`
	}
	var val result

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	vars1 := json.RawMessage(`{ "order": "price_and_id" }`)

	res1, err1 := gj.GraphQL(context.Background(), gql, vars1, nil)
	if err1 != nil {
		fmt.Println(err1)
		return
	}

	if err := json.Unmarshal(res1.Data, &val); err != nil {
		fmt.Println(err)
		return
	}

	if val.Cursor == "" {
		fmt.Println("product_cursor value missing")
		return
	}
	printJSONString(string(val.Products))

	vars2 := json.RawMessage(`{ "order": "just_id" }`)

	res2, err2 := gj.GraphQL(context.Background(), gql, vars2, nil)
	if err2 != nil {
		fmt.Println(err2)
		return
	}

	if err := json.Unmarshal(res2.Data, &val); err != nil {
		fmt.Println(err)
		return
	}

	if val.Cursor == "" {
		fmt.Println("product_cursor value missing")
		return
	}

	printJSONString(string(val.Products))

	// Output:
	//[{"id":5,"price":15.5},{"id":4,"price":14.5},{"id":3,"price":13.5},{"id":2,"price":12.5},{"id":1,"price":11.5}]
	//[{"id":1,"price":11.5},{"id":2,"price":12.5},{"id":3,"price":13.5},{"id":4,"price":14.5},{"id":5,"price":15.5}]
}

func Example_queryWithNestedOrderBy() {
	gql := `
	query getProducts {
		products(order_by: { users: { email: desc }, id: desc}, where: { id: { lt: 6 } }, limit: 5) {
			id
			price
		}
	}`

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
	})

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
	// Output: {"products":[{"id":5,"price":15.5},{"id":4,"price":14.5},{"id":3,"price":13.5},{"id":2,"price":12.5},{"id":1,"price":11.5}]}
}

func Example_queryWithOrderByList() {
	gql := `
	query getProducts {
		products(order_by: { id: [$list, "asc"] }, where: { id: { in: $list } }, limit: 5) {
			id
			price
		}
	}`

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
	})

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	vars := json.RawMessage(`{ "list": [3, 2, 1, 5] }`)
	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":3,"price":13.5},{"id":2,"price":12.5},{"id":1,"price":11.5},{"id":5,"price":15.5}]}
}

func Example_queryWithLimitOffsetOrderByDistinctAndWhere() {
	gql := `query {
		products(
			# returns only 5 items
			limit: 5,

			# starts from item 10, commented out for now
			# offset: 10,

			# orders the response items by highest price
			order_by: { price: desc },

			# no duplicate prices returned
			distinct: [ price ]

			# only items with an id >= 50 and < 100 are returned
			where: { id: { and: { greater_or_equals: 50, lt: 100 } } }) {
			id
			name
			price
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"products":[{"id":99,"name":"Product 99","price":109.5},{"id":98,"name":"Product 98","price":108.5},{"id":97,"name":"Product 97","price":107.5},{"id":96,"name":"Product 96","price":106.5},{"id":95,"name":"Product 95","price":105.5}]}
}

func Example_queryWithWhere1() {
	gql := `query {
		products(where: {
			id: [3, 34],
			or: {
				name: { iregex: $name },
				description: { iregex: $name }
			}
		}) {
			id
		}
	}`

	vars := json.RawMessage(`{
		"name": "Product 3"
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":3},{"id":34}]}
}

func Example_queryWithWhereIn() {
	gql := `query {
		products(where: { id: { in: $list } }) {
			id
		}
	}`

	vars := json.RawMessage(`{
		"list": [1,2,3]
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":1},{"id":2},{"id":3}]}
}

func Example_queryWithWhereNotIsNullAndGreaterThan() {
	gql := `query {
		products(
			where: {
				and: [
					{ not: { id: { is_null: true } } },
					{ price: { gt: 10 } },
				]
			}
			limit: 3
			order_by: { id: asc }) {
			id
			name
			price
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"products":[{"id":1,"name":"Product 1","price":11.5},{"id":2,"name":"Product 2","price":12.5},{"id":3,"name":"Product 3","price":13.5}]}
}

func Example_queryWithWhereGreaterThanOrLesserThan() {
	gql := `query {
		products(
			limit: 3
			order_by: { id: asc }
			where: {
				or: {
					price: { gt: 20 },
					price: { lt: 22 }
				} }
			) {
			id
			name
			price
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"products":[{"id":1,"name":"Product 1","price":11.5},{"id":2,"name":"Product 2","price":12.5},{"id":3,"name":"Product 3","price":13.5}]}
}

func Example_queryWithWhereOnRelatedTable() {
	gql := `query {
		products(where: { owner: { id: { or: [ { eq: $user_id }, { eq: 3 } ] } } }, limit: 2) {
			id
			owner {
				id
				email
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 2)
	res, err := gj.GraphQL(ctx, gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":2,"owner":{"email":"user2@test.com","id":2}},{"id":3,"owner":{"email":"user3@test.com","id":3}}]}
}

func Example_queryWithAlternateFieldNames() {
	gql := `query {
		comments(limit: 2, order_by: { id: asc }) {
			id
			commenter {
				email
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"comments":[{"commenter":{"email":"user1@test.com"},"id":1},{"commenter":{"email":"user2@test.com"},"id":2}]}
}

func Example_queryByID() {
	gql := `query {
		products(id: $id) {
			id
			name
		}
	}`

	vars := json.RawMessage(`{
		"id": 2
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":{"id":2,"name":"Product 2"}}
}

func Example_queryBySearch() {
	gql := `query {
		products(search: $query, limit: 5) {
			id
			name
		}
	}`

	vars := json.RawMessage(`{
		"query": "Product 3"
	}`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":3,"name":"Product 3"}]}
}

func Example_queryParentsWithChildren() {
	gql := `query {
		users(order_by: { id: asc }, limit: 2) {
			email
			products {
				name
				price
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"users":[{"email":"user1@test.com","products":[{"name":"Product 1","price":11.5}]},{"email":"user2@test.com","products":[{"name":"Product 2","price":12.5}]}]}
}

func Example_queryChildrenWithParent() {
	gql := `query {
		products(limit: 2, order_by: { id: asc }) {
			name
			price
			owner {
				email
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"products":[{"name":"Product 1","owner":{"email":"user1@test.com"},"price":11.5},{"name":"Product 2","owner":{"email":"user2@test.com"},"price":12.5}]}
}

func Example_queryManyToManyViaJoinTable1() {
	gql := `query {
		products(limit: 2, order_by: { id: asc }) {
			name
			customer {
				email
			}
			owner {
				email
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"products":[{"customer":[{"email":"user2@test.com"}],"name":"Product 1","owner":{"email":"user1@test.com"}},{"customer":[{"email":"user3@test.com"}],"name":"Product 2","owner":{"email":"user2@test.com"}}]}
}

func Example_queryManyToManyViaJoinTable2() {
	gql := `query {
		users(order_by: { id: asc }) {
			email
			full_name
			products {
				name
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2})
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
	// Output: {"users":[{"email":"user1@test.com","full_name":"User 1","products":[{"name":"Product 1"}]},{"email":"user2@test.com","full_name":"User 2","products":[{"name":"Product 2"}]}]}
}

func Example_queryManyToManyViaJoinTable3() {
	gql := `
	query {
		graph_node {
			id
			dst_node  {
				id
			}
			src_node {
				id
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2})
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
	// Output: {"graph_node":[{"dst_node":[{"id":"b"},{"id":"c"}],"id":"a","src_node":[]},{"dst_node":[],"id":"b","src_node":[{"id":"a"}]}]}
}

func Example_queryWithAggregation() {
	gql := `query {
		products(where: { id: { lteq: 100 } }) {
			count_id
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"products":[{"count_id":100}]}
}

func Example_queryWithAggregationBlockedColumn() {
	gql := `query {
		products {
			sum_price
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	err := conf.AddRoleTable("anon", "products", core.Query{
		Columns: []string{"id", "name"},
	})
	if err != nil {
		panic(err)
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
	// Output: db column blocked: price (role: 'anon')
}

func Example_queryWithFunctionsBlocked() {
	gql := `query {
		products {
			sum_price
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	err := conf.AddRoleTable("anon", "products", core.Query{
		DisableFunctions: true,
	})
	if err != nil {
		panic(err)
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
	// Output: all db functions blocked: sum (role: 'anon')
}

func Example_queryWithFunctionsWithWhere() {
	gql := `query {
		products(where: { id: { lesser_or_equals: 100 } }) {
			max_price
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"products":[{"max_price":110.5}]}
}

func Example_queryWithSyntheticTables() {
	gql := `query {
		me @object {
			email
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	conf.Tables = []core.Table{{Name: "me", Table: "users"}}
	err := conf.AddRoleTable("user", "me", core.Query{
		Filters: []string{`{ id: $user_id }`},
		Limit:   1,
	})
	if err != nil {
		panic(err)
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 1)
	res, err := gj.GraphQL(ctx, gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"me":{"email":"user1@test.com"}}
}

func Example_queryWithVariables() {
	gql := `query {
		products(id: $product_id, where: { price: { gt: $product_price } }) {
			id
			name
		}
	}`

	vars := json.RawMessage(`{ "product_id": 70 }`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	conf.Vars = map[string]string{"product_price": "50"}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":{"id":70,"name":"Product 70"}}
}

func Example_queryWithVariablesDefaultValue() {
	gql := `query ($product_id = 70) {
		products(id: $product_id, where: { price: { gt: $product_price } }) {
			id
			name
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	conf.Vars = map[string]string{"product_price": "50"}

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
	// Output: {"products":{"id":70,"name":"Product 70"}}
}

func Example_queryWithMultipleTopLevelTables() {
	gql := `query {
		products(id: $id) {
			id
			name
			customer {
				email
			}
		}
		users(id: $id) {
			id
			email
		}
		purchases(id: $id) {
			id
		}
	}`

	vars := json.RawMessage(`{ "id": 1 }`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":{"customer":[{"email":"user2@test.com"}],"id":1,"name":"Product 1"},"purchases":{"id":1},"users":{"email":"user1@test.com","id":1}}
}

func Example_queryWithFragments1() {
	gql := `
	fragment userFields1 on user {
		id
		email
	}

	query {
		users(order_by: { id: asc }) {
			...userFields2
			stripe_id
			...userFields1
		}
	}

	fragment userFields2 on user {
		full_name
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2})
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
	// Output: {"users":[{"email":"user1@test.com","full_name":"User 1","id":1,"stripe_id":"payment_id_1001"},{"email":"user2@test.com","full_name":"User 2","id":2,"stripe_id":"payment_id_1002"}]}
}

func Example_queryWithFragments2() {
	gql := `
	query {
		users(order_by: { id: asc }) {
			...userFields2

			stripe_id
			...userFields1
		}
	}

	fragment userFields1 on user {
		id
		email
	}

	fragment userFields2 on user {
		full_name
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2})
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
	// Output: {"users":[{"email":"user1@test.com","full_name":"User 1","id":1,"stripe_id":"payment_id_1001"},{"email":"user2@test.com","full_name":"User 2","id":2,"stripe_id":"payment_id_1002"}]}
}

func Example_queryWithFragments3() {
	gql := `
	fragment userFields1 on user {
		id
		email
	}

	fragment userFields2 on user {
		full_name

		...userFields1
	}

	query {
		users(order_by: { id: asc }) {
			...userFields2
			stripe_id
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2})
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
	// Output: {"users":[{"email":"user1@test.com","full_name":"User 1","id":1,"stripe_id":"payment_id_1001"},{"email":"user2@test.com","full_name":"User 2","id":2,"stripe_id":"payment_id_1002"}]}
}

func Example_queryWithUnionForPolymorphicRelationships() {
	gql := `
	fragment userFields on user {
		email
	}

	fragment productFields on product {
		name
	}

	query {
		notifications {
			id
			verb
			subject {
				...on users {
					...userFields
				}
				...on products {
					...productFields
				}
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2})
	conf.Tables = []core.Table{{
		Name:    "subject",
		Type:    "polymorphic",
		Columns: []core.Column{{Name: "subject_id", ForeignKey: "subject_type.id"}},
	}}

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
	// Output: {"notifications":[{"id":1,"subject":{"email":"user1@test.com"},"verb":"Joined"},{"id":2,"subject":{"name":"Product 2"},"verb":"Bought"}]}
}

func Example_queryWithSkipAndIncludeIfArg() {
	gql := `
	query {
		products(limit: 2, order_by: { id: asc })  {
			id(includeIf: { id: { eq: 1 } })
			name
		}
		users(limit: 3, order_by: { id: asc })  {
			id(skipIf: { id: { eq: 2 } })
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"products":[{"id":1,"name":"Product 1"},{"id":null,"name":"Product 2"}],"users":[{"id":1},{"id":null},{"id":3}]}
}

func Example_queryWithSkipAndIncludeDirective1() {
	gql := `
	query {
		products(limit: 2, order_by: { id: asc }) @include(ifRole: "user") {
			id
			name
		}
		users(limit: 3, order_by: { id: asc }) @skip(ifRole: "user") {
			id
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"products":null,"users":[{"id":1},{"id":2},{"id":3}]}
}

func Example_queryWithSkipAndIncludeDirective2() {
	gql := `
	query {
		products(limit: 2, order_by: { id: asc })  {
			id @skip(ifRole: "user")
			name @include(ifRole: "user")
		}
		users(limit: 3, order_by: { id: asc })  {
			id @include(ifRole: "anon")
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	c := context.WithValue(context.Background(), core.UserIDKey, 1)
	res, err := gj.GraphQL(c, gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":null,"name":"Product 1"},{"id":null,"name":"Product 2"}],"users":[{"id":null},{"id":null},{"id":null}]}
}

func Example_queryWithSkipAndIncludeDirective3() {
	gql := `
	query {
		products(limit: 2, order_by: { id: asc }) @include(ifVar: $test) {
			id
			name
		}
		users(limit: 3, order_by: { id: asc }) @skip(ifVar: $test) {
			id
		}
	}`

	vars := json.RawMessage(`{ "test": true }`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":1,"name":"Product 1"},{"id":2,"name":"Product 2"}],"users":null}
}

func Example_queryWithSkipAndIncludeDirective4() {
	gql := `
	query {
		products(limit: 2, order_by: { id: asc })  {
			id @skip(ifVar: $test)
			name @include(ifVar: $test)
		}
		users(limit: 3, order_by: { id: asc })  {
			id @skip(ifVar: $test)
		}
	}`

	vars := json.RawMessage(`{ "test": true }`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products":[{"id":null,"name":"Product 1"},{"id":null,"name":"Product 2"}],"users":[{"id":null},{"id":null},{"id":null}]}
}

func Example_queryWithAddAndRemoveDirective1() {
	gql := `
	query {
		products(limit: 2, order_by: { id: asc }) @add(ifRole: "user") {
			id
			name
		}
		users(limit: 3, order_by: { id: asc }) @remove(ifRole: "user") {
			id
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"users":[{"id":1},{"id":2},{"id":3}]}
}

func Example_queryWithAddAndRemoveDirective2() {
	gql := `
	query {
		products(limit: 2, order_by: { id: asc })  {
			id
			name @add(ifRole: "user")
		}
		users(limit: 3, order_by: { id: asc })  {
			id @remove(ifRole: "anon")
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"products":[{"id":1},{"id":2}],"users":[{},{},{}]}
}

func Example_queryWithRemoteAPIJoin() {
	gql := `query {
		users(order_by: { id: asc }) {
			email
			payments {
				desc
			}
		}
	}`

	// fake remote api service
	mux := http.NewServeMux()
	mux.HandleFunc("/payments/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[10:]
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[{"desc":"Payment 1 for %s"},{"desc": "Payment 2 for %s"}]}`,
			id, id)
	})

	// Use a listener to ensure we get an available port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}

	// Get the actual port that was assigned
	port := listener.Addr().(*net.TCPAddr).Port

	server := &http.Server{
		Handler: mux,
	}

	go func() {
		log.Fatal(server.Serve(listener)) //nolint:gosec
	}()

	// Wait for server to be ready by polling the actual endpoint
	for i := 0; i < 100; i++ {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/payments/test", port))
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2})
	conf.Resolvers = []core.ResolverConfig{{
		Name:      "payments",
		Type:      "remote_api",
		Table:     "users",
		Column:    "stripe_id",
		StripPath: "data",
		Props:     core.ResolverProps{"url": fmt.Sprintf("http://localhost:%d/payments/$id", port)},
	}}

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
	// Output: {"users":[{"email":"user1@test.com","payments":[{"desc":"Payment 1 for payment_id_1001"},{"desc":"Payment 2 for payment_id_1001"}]},{"email":"user2@test.com","payments":[{"desc":"Payment 1 for payment_id_1002"},{"desc":"Payment 2 for payment_id_1002"}]}]}
}

func Example_queryWithCursorPagination1() {
	gql := `query {
		products(
			where: { id: { lesser_or_equals: 100 } }
			first: 3
			after: $cursor
			order_by: { price: desc }) {
			name
		}
		products_cursor
	}`

	vars := json.RawMessage(`{"cursor": null}`)

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		SecretKey:        "not_a_real_secret",
	})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	type result struct {
		Products json.RawMessage `json:"products"`
		Cursor   string          `json:"products_cursor"`
	}

	var val result
	if err := json.Unmarshal(res.Data, &val); err != nil {
		fmt.Println(err)
		return
	}

	if val.Cursor == "" {
		fmt.Println("product_cursor value missing")
		return
	}
	dst := &bytes.Buffer{}
	if err := json.Compact(dst, val.Products); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(dst.String())
	// Output: [{"name":"Product 100"},{"name":"Product 99"},{"name":"Product 98"}]
}

func Example_queryWithCursorPagination2() {
	gql := `query {
		products(
			first: 1
			after: $cursor
			where: { id: { lteq: 100 }}
			order_by: { price: desc }) {
			name
		}
		products_cursor
	}`

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		SecretKey:        "not_a_real_secret",
	})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	type result struct {
		Products json.RawMessage `json:"products"`
		Cursor   string          `json:"products_cursor"`
	}

	var val result
	for i := 0; i < 25; i++ {
		vars := json.RawMessage(
			`{"cursor": "` + val.Cursor + `"}`)

		res, err := gj.GraphQL(context.Background(), gql, vars, nil)
		if err != nil {
			fmt.Println(err)
			return
		}


		if err := json.Unmarshal(res.Data, &val); err != nil {
			fmt.Println(err)
			return
		}

		dst := &bytes.Buffer{}
		if err := json.Compact(dst, val.Products); err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(dst.String())
	}
	// Output:
	// [{"name":"Product 100"}]
	// [{"name":"Product 99"}]
	// [{"name":"Product 98"}]
	// [{"name":"Product 97"}]
	// [{"name":"Product 96"}]
	// [{"name":"Product 95"}]
	// [{"name":"Product 94"}]
	// [{"name":"Product 93"}]
	// [{"name":"Product 92"}]
	// [{"name":"Product 91"}]
	// [{"name":"Product 90"}]
	// [{"name":"Product 89"}]
	// [{"name":"Product 88"}]
	// [{"name":"Product 87"}]
	// [{"name":"Product 86"}]
	// [{"name":"Product 85"}]
	// [{"name":"Product 84"}]
	// [{"name":"Product 83"}]
	// [{"name":"Product 82"}]
	// [{"name":"Product 81"}]
	// [{"name":"Product 80"}]
	// [{"name":"Product 79"}]
	// [{"name":"Product 78"}]
	// [{"name":"Product 77"}]
	// [{"name":"Product 76"}]
}

func TestQueryWithJsonColumn(t *testing.T) {
	if dbType == "mssql" {
		t.Skip("skipping for mssql - JSON column type detection needs work (nvarchar not recognized as JSON)")
	}

	gql := `query {
		users(id: 1) {
			id
			category_counts {
				count
				category {
					name
				}
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
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	exp := `{"users":{"category_counts":[{"category":{"name":"Category 1"},"count":400},{"category":{"name":"Category 2"},"count":600}],"id":1}}`
	if stdJSON(res.Data) != exp {
		t.Errorf("expected '%s' got '%s'", exp, stdJSON(res.Data))
	}
}

func Example_queryWithView() {
	gql := `query {
		hot_products(limit: 3) {
			product {
				id
				name
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	conf.Tables = []core.Table{
		{
			Name: "hot_products",
			Columns: []core.Column{
				{Name: "product_id", Type: "int", ForeignKey: "products.id"},
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
	// Output: {"hot_products":[{"product":{"id":51,"name":"Product 51"}},{"product":{"id":52,"name":"Product 52"}},{"product":{"id":53,"name":"Product 53"}}]}
}

func Example_queryWithRecursiveRelationship1() {
	gql := `query {
		reply : comments(id: $id) {
			id
			comments(
				where: { id: { lt: 50 } },
				limit: 5,
				find: "parents") {
				id
			}
		}
	}`

	vars := json.RawMessage(`{"id": 50 }`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"reply":{"comments":[{"id":49},{"id":48},{"id":47},{"id":46},{"id":45}],"id":50}}
}

func Example_queryWithRecursiveRelationship2() {
	gql := `query {
		comments(id: 95) {
			id
			replies: comments(find: "children") {
				id
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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

	// Output: {"comments":{"id":95,"replies":[{"id":96},{"id":97},{"id":98},{"id":99},{"id":100}]}}
}

func Example_queryWithRecursiveRelationshipAndAggregations() {
	gql := `query {
		comments(id: 95) {
			id
			replies: comments(find: "children") {
				count_id
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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

	// Output: {"comments":{"id":95,"replies":[{"count_id":5}]}}
}

func Example_queryWithSkippingAuthRequiredSelectors() {
	gql := `query {
		products(limit: 2, order_by: { id: asc }) {
			id
			name
			owner(where: { id: { eq: $user_id } }) {
				id
				email
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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
	// Output: {"products":[{"id":1,"name":"Product 1","owner":null},{"id":2,"name":"Product 2","owner":null}]}
}

func Example_queryBlockWithRoles() {
	gql := `query {
		users {
			id
			full_name
			email
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	conf.RolesQuery = `SELECT * FROM users WHERE id = $user_id`
	conf.Roles = []core.Role{{Name: "disabled_user", Match: "disabled = true"}}

	err := conf.AddRoleTable("disabled_user", "users", core.Query{Block: true})
	if err != nil {
		panic(err)
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 50)
	res, err := gj.GraphQL(ctx, gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"users":null}
}

func Example_queryWithCamelToSnakeCase() {
	gql := `query {
		hotProducts(where: { productID: { eq: 55 } }, order_by: { productID: desc }) {
			countryCode
			countProductID
			products {
				id
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, EnableCamelcase: true})
	conf.Tables = []core.Table{
		{
			Name: "hot_products",
			Columns: []core.Column{
				{Name: "product_id", Type: "int", ForeignKey: "products.id"},
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
	// Output: {"hotProducts":[{"countProductID":1,"countryCode":"US","products":{"id":55}}]}
}

func Example_queryWithWhereHasAnyKey() {
	gql := `query {
		products(
			where: { metadata: { has_key_any: ["foo", "bar"] } }
			limit: 3
		) {
			id
	}
}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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

	// Output: {"products":[{"id":1},{"id":2},{"id":3}]}
}

func Example_queryWithTypename() {
	gql := `query getUser {
		__typename
		users(id: 1) {
		  id
		  email
		  __typename
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, json.RawMessage(`{"id":2}`), nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"__typename":"getUser","users":{"__typename":"users","email":"user1@test.com","id":1}}
}

func Example_queryFunctionWithJsonbParam() {
	gql := `query {
		process_user_data(user_id: 1, user_data: $userData) {
			id
			result_data
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	vars := json.RawMessage(`{"userData": {"field": "Alex", "value": 123}}`)
	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// This test reproduces the JSONB function parameter issue from GitHub issue #521
}
