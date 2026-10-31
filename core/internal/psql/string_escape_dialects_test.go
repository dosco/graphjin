package psql_test

import "testing"

// Escaped Unicode must generate the same query as the equivalent source character.
func TestStringEscapesAcrossDialects(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			qc, pc := newDialectCompilers(t, dialect)
			escaped := compileWith(t, qc, pc, `{ products(where: {name: {eq: "\uD83D\uDE00"}}) { id } }`)
			literal := compileWith(t, qc, pc, `{ products(where: {name: {eq: "😀"}}) { id } }`)
			if escaped != literal {
				t.Fatalf("escaped Unicode differs from literal\nescaped: %s\nliteral: %s", escaped, literal)
			}
			compileWith(t, qc, pc, `{ products(where: {name: {eq: "C:\\"}}) { id } }`)
		})
	}
}
