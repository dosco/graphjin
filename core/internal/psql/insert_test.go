package psql_test

import (
	"encoding/json"
	"testing"
)

func simpleInsert(t *testing.T) {
	gql := `mutation {
		users(insert: $data) {
			id
		}
	}`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{"email": "reannagreenholt@orn.com", "full_name": "Flo Barton"}`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func simpleInlineInsertBulk(t *testing.T) {
	gql := `mutation {
		users(insert: [
			{email: $email1, full_name: $full_name1},
			{email: $email2, full_name: $full_name2}]) {
			id
		}
	}`

	vars := map[string]json.RawMessage{
		"email1":     json.RawMessage(`"one@test.com"`),
		"full_name1": json.RawMessage(`"John One"`),
		"email2":     json.RawMessage(`"two@test.com"`),
		"full_name2": json.RawMessage(`"John Two"`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func singleInsert(t *testing.T) {
	gql := `mutation {
		products(id: $id, insert: $insert) {
			id
			name
		}
	}`

	vars := map[string]json.RawMessage{
		"insert": json.RawMessage(` { "name": "my_name", "price": 6.95, "description": "my_desc", "user_id": 5 }`),
	}

	compileGQLToPSQL(t, gql, vars, "anon")
}

func bulkInsert(t *testing.T) {
	gql := `mutation {
		products(id: $id, insert: $insert) {
			id
			name
		}
	}`

	vars := map[string]json.RawMessage{
		"insert": json.RawMessage(` [{ "name": "my_name", "description": "my_desc"  }]`),
	}

	compileGQLToPSQL(t, gql, vars, "anon")
}

func simpleInsertWithPresets(t *testing.T) {
	gql := `mutation {
		products(insert: $data) {
			id
		}
	}`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{"name": "Tomato", "price": 5.76}`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func nestedInsertManyToMany(t *testing.T) {
	gql := `mutation {
		purchases(insert: $data) {
			sale_type
			quantity
			due_date
			customer {
				id
				vip
				user {
					id
					full_name
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

func nestedInsertOneToMany(t *testing.T) {
	gql := `mutation {
		users(insert: $data) {
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
				"name": "Apple",
				"price": 1.25,
				"created_at": "now",
				"updated_at": "now"
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedInsertOneToOne(t *testing.T) {
	gql := `mutation {
		products(insert: $data) {
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
				"email": "thedude@rug.com",
				"full_name": "The Dude",
				"created_at": "now",
				"updated_at": "now"
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedInsertOneToManyWithConnect(t *testing.T) {
	gql := `mutation {
		users(insert: $data) {
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
				"connect": { "id": 5 }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedInsertOneToOneWithConnect(t *testing.T) {
	gql := `mutation {
		products(insert: $data) {
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

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedInsertOneToOneWithConnectReverse(t *testing.T) {
	gql := `mutation {
		comments(insert: $data) {
			id
			product {
				id
				name
			}
		}
	}`

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{
			"body": "a comment",
			"created_at": "now",
			"updated_at": "now",
			"product": {
				"connect": { "id": 1 }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedInsertOneToOneWithConnectArray(t *testing.T) {
	gql := `mutation {
		products(insert: $data) {
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
				"connect": { "id": [1,2] }
			}
		}`),
	}

	compileGQLToPSQL(t, gql, vars, "admin")
}

func nestedInsertRecursive(t *testing.T) {
	gql := `mutation {
		comments(insert: $data) {
			id
			comments(find: "children") {
				id
				body
			}
		}
	}`

	vars := map[string]json.RawMessage{
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

func TestCompileInsert(t *testing.T) {
	t.Run("simpleInsert", simpleInsert)
	t.Run("singleInsert", singleInsert)
	t.Run("simpleInlineInsertBulk", simpleInlineInsertBulk)
	t.Run("bulkInsert", bulkInsert)
	t.Run("simpleInsertWithPresets", simpleInsertWithPresets)
	t.Run("nestedInsertManyToMany", nestedInsertManyToMany)
	t.Run("nestedInsertOneToMany", nestedInsertOneToMany)
	t.Run("nestedInsertOneToOne", nestedInsertOneToOne)
	t.Run("nestedInsertOneToManyWithConnect", nestedInsertOneToManyWithConnect)
	t.Run("nestedInsertOneToOneWithConnectReverse", nestedInsertOneToOneWithConnectReverse)
	t.Run("nestedInsertOneToOneWithConnect", nestedInsertOneToOneWithConnect)
	t.Run("nestedInsertOneToOneWithConnectArray", nestedInsertOneToOneWithConnectArray)
	t.Run("nestedInsertRecursive", nestedInsertRecursive)
}
