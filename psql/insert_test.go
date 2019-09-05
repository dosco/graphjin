package psql

import (
	"encoding/json"
	"fmt"
	"testing"
)

func singleInsert(t *testing.T) {
	gql := `mutation {
		product(id: 15, insert: $insert) {
			id
			name
		}
	}`

	sql := `test`

	vars := map[string]json.RawMessage{
		"insert": json.RawMessage(` { "name": "my_name", "woo": { "hoo": "goo" }, "description": "my_desc"  }`),
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

func bulkInsert(t *testing.T) {
	gql := `mutation {
		product(id: 15, insert: $insert) {
			id
			name
		}
	}`

	sql := `test`

	vars := map[string]json.RawMessage{
		"insert": json.RawMessage(` [{ "name": "my_name", "woo": { "hoo": "goo" }, "description": "my_desc"  }]`),
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

func TestCompileInsert(t *testing.T) {
	t.Run("singleInsert", singleInsert)
	t.Run("bulkInsert", bulkInsert)

}
