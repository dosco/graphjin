//go:build gofuzz
// +build gofuzz

package psql

import (
	"encoding/json"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

var (
	qcompileTest, _ = qcode.NewCompiler(qcode.Config{})

	schema, _ = GetTestSchema()

	vars = map[string]string{
		"admin_account_id": "5",
	}

	pcompileTest = NewCompiler(Config{
		Schema: schema,
		Vars:   vars,
	})
)

// FuzzerEntrypoint for Fuzzbuzz
func Fuzz(data []byte) int {
	err1 := query(data)
	err2 := insert(data)
	err3 := update(data)
	err4 := delete(data)

	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return 0
	}

	return 1
}

func query(data []byte) error {
	gql := data

	qc, err1 := qcompileTest.Compile(gql, "user")

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(data),
	}

	_, _, err2 := pcompileTest.CompileEx(qc, vars)

	if err1 != nil {
		return err1
	} else {
		return err2
	}
}

func insert(data []byte) error {
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

	qc, err := qcompileTest.Compile([]byte(gql), "user")
	if err != nil {
		panic("qcompile can't fail")
	}

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(data),
	}

	_, _, err = pcompileTest.CompileEx(qc, vars)
	return err
}

func update(data []byte) error {
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

	qc, err := qcompileTest.Compile([]byte(gql), "user")
	if err != nil {
		panic("qcompile can't fail")
	}

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(data),
	}

	_, _, err = pcompileTest.CompileEx(qc, vars)
	return err
}

func delete(data []byte) error {
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

	qc, err := qcompileTest.Compile([]byte(gql), "user")
	if err != nil {
		panic("qcompile can't fail")
	}

	vars := map[string]json.RawMessage{
		"data": json.RawMessage(data),
	}

	_, _, err = pcompileTest.CompileEx(qc, vars)
	return err
}
