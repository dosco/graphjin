package psql_test

import (
	"encoding/json"
	"testing"
)

func singleUpdate(t *testing.T) {
	gql := `mutation {
		product(id: $id, update: $update, where: { id: { eq: 1 } }) {
			id
			name
		}
	}`

	vars := map[string]json.RawMessage{
		"update": json.RawMessage(` { "name": "my_name", "description": "my_desc"  }`),
	}

	compileGQLToPSQL(t, gql, vars, "anon")
}

func simpleUpdateWithPresets(t *testing.T) {
	gql := `mutation {
		product(update: $data) {
			id
		}
	}`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{"name": "Apple", "price": 1.25}`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func nestedUpdateManyToMany(t *testing.T) {
	gql := `mutation {
		purchase(update: $data, id: $id) {
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

	compileGQLToPSQL(t, gql, vars, "admin")
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

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedUpdateOneToOne(t *testing.T) {
	gql := `mutation {
		product(update: $data, id: $id) {
			id
			name
			user {
				id
				full_name
				email
			}
		}
	}`

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

	compileGQLToPSQL(t, gql, vars, "admin")

}

func nestedUpdateOneToManyWithConnect(t *testing.T) {
	gql := `mutation {
		user(update: $data, id: $id) {
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

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedUpdateOneToOneWithConnect(t *testing.T) {
	gql := `mutation {
		product(update: $data, id: $product_id) {
			id
			name
			user {
				id
				full_name
				email
			}
		}
	}`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"name": "Apple",
			"price": 1.25,
			"user": {
				"connect": { "id": 5, "email": "test@test.com" }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedUpdateOneToOneWithDisconnect(t *testing.T) {
	gql := `mutation {
		product(update: $data, id: $id) {
			id
			name
			user_id
		}
	}`
	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"name": "Apple",
			"price": 1.25,
			"user": {
				"disconnect": { "id": 5 }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

// func nestedUpdateOneToOneWithDisconnectArray(t *testing.T) {
// 	gql := `mutation {
// 		product(update: $data, id: 2) {
// 			id
// 			name
// 			user_id
// 		}
// 	}`

// 	sql := `WITH "_sg_input" AS (SELECT $1 :: json AS j), "users" AS (SELECT * FROM (VALUES(NULL::bigint)) AS LOOKUP("id")), "products" AS (UPDATE "products" SET ("name", "price", "user_id") = (SELECT "t"."name", "t"."price", "users"."id" FROM "_sg_input" i, "users", json_populate_record(NULL::products, i.j) t) WHERE (("products"."id") = 2) RETURNING "products".*) SELECT json_object_agg('product', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT "products_0"."id" AS "id", "products_0"."name" AS "name", "products_0"."user_id" AS "user_id") AS "json_row_0")) AS "json_0" FROM (SELECT "products"."id", "products"."name", "products"."user_id" FROM "products" LIMIT ('1') :: integer) AS "products_0" LIMIT ('1') :: integer) AS "sel_0"`

// 	vars := map[string]json.RawMessage{
// 		"data": json.RawMessage(`{
// 			"name": "Apple",
// 			"price": 1.25,
// 			"user": {
// 				"disconnect": { "id": 5 }
// 			}
// 		}`),
// 	}

// 	resSQL, err := compileGQLToPSQL(gql, vars, "admin")
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	if string(resSQL) != sql {
// 		t.Fatal(errNotExpected)
// 	}
// }

func TestCompileUpdate(t *testing.T) {
	t.Run("singleUpdate", singleUpdate)
	t.Run("simpleUpdateWithPresets", simpleUpdateWithPresets)
	t.Run("nestedUpdateManyToMany", nestedUpdateManyToMany)
	t.Run("nestedUpdateOneToMany", nestedUpdateOneToMany)
	t.Run("nestedUpdateOneToOne", nestedUpdateOneToOne)
	t.Run("nestedUpdateOneToManyWithConnect", nestedUpdateOneToManyWithConnect)
	t.Run("nestedUpdateOneToOneWithConnect", nestedUpdateOneToOneWithConnect)
	t.Run("nestedUpdateOneToOneWithDisconnect", nestedUpdateOneToOneWithDisconnect)
	//t.Run("nestedUpdateOneToOneWithDisconnectArray", nestedUpdateOneToOneWithDisconnectArray)
}
