package psql_test

import "testing"

func TestByteOrderMarkAcrossDialects(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			qc, pc := newDialectCompilers(t, dialect)
			marked := compileWith(t, qc, pc, "\ufeff{ products { id } }")
			plain := compileWith(t, qc, pc, `{ products { id } }`)
			if marked != plain {
				t.Fatalf("BOM changed query\nmarked: %s\nplain: %s", marked, plain)
			}
		})
	}
}
