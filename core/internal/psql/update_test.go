package psql_test

import (
	"encoding/json"
	"testing"
)

func singleUpdate(t *testing.T) {
	gql := `mutation {
		products(id: $id, update: $update, where: { id: { eq: 1 } }) {
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
		products(update: $data id: $id) {
			id
		}
	}`

	vars := map[string]json.RawMessage{
		"id":   json.RawMessage(`1`),
		"data": json.RawMessage(`{"name": "Apple", "price": 1.25}`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func nestedUpdateManyToMany(t *testing.T) {
	gql := `mutation {
		purchases(update: $data, id: $id) {
			sale_type
			quantity
			due_date
			customer {
				id
				user {
					full_name
					email
				}
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

// func nestedUpdateOneToMany(t *testing.T) {
// 	gql := `mutation {
// 		user(update: $data, where: { id: { eq: 8 } }) {
// 			id
// 			full_name
// 			email
// 			product {
// 				id
// 				name
// 				price
// 			}
// 		}
// 	}`

// 	vars := map[string]json.RawMessage{
// 		"data": json.RawMessage(`{
// 			"email": "thedude@rug.com",
// 			"full_name": "The Dude",
// 			"created_at": "now",
// 			"updated_at": "now",
// 			"product": {
// 				"where": {
// 					"id": 2
// 				},
// 				"name": "Apple",
// 				"price": 1.25,
// 				"created_at": "now",
// 				"updated_at": "now"
// 			}
// 		}`),
// 	}

// 	compileGQLToPSQL(t, gql, vars, "admin")
// }

func nestedUpdateOneToOne(t *testing.T) {
	gql := `mutation {
		products(update: $data, id: $id) {
			id
			name
			users {
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
			"users": {
				"email": "thedude@rug.com"
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")

}

func nestedUpdateOneToManyWithConnect(t *testing.T) {
	gql := `mutation {
		users(update: $data, id: $id) {
			id
			full_name
			email
			products {
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
			"products": {
				"connect": { "id": 7 }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedUpdateOneToOneWithConnect(t *testing.T) {
	gql := `mutation {
		products(update: $data, id: $product_id) {
			id
			name
			users {
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
		products(update: $data, id: $id) {
			id
			name
			user_id
		}
	}`
	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"name": "Apple",
			"price": 1.25,
			"users": {
				"disconnect": { "id": 5 }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedUpdateOneToOneWithDisconnectArray(t *testing.T) {
	gql := `mutation {
		products(update: $data, id: $id) {
			id
			name
			user_id
		}
	}`

	vars := map[string]json.RawMessage{
		"id": json.RawMessage(`1`),
		"data": json.RawMessage(`{
			"name": "Apple",
			"price": 1.25,
			"users": {
				"disconnect": { "id": 5 }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedUpdateRecursive(t *testing.T) {
	gql := `mutation {
		comments(update: $data, id: $id) {
			id
			comments(find: "children") {
				id
				body
			}
		}
	}`

	vars := map[string]json.RawMessage{
		"id": json.RawMessage(`1002`),
		"data": json.RawMessage(`{
			"id": 1002,
			"body": "hello 2",
			"created_at": "now",
			"updated_at": "now",
			"comments": {
				"find": "children",
				"connect":{ "id": 5 }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func TestCompileUpdate(t *testing.T) {
	t.Run("singleUpdate", singleUpdate)
	t.Run("simpleUpdateWithPresets", simpleUpdateWithPresets)
	t.Run("nestedUpdateManyToMany", nestedUpdateManyToMany)
	//t.Run("nestedUpdateOneToMany", nestedUpdateOneToMany)
	t.Run("nestedUpdateOneToOne", nestedUpdateOneToOne)
	t.Run("nestedUpdateOneToManyWithConnect", nestedUpdateOneToManyWithConnect)
	t.Run("nestedUpdateOneToOneWithConnect", nestedUpdateOneToOneWithConnect)
	t.Run("nestedUpdateOneToOneWithDisconnect", nestedUpdateOneToOneWithDisconnect)
	t.Run("nestedUpdateOneToOneWithDisconnectArray", nestedUpdateOneToOneWithDisconnectArray)
	t.Run("nestedUpdateRecursive", nestedUpdateRecursive)

}
