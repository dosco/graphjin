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

	sql := `WITH "users" AS (WITH "input" AS (SELECT {{data}}::json AS j) INSERT INTO users (full_name, email) SELECT full_name, email FROM input i, json_populate_record(NULL::users, i.j) t  RETURNING *) SELECT json_object_agg('user', sel_json_0) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "users_0"."id" AS "id") AS "sel_0")) AS "sel_json_0" FROM (SELECT "users"."id" FROM "users") AS "users_0") AS "done_1337";`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{"email": "reannagreenholt@orn.com", "full_name": "Flo Barton"}`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars)
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

	sql := `WITH "products" AS (WITH "input" AS (SELECT {{insert}}::json AS j) INSERT INTO products (name, description) SELECT name, description FROM input i, json_populate_record(NULL::products, i.j) t  RETURNING *) SELECT json_object_agg('product', sel_json_0) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."id", "products"."name" FROM "products") AS "products_0") AS "done_1337";`

	vars := map[string]json.RawMessage{
		"insert": json.RawMessage(` { "name": "my_name", "woo": { "hoo": "goo" }, "description": "my_desc"  }`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars)
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func bulkInsert(t *testing.T) {
	gql := `mutation {
		product(id: 15, insert: $insert) {
			id
			name
		}
	}`

	sql := `WITH "products" AS (WITH "input" AS (SELECT {{insert}}::json AS j) INSERT INTO products (name, description) SELECT name, description FROM input i, json_populate_recordset(NULL::products, i.j) t  RETURNING *) SELECT json_object_agg('product', sel_json_0) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."id", "products"."name" FROM "products") AS "products_0") AS "done_1337";`

	vars := map[string]json.RawMessage{
		"insert": json.RawMessage(` [{ "name": "my_name", "woo": { "hoo": "goo" }, "description": "my_desc"  }]`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars)
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func singleUpdate(t *testing.T) {
	gql := `mutation {
		product(id: 15, update: $update, where: { id: { eq: 1 } }) {
			id
			name
		}
	}`

	sql := `WITH "products" AS (WITH "input" AS (SELECT {{update}}::json AS j) UPDATE products SET (name, description) = (SELECT name, description FROM input i, json_populate_record(NULL::products, i.j) t) WHERE (("products"."price") > 0) AND (("products"."price") < 8) AND (("products"."id") = 1) AND (("products"."id") = 15) RETURNING *) SELECT json_object_agg('product', sel_json_0) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."id", "products"."name" FROM "products") AS "products_0") AS "done_1337";`

	vars := map[string]json.RawMessage{
		"update": json.RawMessage(` { "name": "my_name", "woo": { "hoo": "goo" }, "description": "my_desc"  }`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars)
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func delete(t *testing.T) {
	gql := `mutation {
		product(delete: true, where: { id: { eq: 1 } }) {
			id
			name
		}
	}`

	sql := `WITH "products" AS (DELETE FROM products WHERE (("products"."price") > 0) AND (("products"."price") < 8) AND (("products"."id") = 1) RETURNING *) SELECT json_object_agg('product', sel_json_0) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "sel_0")) AS "sel_json_0" FROM (SELECT "products"."id", "products"."name" FROM "products") AS "products_0") AS "done_1337";`

	vars := map[string]json.RawMessage{
		"update": json.RawMessage(` { "name": "my_name", "woo": { "hoo": "goo" }, "description": "my_desc"  }`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars)
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
	t.Run("singleUpdate", singleUpdate)
	t.Run("delete", delete)
}
