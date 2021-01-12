package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/dosco/graphjin/core"
)

func Example_query() {
	gql := `query {
		product {
			id
			user {
				id
			}
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"product": {"id": 1, "user": {"id": 1}}}
}

func Example_queryWithUser() {
	gql := `query {
		product(where: { owner_id: { eq: $user_id } }) {
			id
			user {
				id
			}
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
	res, err := gj.GraphQL(ctx, gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"product": {"id": 3, "user": {"id": 3}}}
}

func Example_queryWithVariableLimit() {
	gql := `query {
		products(limit: $limit) {
			id
		}
	}`

	vars := json.RawMessage(`{
		"limit": 10
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}, {"id": 5}, {"id": 6}, {"id": 7}, {"id": 8}, {"id": 9}, {"id": 10}]}
}

func Example_queryWithLimitOffsetOrderByDistinctAndWhere() {
	gql := `query {
		proDUcts(
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
			NAME
			price
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 99, "name": "Product 99", "price": 109.50}, {"id": 98, "name": "Product 98", "price": 108.50}, {"id": 97, "name": "Product 97", "price": 107.50}, {"id": 96, "name": "Product 96", "price": 106.50}, {"id": 95, "name": "Product 95", "price": 105.50}]}
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

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 1}, {"id": 2}, {"id": 3}]}
}

func Example_queryWithWhereNotIsNullAndGreaterThan() {
	gql := `query {
		product(
			where: {
				and: [
					{ not: { id: { is_null: true } } },
					{ price: { gt: 10 } },
				] } ) {
			id
			name
			price
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"product": {"id": 1, "name": "Product 1", "price": 11.50}}
}

func Example_queryWithWhereGreaterThanOrLesserThan() {
	gql := `query {
		products(
			limit: 3
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

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 1, "name": "Product 1", "price": 11.50}, {"id": 2, "name": "Product 2", "price": 12.50}, {"id": 3, "name": "Product 3", "price": 13.50}]}
}

func Example_queryWithWhereOnRelatedTable() {
	gql := `query {
			product(where: { comment: { user: { email: { eq: $email } } } }) {
			 id
		 }
	}`

	vars := json.RawMessage(`{
		"email": "user10@test.com"
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"product": {"id": 10}}
}

func Example_queryWithAlternateFieldNames() {
	gql := `query {
			comments(limit: 2) {
			 id
			 commenter {
				 email
			 }
		 }
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"comments": [{"id": 1, "commenter": {"email": "user1@test.com"}}, {"id": 2, "commenter": {"email": "user2@test.com"}}]}
}

func Example_queryByID() {
	gql := `query {
		product(id: $id) {
			id
			name
		}
	}`

	vars := json.RawMessage(`{
		"id": 2
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"product": {"id": 2, "name": "Product 2"}}
}

func Example_queryBySearch() {
	gql := `query {
		products(search: $query) {
			id
			name
			search_rank
			search_headline_description
		}
	}`

	vars := json.RawMessage(`{
		"query": "Product 3"
	}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 3, "name": "Product 3", "search_rank": 0.33442792, "search_headline_description": "Description for <b>product</b> <b>3</b>"}]}
}

func Example_queryParentsWithChildren() {
	gql := `query {
		users(limit: 2) {
			email
			products {
				name
				price
			}
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"email": "user1@test.com", "products": [{"name": "Product 1", "price": 11.50}]}, {"email": "user2@test.com", "products": [{"name": "Product 2", "price": 12.50}]}]}
}

func Example_queryChildrenWithParent() {
	gql := `query {
		products(limit: 2) {
			name
			price
			users {
				email
			}
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"name": "Product 1", "price": 11.50, "users": [{"email": "user1@test.com"}]}, {"name": "Product 2", "price": 12.50, "users": [{"email": "user2@test.com"}]}]}
}

