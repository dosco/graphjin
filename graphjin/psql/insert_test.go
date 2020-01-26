package psql

import (
	"encoding/json"
	"testing"
)

func simpleInsert(t *testing.T) {
	gql := `mutation {
		user(insert: $data) {
			id
		}
	}`

	sql := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "users" AS (INSERT INTO "users" ("full_name", "email") SELECT "t"."full_name", "t"."email" FROM "_sg_input" i, json_populate_record(NULL::users, i.j) t RETURNING *) SELECT json_object_agg('user', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "users_0"."id" AS "id") AS "json_row_0")) AS "json_0" FROM (SELECT "users"."id" FROM "users" LIMIT ('1') :: integer) AS "users_0" LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{"email": "reannagreenholt@orn.com", "full_name": "Flo Barton"}`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func singleInsert(t *testing.T) {
	gql := `mutation {
		product(id: 15, insert: $insert) {
			id
			name
		}
	}`

	sql := `WITH "_sg_input" AS (SELECT '{{insert}}' :: json AS j), "products" AS (INSERT INTO "products" ("name", "description", "price", "user_id") SELECT "t"."name", "t"."description", "t"."price", "t"."user_id" FROM "_sg_input" i, json_populate_record(NULL::products, i.j) t RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name" FROM "products" LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"insert": json.RawMessage(` { "name": "my_name", "price": 6.95, "description": "my_desc", "user_id": 5 }`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "anon")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func bulkInsert(t *testing.T) {
	gql := `mutation {
		product(name: "test", id: 15, insert: $insert) {
			id
			name
		}
	}`

	sql := `WITH "_sg_input" AS (SELECT '{{insert}}' :: json AS j), "products" AS (INSERT INTO "products" ("name", "description") SELECT "t"."name", "t"."description" FROM "_sg_input" i, json_populate_recordset(NULL::products, i.j) t RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name" FROM "products" LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"insert": json.RawMessage(` [{ "name": "my_name", "description": "my_desc"  }]`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "anon")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func simpleInsertWithPresets(t *testing.T) {
	gql := `mutation {
		product(insert: $data) {
			id
		}
	}`

	sql := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "products" AS (INSERT INTO "products" ("name", "price", "created_at", "updated_at", "user_id") SELECT "t"."name", "t"."price", 'now' :: timestamp without time zone, 'now' :: timestamp without time zone, '{{user_id}}' :: bigint FROM "_sg_input" i, json_populate_record(NULL::products, i.j) t RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id" FROM "products" LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{"name": "Tomato", "price": 5.76}`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func nestedInsertManyToMany(t *testing.T) {
	gql := `mutation {
		purchase(insert: $data) {
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

	sql1 := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "customers" AS (INSERT INTO "customers" ("full_name", "email") SELECT "t"."full_name", "t"."email" FROM "_sg_input" i, json_populate_record(NULL::customers, i.j->'customer') t RETURNING *), "products" AS (INSERT INTO "products" ("name", "price") SELECT "t"."name", "t"."price" FROM "_sg_input" i, json_populate_record(NULL::products, i.j->'product') t RETURNING *), "purchases" AS (INSERT INTO "purchases" ("sale_type", "quantity", "due_date", "product_id", "customer_id") SELECT "t"."sale_type", "t"."quantity", "t"."due_date", "products"."id", "customers"."id" FROM "_sg_input" i, "products", "customers", json_populate_record(NULL::purchases, i.j) t RETURNING *) SELECT json_object_agg('purchase', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "purchases_0"."sale_type" AS "sale_type", "purchases_0"."quantity" AS "quantity", "purchases_0"."due_date" AS "due_date", "product_1_join"."json_1" AS "product", "customer_2_join"."json_2" AS "customer") AS "json_row_0")) AS "json_0" FROM (SELECT "purchases"."sale_type", "purchases"."quantity", "purchases"."due_date", "purchases"."product_id", "purchases"."customer_id" FROM "purchases" LIMIT ('1') :: integer) AS "purchases_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_2" FROM (SELECT "customers_2"."id" AS "id", "customers_2"."full_name" AS "full_name", "customers_2"."email" AS "email") AS "json_row_2")) AS "json_2" FROM (SELECT "customers"."id", "customers"."full_name", "customers"."email" FROM "customers" WHERE ((("customers"."id") = ("purchases_0"."customer_id"))) LIMIT ('1') :: integer) AS "customers_2" LIMIT ('1') :: integer) AS "customer_2_join" ON ('true') LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "products_1"."id" AS "id", "products_1"."name" AS "name", "products_1"."price" AS "price") AS "json_row_1")) AS "json_1" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."id") = ("purchases_0"."product_id"))) LIMIT ('1') :: integer) AS "products_1" LIMIT ('1') :: integer) AS "product_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

	sql2 := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "products" AS (INSERT INTO "products" ("name", "price") SELECT "t"."name", "t"."price" FROM "_sg_input" i, json_populate_record(NULL::products, i.j->'product') t RETURNING *), "customers" AS (INSERT INTO "customers" ("full_name", "email") SELECT "t"."full_name", "t"."email" FROM "_sg_input" i, json_populate_record(NULL::customers, i.j->'customer') t RETURNING *), "purchases" AS (INSERT INTO "purchases" ("sale_type", "quantity", "due_date", "customer_id", "product_id") SELECT "t"."sale_type", "t"."quantity", "t"."due_date", "customers"."id", "products"."id" FROM "_sg_input" i, "customers", "products", json_populate_record(NULL::purchases, i.j) t RETURNING *) SELECT json_object_agg('purchase', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "purchases_0"."sale_type" AS "sale_type", "purchases_0"."quantity" AS "quantity", "purchases_0"."due_date" AS "due_date", "product_1_join"."json_1" AS "product", "customer_2_join"."json_2" AS "customer") AS "json_row_0")) AS "json_0" FROM (SELECT "purchases"."sale_type", "purchases"."quantity", "purchases"."due_date", "purchases"."product_id", "purchases"."customer_id" FROM "purchases" LIMIT ('1') :: integer) AS "purchases_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_2" FROM (SELECT "customers_2"."id" AS "id", "customers_2"."full_name" AS "full_name", "customers_2"."email" AS "email") AS "json_row_2")) AS "json_2" FROM (SELECT "customers"."id", "customers"."full_name", "customers"."email" FROM "customers" WHERE ((("customers"."id") = ("purchases_0"."customer_id"))) LIMIT ('1') :: integer) AS "customers_2" LIMIT ('1') :: integer) AS "customer_2_join" ON ('true') LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "products_1"."id" AS "id", "products_1"."name" AS "name", "products_1"."price" AS "price") AS "json_row_1")) AS "json_1" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."id") = ("purchases_0"."product_id"))) LIMIT ('1') :: integer) AS "products_1" LIMIT ('1') :: integer) AS "product_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

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

	sql := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "users" AS (INSERT INTO "users" ("full_name", "email", "created_at", "updated_at") SELECT "t"."full_name", "t"."email", "t"."created_at", "t"."updated_at" FROM "_sg_input" i, json_populate_record(NULL::users, i.j) t RETURNING *), "products" AS (INSERT INTO "products" ("name", "price", "created_at", "updated_at", "user_id") SELECT "t"."name", "t"."price", "t"."created_at", "t"."updated_at", "users"."id" FROM "_sg_input" i, "users", json_populate_record(NULL::products, i.j->'product') t RETURNING *) SELECT json_object_agg('user', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "users_0"."id" AS "id", "users_0"."full_name" AS "full_name", "users_0"."email" AS "email", "product_1_join"."json_1" AS "product") AS "json_row_0")) AS "json_0" FROM (SELECT "users"."id", "users"."full_name", "users"."email" FROM "users" LIMIT ('1') :: integer) AS "users_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "products_1"."id" AS "id", "products_1"."name" AS "name", "products_1"."price" AS "price") AS "json_row_1")) AS "json_1" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."user_id") = ("users_0"."id"))) LIMIT ('1') :: integer) AS "products_1" LIMIT ('1') :: integer) AS "product_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

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

	resSQL, err := compileGQLToPSQL(gql, vars, "admin")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
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

	sql := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "users" AS (INSERT INTO "users" ("full_name", "email", "created_at", "updated_at") SELECT "t"."full_name", "t"."email", "t"."created_at", "t"."updated_at" FROM "_sg_input" i, json_populate_record(NULL::users, i.j->'user') t RETURNING *), "products" AS (INSERT INTO "products" ("name", "price", "created_at", "updated_at", "user_id") SELECT "t"."name", "t"."price", "t"."created_at", "t"."updated_at", "users"."id" FROM "_sg_input" i, "users", json_populate_record(NULL::products, i.j) t RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "user_1_join"."json_1" AS "user") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name", "products"."user_id" FROM "products" LIMIT ('1') :: integer) AS "products_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "users_1"."id" AS "id", "users_1"."full_name" AS "full_name", "users_1"."email" AS "email") AS "json_row_1")) AS "json_1" FROM (SELECT "users"."id", "users"."full_name", "users"."email" FROM "users" WHERE ((("users"."id") = ("products_0"."user_id"))) LIMIT ('1') :: integer) AS "users_1" LIMIT ('1') :: integer) AS "user_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"name": "Apple",
			"price": 1.25,
			"created_at": "now",
			"updated_at": "now",
			"user": {
				"hey": {
					"now": "what's the matter"
				},
				"email": "thedude@rug.com",
				"full_name": "The Dude",
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

	sql := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "users" AS (INSERT INTO "users" ("full_name", "email", "created_at", "updated_at") SELECT "t"."full_name", "t"."email", "t"."created_at", "t"."updated_at" FROM "_sg_input" i, json_populate_record(NULL::users, i.j) t RETURNING *), "products" AS ( UPDATE "products" SET "user_id" = "users"."id" FROM "users" WHERE ("products"."id"= ((i.j->'product'->'connect'->>'id'))::bigint) RETURNING "products".*) SELECT json_object_agg('user', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "users_0"."id" AS "id", "users_0"."full_name" AS "full_name", "users_0"."email" AS "email", "product_1_join"."json_1" AS "product") AS "json_row_0")) AS "json_0" FROM (SELECT "users"."id", "users"."full_name", "users"."email" FROM "users" LIMIT ('1') :: integer) AS "users_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "products_1"."id" AS "id", "products_1"."name" AS "name", "products_1"."price" AS "price") AS "json_row_1")) AS "json_1" FROM (SELECT "products"."id", "products"."name", "products"."price" FROM "products" WHERE ((("products"."user_id") = ("users_0"."id"))) LIMIT ('1') :: integer) AS "products_1" LIMIT ('1') :: integer) AS "product_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

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

	resSQL, err := compileGQLToPSQL(gql, vars, "admin")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
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

	sql := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "users" AS (SELECT "id" FROM "_sg_input" i,"users" WHERE "users"."id"= ((i.j->'user'->'connect'->>'id'))::bigint LIMIT 1), "products" AS (INSERT INTO "products" ("name", "price", "created_at", "updated_at", "user_id") SELECT "t"."name", "t"."price", "t"."created_at", "t"."updated_at", "users"."id" FROM "_sg_input" i, "users", json_populate_record(NULL::products, i.j) t RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "user_1_join"."json_1" AS "user", "tags_2_join"."json_2" AS "tags") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name", "products"."user_id", "products"."tags" FROM "products" LIMIT ('1') :: integer) AS "products_0" LEFT OUTER JOIN LATERAL (SELECT coalesce(json_agg("json_2"), '[]') AS "json_2" FROM (SELECT row_to_json((SELECT "json_row_2" FROM (SELECT "tags_2"."id" AS "id", "tags_2"."name" AS "name") AS "json_row_2")) AS "json_2" FROM (SELECT "tags"."id", "tags"."name" FROM "tags" WHERE ((("tags"."slug") = any ("products_0"."tags"))) LIMIT ('20') :: integer) AS "tags_2" LIMIT ('20') :: integer) AS "json_agg_2") AS "tags_2_join" ON ('true') LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "users_1"."id" AS "id", "users_1"."full_name" AS "full_name", "users_1"."email" AS "email") AS "json_row_1")) AS "json_1" FROM (SELECT "users"."id", "users"."full_name", "users"."email" FROM "users" WHERE ((("users"."id") = ("products_0"."user_id"))) LIMIT ('1') :: integer) AS "users_1" LIMIT ('1') :: integer) AS "user_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

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

	resSQL, err := compileGQLToPSQL(gql, vars, "admin")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
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

	sql := `WITH "_sg_input" AS (SELECT '{{data}}' :: json AS j), "users" AS (SELECT "id" FROM "_sg_input" i,"users" WHERE "users"."id" = ANY((select a::bigint AS list from json_array_elements_text((i.j->'user'->'connect'->>'id')::json) AS a)) LIMIT 1), "products" AS (INSERT INTO "products" ("name", "price", "created_at", "updated_at", "user_id") SELECT "t"."name", "t"."price", "t"."created_at", "t"."updated_at", "users"."id" FROM "_sg_input" i, "users", json_populate_record(NULL::products, i.j) t RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "user_1_join"."json_1" AS "user") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name", "products"."user_id" FROM "products" LIMIT ('1') :: integer) AS "products_0" LEFT OUTER JOIN LATERAL (SELECT row_to_json((SELECT "json_row_1" FROM (SELECT "users_1"."id" AS "id", "users_1"."full_name" AS "full_name", "users_1"."email" AS "email") AS "json_row_1")) AS "json_1" FROM (SELECT "users"."id", "users"."full_name", "users"."email" FROM "users" WHERE ((("users"."id") = ("products_0"."user_id"))) LIMIT ('1') :: integer) AS "users_1" LIMIT ('1') :: integer) AS "user_1_join" ON ('true') LIMIT ('1') :: integer) AS "sel_0"`

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

	resSQL, err := compileGQLToPSQL(gql, vars, "admin")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func TestCompileInsert(t *testing.T) {
	t.Run("simpleInsert", simpleInsert)
	t.Run("singleInsert", singleInsert)
	t.Run("bulkInsert", bulkInsert)
	t.Run("simpleInsertWithPresets", simpleInsertWithPresets)
	t.Run("nestedInsertManyToMany", nestedInsertManyToMany)
	t.Run("nestedInsertOneToMany", nestedInsertOneToMany)
	t.Run("nestedInsertOneToOne", nestedInsertOneToOne)
	t.Run("nestedInsertOneToManyWithConnect", nestedInsertOneToManyWithConnect)
	t.Run("nestedInsertOneToOneWithConnect", nestedInsertOneToOneWithConnect)
	t.Run("nestedInsertOneToOneWithConnectArray", nestedInsertOneToOneWithConnectArray)

}
