package psql

import (
	"bytes"
	"testing"
)

func withComplexArgs(t *testing.T) {
	gql := `query {
		proDUcts(
			# returns only 30 items
			limit: 30,
	
			# starts from item 10, commented out for now
			# offset: 10,
	
			# orders the response items by highest price
			order_by: { price: desc },
	
			# no duplicate prices returned
			distinct: [ price ]
			
			# only items with an id >= 20 and < 28 are returned
			where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) {
			id
			NAME
			price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("sel_json_0" ORDER BY "products_0_price_ob" DESC), '[]') AS "products" FROM (SELECT DISTINCT ON ("products_0_price_ob") row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "products_0"."price" AS "price") AS "sel_0")) AS "sel_json_0", "products_0"."price" AS "products_0_price_ob" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."price") > 0) AND (("products"."price") < 8) AND (("products"."id") < 28) AND (("products"."id") >= 20)) LIMIT ('30') :: integer) AS "products_0" ORDER BY "products_0_price_ob" DESC LIMIT ('30') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func withWhereMultiOr(t *testing.T) {
	gql := `query {
		products(
			where: {
				or: {
					not: { id: { is_null: true } },
					price: { gt: 10 },
					price: { lt: 20 }
				} }
			) {
			id
			name
			price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "products_0"."price" AS "price") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."price") > 0) AND (("products"."price") < 8) AND (("products"."price") < 20) OR (("products"."price") > 10) OR NOT (("products"."id") IS NULL)) LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func withWhereIsNull(t *testing.T) {
	gql := `query {
		products(
			where: {
				and: {
					not: { id: { is_null: true } },
					price: { gt: 10 }
				}}) {
			id
			name
			price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "products_0"."price" AS "price") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."price") > 0) AND (("products"."price") < 8) AND (("products"."price") > 10) AND NOT (("products"."id") IS NULL)) LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func withWhereAndList(t *testing.T) {
	gql := `query {
		products(
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

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "products_0"."price" AS "price") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."price") > 0) AND (("products"."price") < 8) AND (("products"."price") > 10) AND NOT (("products"."id") IS NULL)) LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func fetchByID(t *testing.T) {
	gql := `query {
		product(id: 15) {
			id
			name
		}
	}`

	sql := `SELECT json_object_agg('product', sel_json_0) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."id", "products"."name" FROM "products" WHERE ((("products"."price") > 0) AND (("products"."price") < 8) AND (("products"."id") = 15)) LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func searchQuery(t *testing.T) {
	gql := `query {
		products(search: "Imperial") {
			id
			name
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."id", "products"."name" FROM "products" WHERE ((("products"."price") > 0) AND (("products"."price") < 8) AND (("tsv") @@ to_tsquery('Imperial'))) LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func oneToMany(t *testing.T) {
	gql := `query {
		users {
			email
			products {
				name
				price
			}
		}
	}`

	sql := `SELECT json_object_agg('users', users) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "users" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "users_0"."email" AS "email", "products_1_join"."products" AS "products") AS "sel_0")) AS "sel_json_0" FROM (SELECT "users"."email", "users"."id" FROM "users" LIMIT ('20') :: integer) AS "users_0" LEFT OUTER JOIN LATERAL (SELECT coalesce(json_agg("sel_json_1"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_1" FROM (SELECT "products_1"."name" AS "name", "products_1"."price" AS "price") AS "sel_1")) AS "sel_json_1" FROM (SELECT "products"."name", "products"."price" FROM "products" WHERE ((("products"."user_id") = ("users_0"."id"))) LIMIT ('20') :: integer) AS "products_1" LIMIT ('20') :: integer) AS "sel_json_agg_1") AS "products_1_join" ON ('true') LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func belongsTo(t *testing.T) {
	gql := `query {
		products {
			name
			price
			users {
				email
			}
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."name" AS "name", "products_0"."price" AS "price", "users_1_join"."users" AS "users") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."name", "products"."price", "products"."user_id" FROM "products" WHERE ((("products"."price") > 0) AND (("products"."price") < 8)) LIMIT ('20') :: integer) AS "products_0" LEFT OUTER JOIN LATERAL (SELECT coalesce(json_agg("sel_json_1"), '[]') AS "users" FROM (SELECT row_to_json((SELECT "sel_1" FROM (SELECT "users_1"."email" AS "email") AS "sel_1")) AS "sel_json_1" FROM (SELECT "users"."email" FROM "users" WHERE ((("users"."id") = ("products_0"."user_id"))) LIMIT ('20') :: integer) AS "users_1" LIMIT ('20') :: integer) AS "sel_json_agg_1") AS "users_1_join" ON ('true') LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func manyToMany(t *testing.T) {
	gql := `query {
		products {
			name
			customers {
				email
				full_name
			}
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."name" AS "name", "customers_1_join"."customers" AS "customers") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."name", "products"."id" FROM "products" WHERE ((("products"."price") > 0) AND (("products"."price") < 8)) LIMIT ('20') :: integer) AS "products_0" LEFT OUTER JOIN LATERAL (SELECT coalesce(json_agg("sel_json_1"), '[]') AS "customers" FROM (SELECT row_to_json((SELECT "sel_1" FROM (SELECT "customers_1"."email" AS "email", "customers_1"."full_name" AS "full_name") AS "sel_1")) AS "sel_json_1" FROM (SELECT "customers"."email", "customers"."full_name" FROM "customers" LEFT OUTER JOIN "purchases" ON (("purchases"."product_id") = ("products_0"."id")) WHERE ((("customers"."id") = ("purchases"."customer_id"))) LIMIT ('20') :: integer) AS "customers_1" LIMIT ('20') :: integer) AS "sel_json_agg_1") AS "customers_1_join" ON ('true') LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func manyToManyReverse(t *testing.T) {
	gql := `query {
		customers {
			email
			full_name
			products {
				name
			}
		}
	}`

	sql := `SELECT json_object_agg('customers', customers) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "customers" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "customers_0"."email" AS "email", "customers_0"."full_name" AS "full_name", "products_1_join"."products" AS "products") AS "sel_0")) AS "sel_json_0" FROM (SELECT "customers"."email", "customers"."full_name", "customers"."id" FROM "customers" LIMIT ('20') :: integer) AS "customers_0" LEFT OUTER JOIN LATERAL (SELECT coalesce(json_agg("sel_json_1"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_1" FROM (SELECT "products_1"."name" AS "name") AS "sel_1")) AS "sel_json_1" FROM (SELECT "products"."name" FROM "products" LEFT OUTER JOIN "purchases" ON (("purchases"."customer_id") = ("customers_0"."id")) WHERE ((("products"."id") = ("purchases"."product_id"))) LIMIT ('20') :: integer) AS "products_1" LIMIT ('20') :: integer) AS "sel_json_agg_1") AS "products_1_join" ON ('true') LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func aggFunction(t *testing.T) {
	gql := `query {
		products {
			name
			count_price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."name" AS "name", "products_0"."count_price" AS "count_price") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."name", count("products"."price") AS "count_price" FROM "products" WHERE ((("products"."price") > 0) AND (("products"."price") < 8)) GROUP BY "products"."name" LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func aggFunctionBlockedByCol(t *testing.T) {
	gql := `query {
		products {
			name
			count_price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."name" AS "name") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."name" FROM "products" LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "anon")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func aggFunctionDisabled(t *testing.T) {
	gql := `query {
		products {
			name
			count_price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."name" AS "name") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."name" FROM "products" LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "anon1")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func aggFunctionWithFilter(t *testing.T) {
	gql := `query {
		products(where: { id: { gt: 10 } }) {
			id
			max_price
		}
	}`

	sql := `SELECT json_object_agg('products', products) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "products" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."max_price" AS "max_price") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."id", max("products"."price") AS "max_price" FROM "products" WHERE ((("products"."price") > 0) AND (("products"."price") < 8) AND (("products"."id") > 10)) GROUP BY "products"."id" LIMIT ('20') :: integer) AS "products_0" LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func queryWithVariables(t *testing.T) {
	gql := `query {
		product(id: $PRODUCT_ID, where: { price: { eq: $PRODUCT_PRICE } }) {
			id
			name
		}
	}`

	sql := `SELECT json_object_agg('product', sel_json_0) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."id", "products"."name" FROM "products" WHERE ((("products"."price") > 0) AND (("products"."price") < 8) AND (("products"."price") = {{product_price}}) AND (("products"."id") = {{product_id}})) LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func syntheticTables(t *testing.T) {
	gql := `query {
		me {
			email
		}
	}`

	sql := `SELECT json_object_agg('me', sel_json_0) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT ) AS "sel_0")) AS "sel_json_0" FROM (SELECT "users"."email" FROM "users" WHERE ((("users"."id") = {{user_id}})) LIMIT ('1') :: integer) AS "users_0" LIMIT ('1') :: integer) AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func TestCompileQuery(t *testing.T) {
	t.Run("withComplexArgs", withComplexArgs)
	t.Run("withWhereAndList", withWhereAndList)
	t.Run("withWhereIsNull", withWhereIsNull)
	t.Run("withWhereMultiOr", withWhereMultiOr)
	t.Run("fetchByID", fetchByID)
	t.Run("searchQuery", searchQuery)
	t.Run("belongsTo", belongsTo)
	t.Run("oneToMany", oneToMany)
	t.Run("manyToMany", manyToMany)
	t.Run("manyToManyReverse", manyToManyReverse)
	t.Run("aggFunction", aggFunction)
	t.Run("aggFunctionBlockedByCol", aggFunctionBlockedByCol)
	t.Run("aggFunctionDisabled", aggFunctionDisabled)
	t.Run("aggFunctionWithFilter", aggFunctionWithFilter)
	t.Run("syntheticTables", syntheticTables)
	t.Run("queryWithVariables", queryWithVariables)
	t.Run("blockedQuery", blockedQuery)
	t.Run("blockedFunctions", blockedFunctions)
}

var benchGQL = []byte(`query {
	proDUcts(
		# returns only 30 items
		limit: 30,

		# starts from item 10, commented out for now
		# offset: 10,

		# orders the response items by highest price
		order_by: { price: desc },

		# only items with an id >= 30 and < 30 are returned
		where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) {
		id
		NAME
		price
		user {
			full_name
			picture : avatar
		}
	}
}`)

func blockedQuery(t *testing.T) {
	gql := `query {
		user(id: 5, where: { id: { gt: 3 } }) {
			id
			full_name
			email
		}
	}`

	sql := `SELECT json_object_agg('user', sel_json_0) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "users_0"."id" AS "id", "users_0"."full_name" AS "full_name", "users_0"."email" AS "email") AS "sel_0")) AS "sel_json_0" FROM (SELECT "users"."id", "users"."full_name", "users"."email" FROM "users" WHERE (false) LIMIT ('1') :: integer) AS "users_0" LIMIT ('1') :: integer) AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "bad_dude")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func blockedFunctions(t *testing.T) {
	gql := `query {
		users {
			count_id
			email
		}
	}`

	sql := `SELECT json_object_agg('users', users) FROM (SELECT coalesce(json_agg("sel_json_0"), '[]') AS "users" FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "users_0"."email" AS "email") AS "sel_0")) AS "sel_json_0" FROM (SELECT "users"."email" FROM "users" WHERE (false) LIMIT ('20') :: integer) AS "users_0" LIMIT ('20') :: integer) AS "sel_json_agg_0") AS "done_1337"`

	resSQL, err := compileGQLToPSQL(gql, nil, "bad_dude")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func BenchmarkCompile(b *testing.B) {
	w := &bytes.Buffer{}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		w.Reset()

		qc, err := qcompile.Compile(benchGQL, "user")
		if err != nil {
			b.Fatal(err)
		}

		_, err = pcompile.Compile(qc, w, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileParallel(b *testing.B) {
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		w := &bytes.Buffer{}

		for pb.Next() {
			w.Reset()

			qc, err := qcompile.Compile(benchGQL, "user")
			if err != nil {
				b.Fatal(err)
			}

			_, err = pcompile.Compile(qc, w, nil)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
