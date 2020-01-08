package psql

import (
	"encoding/json"
	"testing"
)

func singleUpsert(t *testing.T) {
	gql := `mutation {
		product(upsert: $upsert) {
			id
			name
		}
	}`

	sql := `WITH "_sg_input" AS (SELECT '{{upsert}}' :: json AS j), "products" AS (INSERT INTO "products" ("name", "description") SELECT "t"."name", "t"."description" FROM "_sg_input" i, json_populate_record(NULL::products, i.j) t RETURNING *)  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name" FROM "products" LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"upsert": json.RawMessage(` { "name": "my_name", "description": "my_desc"  }`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func singleUpsertWhere(t *testing.T) {
	gql := `mutation {
		product(upsert: $upsert, where: { price : { gt: 3 } }) {
			id
			name
		}
	}`

	sql := `WITH "_sg_input" AS (SELECT '{{upsert}}' :: json AS j), "products" AS (INSERT INTO "products" ("name", "description") SELECT "t"."name", "t"."description" FROM "_sg_input" i, json_populate_record(NULL::products, i.j) t RETURNING *)  ON CONFLICT (id) WHERE (("products"."price") > 3) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name" FROM "products" LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"upsert": json.RawMessage(` { "name": "my_name",  "description": "my_desc"  }`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func bulkUpsert(t *testing.T) {
	gql := `mutation {
		product(upsert: $upsert) {
			id
			name
		}
	}`

	sql := `WITH "_sg_input" AS (SELECT '{{upsert}}' :: json AS j), "products" AS (INSERT INTO "products" ("name", "description") SELECT "t"."name", "t"."description" FROM "_sg_input" i, json_populate_recordset(NULL::products, i.j) t RETURNING *)  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description RETURNING *) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name" FROM "products" LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"upsert": json.RawMessage(` [{ "name": "my_name",  "description": "my_desc"  }]`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "user")
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

	sql := `WITH "products" AS (DELETE FROM "products" WHERE (((("products"."price") > 0) AND (("products"."price") < 8)) AND (("products"."id") IS NOT DISTINCT FROM 1)) RETURNING "products".*)SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name" FROM "products" LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "sel_0"`

	vars := map[string]json.RawMessage{
		"update": json.RawMessage(` { "name": "my_name", "description": "my_desc"  }`),
	}

	resSQL, err := compileGQLToPSQL(gql, vars, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

// func blockedInsert(t *testing.T) {
// 	gql := `mutation {
// 		user(insert: $data) {
// 			id
// 		}
// 	}`

// 	sql := `WITH "users" AS (WITH "input" AS (SELECT '{{data}}' :: json AS j) INSERT INTO "users" ("full_name", "email") SELECT "full_name", "email" FROM input i, json_populate_record(NULL::users, i.j) t WHERE false RETURNING *) SELECT json_object_agg('user', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "users_0"."id" AS "id") AS "json_row_0")) AS "json_0" FROM (SELECT "users"."id" FROM "users" LIMIT ('1') :: integer) AS "users_0" LIMIT ('1') :: integer) AS "sel_0"`

// 	vars := map[string]json.RawMessage{
// 		"data": json.RawMessage(`{"email": "reannagreenholt@orn.com", "full_name": "Flo Barton"}`),
// 	}

// 	resSQL, err := compileGQLToPSQL(gql, vars, "bad_dude")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	fmt.Println(string(resSQL))

// 	if string(resSQL) != sql {
// 		t.Fatal(errNotExpected)
// 	}
// }

// func blockedUpdate(t *testing.T) {
// 	gql := `mutation {
// 		user(where: { id: { lt: 5 } }, update: $data) {
// 			id
// 			email
// 		}
// 	}`

// 	sql := `WITH "users" AS (WITH "input" AS (SELECT '{{data}}' :: json AS j) UPDATE "users" SET ("full_name", "email") = (SELECT "full_name", "email" FROM input i, json_populate_record(NULL::users, i.j) t) WHERE false RETURNING *) SELECT json_object_agg('user', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "users_0"."id" AS "id", "users_0"."email" AS "email") AS "json_row_0")) AS "json_0" FROM (SELECT "users"."id", "users"."email" FROM "users" LIMIT ('1') :: integer) AS "users_0" LIMIT ('1') :: integer) AS "sel_0"`

// 	vars := map[string]json.RawMessage{
// 		"data": json.RawMessage(`{"email": "reannagreenholt@orn.com", "full_name": "Flo Barton"}`),
// 	}

// 	resSQL, err := compileGQLToPSQL(gql, vars, "bad_dude")
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	if string(resSQL) != sql {
// 		t.Fatal(errNotExpected)
// 	}
// }

func TestCompileMutate(t *testing.T) {
	t.Run("singleUpsert", singleUpsert)
	t.Run("singleUpsertWhere", singleUpsertWhere)
	t.Run("bulkUpsert", bulkUpsert)
	t.Run("delete", delete)
	// t.Run("blockedInsert", blockedInsert)
	// t.Run("blockedUpdate", blockedUpdate)
}