func Example_queryParentAndChildrenViaArrayColumn() {
	gql := `
	query {
		products {
			name
			price
			categories {
				id
				name
			}
		}
		categories {
			name
			product {
				name
			}
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2}
	conf.Tables = []core.Table{
		{Name: "products", Columns: []core.Column{{Name: "category_ids", ForeignKey: "categories.id"}}},
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"name": "Product 1", "price": 11.50, "categories": [{"id": 1, "name": "Category 1"}, {"id": 2, "name": "Category 2"}]}, {"name": "Product 2", "price": 12.50, "categories": [{"id": 1, "name": "Category 1"}, {"id": 2, "name": "Category 2"}]}], "categories": [{"name": "Category 1", "product": {"name": "Product 1"}}, {"name": "Category 2", "product": {"name": "Product 1"}}]}
}

func Example_queryManyToManyViaJoinTable1() {
	gql := `query {
		product {
			name
			customers {
				email
			}
			owner {
				email
			}
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"product": {"name": "Product 1", "owner": {"email": "user1@test.com"}, "customers": [{"email": "user2@test.com"}]}}
}

func Example_queryManyToManyViaJoinTable2() {
	gql := `query {
		users {
			email
			full_name
			products {
				name
			}
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"email": "user1@test.com", "products": [{"name": "Product 1"}], "full_name": "User 1"}, {"email": "user2@test.com", "products": [{"name": "Product 2"}], "full_name": "User 2"}]}
}

func Example_queryWithAggregation() {
	gql := `query(where: { id: { lteq: 100 } }) {
		products {
			count_id
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"count_id": 100}]}
}

func Example_queryWithAggregationBlockedColumn() {
	gql := `query {
		products {
			sum_price
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
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

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: column blocked: sum (anon)
}

func Example_queryWithFunctionsBlocked() {
	gql := `query {
		products {
			sum_price
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
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

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: functions blocked: price (anon)
}

func Example_queryWithFunctionsWithWhere() {
	gql := `query {
		products(where: { id: { lesser_or_equals: 100 } }) {
			sum_price
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"sum_price": 6100.00}]}
}

func Example_queryWithSyntheticTables() {
	gql := `query {
		me {
			email
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	conf.Tables = []core.Table{{Name: "me", Table: "user"}}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"me": {"email": "user1@test.com"}}
}

func Example_queryWithVariables() {
	gql := `query {
		product(id: $product_id, where: { price: { gt: $product_price } }) {
			id
			name
		}
	}`

	vars := json.RawMessage(`{ "product_id": 70 }`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	conf.Vars = map[string]string{"product_price": "50"}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"product": {"id": 70, "name": "Product 70"}}

}

func Example_queryWithMultipleTopLevelTables() {
	gql := `query {
		product {
			id
			name
			customer {
				email
			}
		}
		user {
			id
			email
		}
		purchase {
			id
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"user": {"id": 1, "email": "user1@test.com"}, "product": {"id": 1, "name": "Product 1", "customer": {"email": "user2@test.com"}}, "purchase": {"id": 1}}
}

func Example_queryWithFragments1() {
	gql := `
	fragment userFields1 on user {
		id
		email
	}

	query {
		users {
			...userFields2
			created_at
			...userFields1
		}
	}

	fragment userFields2 on user {
		full_name
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"id": 1, "email": "user1@test.com", "full_name": "User 1", "created_at": "2021-01-09T16:37:01.15627"}, {"id": 2, "email": "user2@test.com", "full_name": "User 2", "created_at": "2021-01-09T16:37:01.15627"}]}
}

func Example_queryWithFragments2() {
	gql := `
	query {
		users {
			...userFields2

			created_at
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

	conf := &core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"id": 1, "email": "user1@test.com", "full_name": "User 1", "created_at": "2021-01-09T16:37:01.15627"}, {"id": 2, "email": "user2@test.com", "full_name": "User 2", "created_at": "2021-01-09T16:37:01.15627"}]}
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
		users {
			...userFields2
			created_at
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"id": 1, "email": "user1@test.com", "full_name": "User 1", "created_at": "2021-01-09T16:37:01.15627"}, {"id": 2, "email": "user2@test.com", "full_name": "User 2", "created_at": "2021-01-09T16:37:01.15627"}]}
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
			key
			subjects {
				...on users {
					...userFields
				}
				...on products {
					...productFields
				}
			}
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2}
	conf.Tables = []core.Table{{
		Name:    "subject",
		Type:    "polymorphic",
		Columns: []core.Column{{Name: "subject_id", ForeignKey: "subject_type.id"}},
	}}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"notifications": [{"id": 1, "key": "Joined", "subjects": [{"email": "user1@test.com"}]}, {"id": 2, "key": "Bought", "subjects": [{"name": "Product 2"}]}]}
}

func Example_queryWithSkipAndIncludeDirectives() {
	gql := `
	query {
		products(limit: 2) @include(if: $test) {
			id
			name
		}
		users(limit: 3) @skip(if: $test) {
			id
		}
	}`

	vars := json.RawMessage(`{ "test": true }`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [], "products": [{"id": 1, "name": "Product 1"}, {"id": 2, "name": "Product 2"}]}
}

/*
func Example_subscription() {
	gql := `subscription test {
		user(id: $id) {
			id
			email
		}
	}`

	vars := json.RawMessage(`{ "id": 3 }`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true, PollDuration: 1}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	m, err := gj.Subscribe(context.Background(), gql, vars)
	if err != nil {
		fmt.Println(err)
		return
	}
	msg := <-m.Result
	fmt.Println(string(msg.Data))
	// Output: {"user": {"id": 3, "email": "user3@test.com"}}
}
*/

func Example_queryWithRemoteAPIJoin() {
	gql := `query {
		users {
			email
			payments {
				desc
			}
		}
	}`

	// fake remote api service
	go func() {
		http.HandleFunc("/payments/", func(w http.ResponseWriter, r *http.Request) {
			id := r.URL.Path[10:]
			fmt.Fprintf(w, `{"data":[{"desc":"Payment 1 for %s"},{"desc": "Payment 2 for %s"}]}`,
				id, id)
		})
		log.Fatal(http.ListenAndServe(":12345", nil))
	}()

	conf := &core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2}
	conf.Resolvers = []core.ResolverConfig{{
		Name:      "payments",
		Type:      "remote_api",
		Table:     "users",
		Column:    "stripe_id",
		StripPath: "data",
		Props:     core.ResolverProps{"url": "http://localhost:12345/payments/$id"},
	}}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"email": "user1@test.com", "payments":[{"desc":"Payment 1 for payment_id_1001"},{"desc": "Payment 2 for payment_id_1001"}]}, {"email": "user2@test.com", "payments":[{"desc":"Payment 1 for payment_id_1002"},{"desc": "Payment 2 for payment_id_1002"}]}]}
}

func Example_queryWithCursorPagination() {
	gql := `query {
		Products(
			where: { id: { lesser_or_equals: 100 } }
			first: 3
			after: $cursor
			order_by: { price: desc }) {
			Name
		}
		products_cursor
	}`

	vars := json.RawMessage(`{"cursor": null}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars)
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

	fmt.Println(string(val.Products))
	// Output: [{"name": "Product 100"}, {"name": "Product 99"}, {"name": "Product 98"}]
}

