package psql_test

import "testing"

func TestFragmentDefinitionOrderAcrossDialects(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			qc, pc := newDialectCompilers(t, dialect)
			forward := compileWith(t, qc, pc, `fragment A on products { id ...B } fragment B on products { name } query { products { ...A } }`)
			backward := compileWith(t, qc, pc, `fragment B on products { name } fragment A on products { id ...B } query { products { ...A } }`)
			inline := compileWith(t, qc, pc, `query { products { id name } }`)
			if forward != backward || forward != inline {
				t.Fatalf("definition order changed query\nforward:%s\nbackward:%s\ninline:%s", forward, backward, inline)
			}
		})
	}
}
