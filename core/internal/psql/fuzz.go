// +build gofuzz

package psql

import (
	"encoding/json"

	"github.com/dosco/super-graph/core/internal/qcode"
)

var (
	qcompileTest, _ = qcode.NewCompiler(qcode.Config{})

	schema = GetTestSchema()

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
	if err != nil {
		return 0
	}

	return 1
}