func Example_queryWithJsonColumn() {
	gql := `query {
		user {
			id
			category_counts {
				count
				category {
					name
				}
			}
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
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
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"user": {"id": 1, "category_counts": [{"count": 400, "category": {"name": "Category 1"}}, {"count": 600, "category": {"name": "Category 2"}}]}}
}

func Example_queryWithNestedRelationship1() {
	gql := `query {
		reply : comment(id: $id) {
			id
			comments(find: "parents") {
				id
			}
		}
	}`

	vars := json.RawMessage(`{"id": 5}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"reply": {"id": 5, "comments": [{"id": 4}, {"id": 3}, {"id": 2}, {"id": 1}]}}
}

func Example_queryWithNestedRelationship2() {
	gql := `query {
		comment(id: $id) {
			id
			replies: comments(find: "children") {
				id
			}
		}
	}`

	vars := json.RawMessage(`{"id": 95}`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"comment": {"id": 95, "replies": [{"id": 96}, {"id": 97}, {"id": 98}, {"id": 99}, {"id": 100}]}}
}

func Example_queryWithSkippingAuthRequiredSelectors() {
	gql := `query {
		product {
			id
			name
			user(where: { id: { eq: $user_id } }) {
				id
				email
			}
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"product": {"id": 1, "name": "Product 1", "user": null}}
}

func Example_blockQueryWithRoles() {
	gql := `query {
		users {
			id
			full_name
			email
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true}
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
	res, err := gj.GraphQL(ctx, gql, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": null}
}
