package psql

import (
	"encoding/json"
	"fmt"
	"testing"
)

func singleUpdate(t *testing.T) {
	gql := `mutation {
		product(id: 15, update: $update, where: { id: { eq: 1 } }) {
			id
			name
		}
	}`

	sql := `WITH "_sg_input" AS (SELECT '{{update}}' :: json AS j), "products" AS (UPDATE "products" SET ("name", "description") = (SELECT "t"."name", "t"."description" FROM "_sg_input" i, json_populate_record(NULL::products, i.j) t WHERE (("products"."id") = 1) AND (("products"."id") = 15)) RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name" FROM "products" LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"update": json.RawMessage(` { "name": "my_name", "description": "my_desc"  }`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "anon")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func simpleUpdateWithPresets(t *testing.T) {
	gql := `mutation {
		product(update: $data) {
			id
		}
	}`

	sql := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "products" AS (UPDATE "products" SET ("name", "price", "updated_at") = (SELECT "t"."name", "t"."price", 'now' :: timestamp without time zone FROM "_sg_input" i, json_populate_record(NULL::products, i.j) t WHERE (("products"."user_id") = '{{user_id}}' :: bigint)) RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id" FROM "products" LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{"name": "Apple", "price": 1.25}`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func nestedUpdateManyToMany(t *testing.T) {
	gql := `mutation {
		purchase(update: $data, id: 5) {
			sale_type
			quantity
			due_date
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

	sql1 := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "purchases" AS (UPDATE "purchases" SET ("sale_type", "quantity", "due_date") = (SELECT "t"."sale_type", "t"."quantity", "t"."due_date" FROM "_sg_input" i, json_populate_record(NULL::purchases, i.j) t WHERE (("purchases"."id") = 5)) RETURNING *), "customers" AS (UPDATE "customers" SET ("full_name", "email") = (SELECT "t"."full_name", "t"."email" FROM "_sg_input" i, json_populate_record(NULL::customers, i.j) t WHERE (("customers"."id") = ("purchases"."customer_id"))) RETURNING *), "products" AS (UPDATE "products" SET ("name", "price") = (SELECT "t"."name", "t"."price" FROM "_sg_input" i, json_populate_record(NULL::products, i.j) t WHERE (("products"."id") = ("purchases"."product_id"))) RETURNING *) SELECT json_object_agg('purchase', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "purchases_0"."sale_type" AS "sale_type", "purchases_0"."quantity" AS "quantity", "purchases_0"."due_date" AS "due_date", "product_1_join"."json_1" AS "product", "customer_2_join"."json_2" AS "customer") AS "json_row_0")) AS "json_0" FROM (SELECT "purchases"."sale_type", "purchases"."quantity", "purchases"."due_date", "purchases"."product_id", "purchases"."customer_id" FROM "purchases" LIMIT ('1') :: integer) AS "purchases_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_2" FROM (SELECT "customers_2"."id" AS "id", "customers_2"."full_name" AS "full_name", "customers_2"."email" AS "email") AS "json_row_2")) AS "json_2" FROM (SELECT "customers"."id", "customers"."full_name", "customers"."email" FROM "customers" WHERE ((("customers"."id") = ("purchases_0"."customer_id"))) LIMIT ('1') :: integer) AS "customers_2" LIMIT ('1') :: integer) AS "customer_2_join" ON ('true') LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "products_1"."id" AS "id", "products_1"."name" AS "name", "products_1"."price" AS "price") AS "json_row_1")) AS "json_1" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."id") = ("purchases_0"."product_id"))) LIMIT ('1') :: integer) AS "products_1" LIMIT ('1') :: integer) AS "product_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

	sql2 := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "purchases" AS (UPDATE "purchases" SET ("sale_type", "quantity", "due_date") = (SELECT "t"."sale_type", "t"."quantity", "t"."due_date" FROM "_sg_input" i, json_populate_record(NULL::purchases, i.j) t WHERE (("purchases"."id") = 5)) RETURNING *), "products" AS (UPDATE "products" SET ("name", "price") = (SELECT "t"."name", "t"."price" FROM "_sg_input" i, json_populate_record(NULL::products, i.j) t WHERE (("products"."id") = ("purchases"."product_id"))) RETURNING *), "customers" AS (UPDATE "customers" SET ("full_name", "email") = (SELECT "t"."full_name", "t"."email" FROM "_sg_input" i, json_populate_record(NULL::customers, i.j) t WHERE (("customers"."id") = ("purchases"."customer_id"))) RETURNING *) SELECT json_object_agg('purchase', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "purchases_0"."sale_type" AS "sale_type", "purchases_0"."quantity" AS "quantity", "purchases_0"."due_date" AS "due_date", "product_1_join"."json_1" AS "product", "customer_2_join"."json_2" AS "customer") AS "json_row_0")) AS "json_0" FROM (SELECT "purchases"."sale_type", "purchases"."quantity", "purchases"."due_date", "purchases"."product_id", "purchases"."customer_id" FROM "purchases" LIMIT ('1') :: integer) AS "purchases_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_2" FROM (SELECT "customers_2"."id" AS "id", "customers_2"."full_name" AS "full_name", "customers_2"."email" AS "email") AS "json_row_2")) AS "json_2" FROM (SELECT "customers"."id", "customers"."full_name", "customers"."email" FROM "customers" WHERE ((("customers"."id") = ("purchases_0"."customer_id"))) LIMIT ('1') :: integer) AS "customers_2" LIMIT ('1') :: integer) AS "customer_2_join" ON ('true') LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "products_1"."id" AS "id", "products_1"."name" AS "name", "products_1"."price" AS "price") AS "json_row_1")) AS "json_1" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."id") = ("purchases_0"."product_id"))) LIMIT ('1') :: integer) AS "products_1" LIMIT ('1') :: integer) AS "product_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(` {
			"sale_type": "bought",
			"quantity": 5,
			"due_date": "now",
			"customer": {
				"email": "thedude@rug.com",
				"full_name": "The Dude"
			},
			"product": {
				"name": "Apple",
				"price": 1.25
			}
		}
	`),
	}

	for i := 0; i < 1000; i++ {
		resSQL, err := compileGQLToPSQL(gql, vars, "admin")
		if err != nil {
			t.Fatal(err)
		}

		if string(resSQL) != sql1 && string(resSQL) != sql2 {
			t.Fatal(errNotExpected)
		}
	}

}

func nestedUpdateOneToMany(t *testing.T) {
	gql := `mutation {
		user(update: $data, where: { id: { eq: 8 } }) {
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

	sql := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "users" AS (UPDATE "users" SET ("full_name", "email", "created_at", "updated_at") = (SELECT "t"."full_name", "t"."email", "t"."created_at", "t"."updated_at" FROM "_sg_input" i, json_populate_record(NULL::users, i.j) t WHERE (("users"."id") = 8)) RETURNING *), "products" AS (UPDATE "products" SET ("name", "price", "created_at", "updated_at") = (SELECT "t"."name", "t"."price", "t"."created_at", "t"."updated_at" FROM "_sg_input" i, json_populate_record(NULL::products, i.j) t WHERE (("products"."user_id") = ("users"."id") AND "id" = '2')) RETURNING *) SELECT json_object_agg('user', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "users_0"."id" AS "id", "users_0"."full_name" AS "full_name", "users_0"."email" AS "email", "product_1_join"."json_1" AS "product") AS "json_row_0")) AS "json_0" FROM (SELECT "users"."id", "users"."full_name", "users"."email" FROM "users" LIMIT ('1') :: integer) AS "users_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "products_1"."id" AS "id", "products_1"."name" AS "name", "products_1"."price" AS "price") AS "json_row_1")) AS "json_1" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."user_id") = ("users_0"."id"))) LIMIT ('1') :: integer) AS "products_1" LIMIT ('1') :: integer) AS "product_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"email": "thedude@rug.com",
			"full_name": "The Dude",
			"created_at": "now",
			"updated_at": "now",
			"product": {
				"where": {
					"id": 2
				},
				"name": "Apple",
				"price": 1.25,
				"created_at": "now",
				"updated_at": "now"
			}
		}`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "admin")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func nestedUpdateOneToOne(t *testing.T) {
	gql := `mutation {
		product(update: $data, id: 6) {
			id
			name
			user {
				id
				full_name
				email
			}
		}
	}`

	sql := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "products" AS (UPDATE "products" SET ("name", "price", "created_at", "updated_at") = (SELECT "t"."name", "t"."price", "t"."created_at", "t"."updated_at" FROM "_sg_input" i, json_populate_record(NULL::products, i.j) t WHERE (("products"."id") = 6)) RETURNING *), "users" AS (UPDATE "users" SET ("email") = (SELECT "t"."email" FROM "_sg_input" i, json_populate_record(NULL::users, i.j) t WHERE (("users"."id") = ("products"."user_id"))) RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "user_1_join"."json_1" AS "user") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name", "products"."user_id" FROM "products" LIMIT ('1') :: integer) AS "products_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "users_1"."id" AS "id", "users_1"."full_name" AS "full_name", "users_1"."email" AS "email") AS "json_row_1")) AS "json_1" FROM (SELECT "users"."id", "users"."full_name", "users"."email" FROM "users" WHERE ((("users"."id") = ("products_0"."user_id"))) LIMIT ('1') :: integer) AS "users_1" LIMIT ('1') :: integer) AS "user_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"name": "Apple",
			"price": 1.25,
			"created_at": "now",
			"updated_at": "now",
			"user": {
				"email": "thedude@rug.com"
			}
		}`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "admin")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func nestedUpdateOneToManyWithConnect(t *testing.T) {
	gql := `mutation {
		user(update: $data, id: 6) {
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

	sql1 := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "users" AS (UPDATE "users" SET ("full_name", "email", "created_at", "updated_at") = (SELECT "t"."full_name", "t"."email", "t"."created_at", "t"."updated_at" FROM "_sg_input" i, json_populate_record(NULL::users, i.j) t WHERE (("users"."id") = 6)) RETURNING *), "products_3" AS (UPDATE "products" SET "user_id" = NULL  WHERE "products"."user_id" = "users"."id" AND "id" = '8' RETURNING *), "products_2" AS (UPDATE "products" SET "user_id" = "users"."id" WHERE "id" = '7' RETURNING *), "products" AS (SELECT * FROM "products_2" UNION ALL SELECT * FROM "products_3") SELECT json_object_agg('user', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "users_0"."id" AS "id", "users_0"."full_name" AS "full_name", "users_0"."email" AS "email", "product_1_join"."json_1" AS "product") AS "json_row_0")) AS "json_0" FROM (SELECT "users"."id", "users"."full_name", "users"."email" FROM "users" LIMIT ('1') :: integer) AS "users_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "products_1"."id" AS "id", "products_1"."name" AS "name", "products_1"."price" AS "price") AS "json_row_1")) AS "json_1" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."user_id") = ("users_0"."id"))) LIMIT ('1') :: integer) AS "products_1" LIMIT ('1') :: integer) AS "product_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

	sql2 := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "users" AS (UPDATE "users" SET ("full_name", "email", "created_at", "updated_at") = (SELECT "t"."full_name", "t"."email", "t"."created_at", "t"."updated_at" FROM "_sg_input" i, json_populate_record(NULL::users, i.j) t WHERE (("users"."id") = 6)) RETURNING *), "products_3" AS (UPDATE "products" SET "user_id" = "users"."id" WHERE "id" = '7' RETURNING *), "products_2" AS (UPDATE "products" SET "user_id" = NULL  WHERE "products"."user_id" = "users"."id" AND "id" = '8' RETURNING *), "products" AS (SELECT * FROM "products_2" UNION ALL SELECT * FROM "products_3") SELECT json_object_agg('user', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "users_0"."id" AS "id", "users_0"."full_name" AS "full_name", "users_0"."email" AS "email", "product_1_join"."json_1" AS "product") AS "json_row_0")) AS "json_0" FROM (SELECT "users"."id", "users"."full_name", "users"."email" FROM "users" LIMIT ('1') :: integer) AS "users_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "products_1"."id" AS "id", "products_1"."name" AS "name", "products_1"."price" AS "price") AS "json_row_1")) AS "json_1" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."user_id") = ("users_0"."id"))) LIMIT ('1') :: integer) AS "products_1" LIMIT ('1') :: integer) AS "product_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"email": "thedude@rug.com",
			"full_name": "The Dude",
			"created_at": "now",
			"updated_at": "now",
			"product": {
				"connect": { "id": 7 },
				"disconnect": { "id": 8 }
			}
		}`),
	}

	for i := 0; i < 1000; i++ {
		resSQL, err := compileGQLToPSQL(gql, vars, "admin")
		if err != nil {
			t.Fatal(err)
		}

		if string(resSQL) != sql1 && string(resSQL) != sql2 {
			t.Fatal(errNotExpected)
		}
	}
}

