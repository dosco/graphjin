package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path"

	"github.com/dosco/graphjin/core"
)

func Example_query() {
	gql := `query {
		products(limit: 3) {
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

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 1, "owner": {"id": 1, "fullName": "User 1"}}, {"id": 2, "owner": {"id": 2, "fullName": "User 2"}}, {"id": 3, "owner": {"id": 3, "fullName": "User 3"}}]}
}

func Example_queryWithUser() {
	gql := `query {
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 31, "owner": {"id": 31}}]}
}

func Example_queryWithDynamicOrderBy() {
	gql := `query getProducts {
		products(order_by: $order, where: { id: { lt: 6 } }, limit: 5) {
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

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	vars1 := json.RawMessage(`{ "order": "price_and_id" }`)

	res1, err1 := gj.GraphQL(context.Background(), gql, vars1, nil)
	if err != nil {
		fmt.Println(err1)
	} else {
		fmt.Println(string(res1.Data))
	}

	vars2 := json.RawMessage(`{ "order": "just_id" }`)

	res2, err2 := gj.GraphQL(context.Background(), gql, vars2, nil)
	if err != nil {
		fmt.Println(err2)
	} else {
		fmt.Println(string(res2.Data))
	}

	// Output:
	// {"products": [{"id": 5, "price": 15.5}, {"id": 4, "price": 14.5}, {"id": 3, "price": 13.5}, {"id": 2, "price": 12.5}, {"id": 1, "price": 11.5}]}
	// {"products": [{"id": 1, "price": 11.5}, {"id": 2, "price": 12.5}, {"id": 3, "price": 13.5}, {"id": 4, "price": 14.5}, {"id": 5, "price": 15.5}]}
}
func Example_queryWithNestedOrderBy() {
	gql := `query getProducts {
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

	res1, err1 := gj.GraphQL(context.Background(), gql, nil, nil)
	if err1 != nil {
		fmt.Println(res1.SQL())
		fmt.Println(err1)
	} else {
		fmt.Println(string(res1.Data))
	}

	// Output: {"products": [{"id": 5, "price": 15.5}, {"id": 4, "price": 14.5}, {"id": 3, "price": 13.5}, {"id": 2, "price": 12.5}, {"id": 1, "price": 11.5}]}
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 99, "name": "Product 99", "price": 109.5}, {"id": 98, "name": "Product 98", "price": 108.5}, {"id": 97, "name": "Product 97", "price": 107.5}, {"id": 96, "name": "Product 96", "price": 106.5}, {"id": 95, "name": "Product 95", "price": 105.5}]}
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 1}, {"id": 2}, {"id": 3}]}
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
			limit: 3) {
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 1, "name": "Product 1", "price": 11.5}, {"id": 2, "name": "Product 2", "price": 12.5}, {"id": 3, "name": "Product 3", "price": 13.5}]}
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

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 1, "name": "Product 1", "price": 11.5}, {"id": 2, "name": "Product 2", "price": 12.5}, {"id": 3, "name": "Product 3", "price": 13.5}]}
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 2, "owner": {"id": 2, "email": "user2@test.com"}}, {"id": 3, "owner": {"id": 3, "email": "user3@test.com"}}]}
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

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"comments": [{"id": 1, "commenter": {"email": "user1@test.com"}}, {"id": 2, "commenter": {"email": "user2@test.com"}}]}
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": {"id": 2, "name": "Product 2"}}
}

func Example_queryBySearch() {
	gql := `query {
		products(search: $query, limit: 5) {
			id
			name
		}
	}`

	vars := json.RawMessage(`{
		"query": "\"Product 3\""
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 3, "name": "Product 3"}]}
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
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"email": "user1@test.com", "products": [{"name": "Product 1", "price": 11.5}]}, {"email": "user2@test.com", "products": [{"name": "Product 2", "price": 12.5}]}]}
}

func Example_queryChildrenWithParent() {
	gql := `query {
		products(limit: 2) {
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"name": "Product 1", "owner": {"email": "user1@test.com"}, "price": 11.5}, {"name": "Product 2", "owner": {"email": "user2@test.com"}, "price": 12.5}]}
}

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
	conf.Tables = []core.Table{{
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"name": "Product 1", "price": 11.5, "categories": [{"id": 1, "name": "Category 1"}, {"id": 2, "name": "Category 2"}]}, {"name": "Product 2", "price": 12.5, "categories": [{"id": 1, "name": "Category 1"}, {"id": 2, "name": "Category 2"}]}], "categories": [{"name": "Category 1", "products": [{"name": "Product 1"}, {"name": "Product 2"}]}, {"name": "Category 2", "products": [{"name": "Product 1"}, {"name": "Product 2"}]}]}
}

