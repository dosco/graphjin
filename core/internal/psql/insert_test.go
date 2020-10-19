package psql_test

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

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`{"email": "reannagreenholt@orn.com", "full_name": "Flo Barton"}`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func singleInsert(t *testing.T) {
	gql := `mutation {
		product(id: $id, insert: $insert) {
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
		product(name: "test", id: $id, insert: $insert) {
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
		product(insert: $data) {
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

	compileGQLToPSQL(t, gql, vars, "admin")
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

	compileGQLToPSQL(t, gql, vars, "admin")
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
		comment(insert: $data) {
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

func TestCompileInsert(t *testing.T) {
	t.Run("simpleInsert", simpleInsert)
	t.Run("singleInsert", singleInsert)
	t.Run("bulkInsert", bulkInsert)
	t.Run("simpleInsertWithPresets", simpleInsertWithPresets)
	t.Run("nestedInsertManyToMany", nestedInsertManyToMany)
	t.Run("nestedInsertOneToMany", nestedInsertOneToMany)
	t.Run("nestedInsertOneToOne", nestedInsertOneToOne)
	t.Run("nestedInsertOneToManyWithConnect", nestedInsertOneToManyWithConnect)
	t.Run("nestedInsertOneToOneWithConnectReverse", nestedInsertOneToOneWithConnectReverse)
	t.Run("nestedInsertOneToOneWithConnect", nestedInsertOneToOneWithConnect)
	t.Run("nestedInsertOneToOneWithConnectArray", nestedInsertOneToOneWithConnectArray)
}
