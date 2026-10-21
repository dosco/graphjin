package psql_test

import "testing"

func TestTypedVariableDefaultAcrossDialects(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			qc, pc := newDialectCompilers(t, dialect)
			query := `query q($minimum: Float = 5.5) { products(where: {price: {gt: $minimum}}) { id } }`
			compiled, err := qc.Compile([]byte(query), nil, "user", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(compiled.Vars) != 1 || compiled.Vars[0].Name != "minimum" || string(compiled.Vars[0].Val) != "5.5" {
				t.Fatalf("default missing: %#v", compiled.Vars)
			}
			if _, _, err = pc.CompileEx(compiled); err != nil {
				t.Fatal(err)
			}
		})
	}
}