func Example_queryManyToManyViaJoinTable1() {
	gql := `query {
		products(limit: 2) {
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"name": "Product 1", "owner": {"email": "user1@test.com"}, "customer": [{"email": "user2@test.com"}]}, {"name": "Product 2", "owner": {"email": "user2@test.com"}, "customer": [{"email": "user3@test.com"}]}]}
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

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"email": "user1@test.com", "products": [{"name": "Product 1"}], "full_name": "User 1"}, {"email": "user2@test.com", "products": [{"name": "Product 2"}], "full_name": "User 2"}]}
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
		fmt.Println(string(res.Data))
	}
	// Output: functions blocked: price (anon)
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"max_price": 110.5}]}
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
		fmt.Println(string(res.Data))
	}
	// Output: {"me": {"email": "user1@test.com"}}
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": {"id": 70, "name": "Product 70"}}

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
		fmt.Println(string(res.Data))
	}
	// Output: {"users": {"id": 1, "email": "user1@test.com"}, "products": {"id": 1, "name": "Product 1", "customer": [{"email": "user2@test.com"}]}, "purchases": {"id": 1}}
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
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"id": 1, "email": "user1@test.com", "full_name": "User 1", "stripe_id": "payment_id_1001"}, {"id": 2, "email": "user2@test.com", "full_name": "User 2", "stripe_id": "payment_id_1002"}]}
}

func Example_queryWithFragments2() {
	gql := `
	query {
		users {
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
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"id": 1, "email": "user1@test.com", "full_name": "User 1", "stripe_id": "payment_id_1001"}, {"id": 2, "email": "user2@test.com", "full_name": "User 2", "stripe_id": "payment_id_1002"}]}
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
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"id": 1, "email": "user1@test.com", "full_name": "User 1", "stripe_id": "payment_id_1001"}, {"id": 2, "email": "user2@test.com", "full_name": "User 2", "stripe_id": "payment_id_1002"}]}
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
		fmt.Println(string(res.Data))
	}
	// Output: {"notifications": [{"id": 1, "verb": "Joined", "subject": {"email": "user1@test.com"}}, {"id": 2, "verb": "Bought", "subject": {"name": "Product 2"}}]}
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

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [], "products": [{"id": 1, "name": "Product 1"}, {"id": 2, "name": "Product 2"}]}
}

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
		log.Fatal(http.ListenAndServe("localhost:12345", nil))
	}()

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2})
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

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": [{"email": "user1@test.com", "payments":[{"desc":"Payment 1 for payment_id_1001"},{"desc": "Payment 2 for payment_id_1001"}]}, {"email": "user2@test.com", "payments":[{"desc":"Payment 1 for payment_id_1002"},{"desc": "Payment 2 for payment_id_1002"}]}]}
}

func Example_queryWithCursorPagination() {
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

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
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

	fmt.Println(string(val.Products))
	// Output: [{"name": "Product 100"}, {"name": "Product 99"}, {"name": "Product 98"}]
}

func Example_queryWithJsonColumn() {
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
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"users": {"id": 1, "category_counts": [{"count": 400, "category": {"name": "Category 1"}}, {"count": 600, "category": {"name": "Category 2"}}]}}
}

func Example_queryWithScriptDirective() {
	gql := `query @script(name: "test.js") {
		usersByID(id: $id)  {
			id
			email
		}
	}`

	script := `
	function request(vars) {
		return { id: 2 };
	}
	
	function response(json) {
		json.usersByID.email = "u...@test.com";
		return json;
	}
	`

	dir, err := ioutil.TempDir("", "test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	err = ioutil.WriteFile(path.Join(dir, "test.js"), []byte(script), 0644)
	if err != nil {
		panic(err)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, ScriptPath: dir})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}
	// Output: {"usersByID":{"email":"u...@test.com","id":2}}
}

func Example_queryWithScriptDirectiveUsingGraphQL() {
	gql := `query @script(name: "test.js") {
		usersByID(id: 2)  {
			id
			email
		}
	}`

	script := `
	function response(json) {
		let val = graphql('query { users(id: 1) { id email } }')
		json.usersByID.email = val.users.email
		return json;
	}
	`

	dir, err := ioutil.TempDir("", "test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	err = ioutil.WriteFile(path.Join(dir, "test.js"), []byte(script), 0644)
	if err != nil {
		panic(err)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, ScriptPath: dir})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}

	// Output: {"usersByID":{"email":"user1@test.com","id":2}}
}

func Example_queryWithScriptDirectiveUsingHttp() {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{ "hello": "world" }`)
	}))
	defer ts.Close()

	gql := `query @script(name: "test.js") {
		usersByID(id: 2)  {
			id
			email
		}
	}`

	script := `
	function response(json) {
		let val = http.get("` + ts.URL + `")
		return JSON.parse(val);
	}
	`

	dir, err := ioutil.TempDir("", "test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	err = ioutil.WriteFile(path.Join(dir, "test.js"), []byte(script), 0644)
	if err != nil {
		panic(err)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, ScriptPath: dir})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(res.Data))
	}

	// Output: {"hello":"world"}
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
		fmt.Println(string(res.Data))
	}
	// Output: {"hot_products": [{"product": {"id": 51, "name": "Product 51"}}, {"product": {"id": 52, "name": "Product 52"}}, {"product": {"id": 53, "name": "Product 53"}}]}
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
		fmt.Println(string(res.Data))
	}
	// Output: {"reply": {"id": 50, "comments": [{"id": 49}, {"id": 48}, {"id": 47}, {"id": 46}, {"id": 45}]}}
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
		fmt.Println(string(res.Data))
	}

	// Output: {"comments": {"id": 95, "replies": [{"id": 96}, {"id": 97}, {"id": 98}, {"id": 99}, {"id": 100}]}}
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
		fmt.Println(string(res.Data))
	}

	// Output: {"comments": {"id": 95, "replies": [{"count_id": 5}]}}
}

func Example_queryWithSkippingAuthRequiredSelectors() {
	gql := `query {
		products(limit: 2) {
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
		fmt.Println(string(res.Data))
	}
	// Output: {"products": [{"id": 1, "name": "Product 1", "owner": null}, {"id": 2, "name": "Product 2", "owner": null}]}
}

func Example_blockQueryWithRoles() {
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
		fmt.Println(string(res.Data))
	}
	// Output: {"users": null}
}

func Example_queryWithCamelToSnakeCase() {
	gql := `query {
		hotProducts(where: { productID: { eq: 55 } }) {
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
		fmt.Println(string(res.Data))
	}
	// Output: {"hotProducts": [{"products": {"id": 55}, "countryCode": "US", "countProductID": 1}]}
}