func nestedUpdateOneToOneWithConnect(t *testing.T) {
	gql := `mutation {
		product(update: $data, id: 9) {
			id
			name
			user {
				id
				full_name
				email
			}
		}
	}`

	sql := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "users" AS (SELECT * FROM "users" WHERE "id" = '5' AND "email" = 'test@test.com' LIMIT 1), "products" AS (UPDATE "products" SET ("name", "price", "created_at", "updated_at", "user_id") = (SELECT "t"."name", "t"."price", "t"."created_at", "t"."updated_at", "users"."id" FROM "_sg_input" i, "users", json_populate_record(NULL::products, i.j) t WHERE (("products"."id") = 9)) RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "user_1_join"."json_1" AS "user") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name", "products"."user_id" FROM "products" LIMIT ('1') :: integer) AS "products_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "users_1"."id" AS "id", "users_1"."full_name" AS "full_name", "users_1"."email" AS "email") AS "json_row_1")) AS "json_1" FROM (SELECT "users"."id", "users"."full_name", "users"."email" FROM "users" WHERE ((("users"."id") = ("products_0"."user_id"))) LIMIT ('1') :: integer) AS "users_1" LIMIT ('1') :: integer) AS "user_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"name": "Apple",
			"price": 1.25,
			"created_at": "now",
			"updated_at": "now",
			"user": {
				"connect": { "id": 5, "email": "test@test.com" }
			}
		}`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "admin")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(string(resSQL))

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func TestCompileUpdate(t *testing.T) {
	t.Run("singleUpdate", singleUpdate)
	t.Run("simpleUpdateWithPresets", simpleUpdateWithPresets)
	t.Run("nestedUpdateManyToMany", nestedUpdateManyToMany)
	t.Run("nestedUpdateOneToMany", nestedUpdateOneToMany)
	t.Run("nestedUpdateOneToOne", nestedUpdateOneToOne)
	t.Run("nestedUpdateOneToManyWithConnect", nestedUpdateOneToManyWithConnect)
	t.Run("nestedUpdateOneToOneWithConnect", nestedUpdateOneToOneWithConnect)
}
