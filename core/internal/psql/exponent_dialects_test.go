package psql_test

import (
	"strings"
	"testing"
)

func TestExponentLiteralsAcrossDialects(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			qc, pc := newDialectCompilers(t, dialect)
			for _, number := range []string{"1e3", "1.25e-3", "-2E+4"} {
				sql := compileWith(t, qc, pc, `{ products(where: {price: {gt: `+number+`}}) { id } }`)
				if !strings.Contains(sql, number) {
					t.Fatalf("number %s missing in SQL %s", number, sql)
				}
			}
		})
	}
}
