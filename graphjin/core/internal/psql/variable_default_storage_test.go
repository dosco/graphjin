package psql_test

import (
	"testing"
)

func TestVariableDefaultsOwnTheirJSONAcrossDialects(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			qc, pc := newDialectCompilers(t, dialect)
			query := `query q($first = 12, $second = 99) { products(where: {id: {eq: $first}, price: {gt: $second}}) { id } }`
			compiled, err := qc.Compile([]byte(query), nil, "user", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(compiled.Vars) != 2 {
				t.Fatalf("defaults: %#v", compiled.Vars)
			}
			if string(compiled.Vars[0].Val) != "12" || string(compiled.Vars[1].Val) != "99" {
				t.Fatalf("defaults alias: %s, %s", compiled.Vars[0].Val, compiled.Vars[1].Val)
			}
			if _, _, err = pc.CompileEx(compiled); err != nil {
				t.Fatal(err)
			}
		})
	}
}
