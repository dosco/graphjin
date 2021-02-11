package psql_test

import (
	"encoding/json"
	"testing"
)

func singleUpsert(t *testing.T) {
	gql := `mutation {
		products(upsert: $upsert, where: { id: { eq: 1} }) {
			id
			name
		}
	}`

	vars := map[string]json.RawMessage{
		"upsert": json.RawMessage(` { "name": "my_name", "description": "my_desc"  }`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func singleUpsertWhere(t *testing.T) {
	gql := `mutation {
		products(upsert: $upsert, where: { price : { gt: 3 } }) {
			id
			name
		}
	}`

	vars := map[string]json.RawMessage{
		"upsert": json.RawMessage(` { "name": "my_name",  "description": "my_desc"  }`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

// func bulkUpsert(t *testing.T) {
// 	gql := `mutation {
// 		product(upsert: $upsert, where: { id: { eq: 1 } }) {
// 			id
// 			name
// 		}
// 	}`

// 	vars := map[string]json.RawMessage{
// 		"upsert": json.RawMessage(` [{ "name": "my_name",  "description": "my_desc"  }]`),
// 	}

// 	compileGQLToPSQL(t, gql, vars, "user")
// }

func delete(t *testing.T) {
	gql := `mutation {
		products(delete: true, where: { id: { eq: 1 } }) {
			id
			name
		}
	}`

	vars := map[string]json.RawMessage{
		"update": json.RawMessage(` { "name": "my_name", "description": "my_desc"  }`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

// func blockedInsert(t *testing.T) {
// 	gql := `mutation {
// 		user(insert: $data) {
// 			id
// 		}
// 	}`

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

// 	sql := `WITH "users" AS (WITH "input" AS (SELECT '$1' :: json AS j) UPDATE "users" SET ("full_name", "email") = (SELECT "full_name", "email" FROM input i, json_populate_record(NULL::users, i.j) t) WHERE false RETURNING *) SELECT json_object_agg('user', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "users_0"."id" AS "id", "users_0"."email" AS "email") AS "json_row_0")) AS "json_0" FROM (SELECT "users"."id", "users"."email" FROM "users" LIMIT ('1') :: integer) AS "users_0" LIMIT ('1') :: integer) AS "sel_0"`

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
	// t.Run("bulkUpsert", bulkUpsert)
	t.Run("delete", delete)
	// t.Run("blockedInsert", blockedInsert)
	// t.Run("blockedUpdate", blockedUpdate)
}
