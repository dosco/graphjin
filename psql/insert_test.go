package psql

import (
	"encoding/json"
	"fmt"
	"testing"
)

func simpleInsert(t *testing.T) {
	gql := `mutation {
		user(insert: $data) {
			id
		}
	}`

	sql := `WITH "users" AS (WITH "input" AS (SELECT {{data}}::json AS j) INSERT INTO users (full_name, email) SELECT full_name, email FROM input i, json_populate_record(NULL::users, i.j) t  RETURNING *) SELECT json_object_agg('user', sel_json_0) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "user_0"."id" AS "id") AS "sel_0")) AS "sel_json_0" FROM (SELECT "user"."id" FROM "users" AS "user" WHERE ((("user"."id") = {{user_id}})) LIMIT ('1') :: integer) AS "user_0" LIMIT ('1') :: integer) AS "done_1337";`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{"email": "reannagreenholt@orn.com", "full_name": "Flo Barton"}`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(">", string(resSQL))

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

	sql := `WITH product AS (WITH input AS (SELECT {{insert}}::json AS j) INSERT INTO product (name, description) SELECT name, description FROM input i, json_populate_record(NULL::product, i.j) t  RETURNING *) SELECT json_object_agg('product', product) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "product_0"."id" AS "id", "product_0"."name" AS "name") AS "sel_0")) AS "product" FROM (SELECT "product"."id", "product"."name" FROM "products" AS "product" WHERE ((("product"."price") > 0) AND (("product"."price") < 8) AND (("id") = 15)) LIMIT ('1') :: integer) AS "product_0" LIMIT ('1') :: integer) AS "done_1337";`

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

	sql := `WITH product AS (WITH input AS (SELECT {{insert}}::json AS j) INSERT INTO product (name, description) SELECT name, description FROM input i, json_populate_recordset(NULL::product, i.j) t  RETURNING *) SELECT json_object_agg('product', product) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "product_0"."id" AS "id", "product_0"."name" AS "name") AS "sel_0")) AS "product" FROM (SELECT "product"."id", "product"."name" FROM "products" AS "product" WHERE ((("product"."price") > 0) AND (("product"."price") < 8) AND (("id") = 15)) LIMIT ('1') :: integer) AS "product_0" LIMIT ('1') :: integer) AS "done_1337";`

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

	sql := `WITH product AS (WITH input AS (SELECT {{update}}::json AS j) UPDATE product SET (name, description) = (SELECT name, description FROM input i, json_populate_record(NULL::product, i.j) t) WHERE (("product"."price") > 0) AND (("product"."price") < 8) AND (("product"."id") = 1) AND (("id") = 15) RETURNING *) SELECT json_object_agg('product', product) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "product_0"."id" AS "id", "product_0"."name" AS "name") AS "sel_0")) AS "product" FROM (SELECT "product"."id", "product"."name" FROM "products" AS "product" WHERE ((("product"."price") > 0) AND (("product"."price") < 8) AND (("product"."id") = 1) AND (("id") = 15)) LIMIT ('1') :: integer) AS "product_0" LIMIT ('1') :: integer) AS "done_1337";`

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

	sql := `DELETE FROM product WHERE (("product"."price") > 0) AND (("product"."price") < 8) AND (("product"."id") = 1) RETURNING *) SELECT json_object_agg('product', product) FROM (SELECT row_to_json((SELECT "sel_0" FROM (SELECT "product_0"."id" AS "id", "product_0"."name" AS "name") AS "sel_0")) AS "product" FROM (SELECT "product"."id", "product"."name" FROM "products" AS "product" WHERE ((("product"."price") > 0) AND (("product"."price") < 8) AND (("product"."id") = 1)) LIMIT ('1') :: integer) AS "product_0" LIMIT ('1') :: integer) AS "done_1337";`

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
