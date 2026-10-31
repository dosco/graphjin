package psql_test

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestVariableLiteralDefaultsAreValidJSON(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			for _, tc := range []struct {
				value string
				want  any
			}{
				{`"a\"b\nnext"`, "a\"b\nnext"},
				{`null`, nil},
				{`ACTIVE`, "ACTIVE"},
				{`["a\"b",null,ACTIVE]`, []any{"a\"b", nil, "ACTIVE"}},
			} {
				qc, pc := newDialectCompilers(t, dialect)
				query := `query q($value = ` + tc.value + `) { products { id } }`
				compiled, err := qc.Compile([]byte(query), nil, "user", "")
				if err != nil {
					t.Fatal(err)
				}
				if len(compiled.Vars) != 1 {
					t.Fatalf("defaults: %#v", compiled.Vars)
				}
				var got any
				if err = json.Unmarshal(compiled.Vars[0].Val, &got); err != nil {
					t.Errorf("invalid default JSON %q: %v", compiled.Vars[0].Val, err)
					continue
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Errorf("got %#v want %#v", got, tc.want)
				}
				if _, _, err = pc.CompileEx(compiled); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
