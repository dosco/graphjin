package psql_test

import (
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"testing"
)

func TestRoleFiltersRequireCompleteValuesAcrossDialects(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			for _, filter := range []string{`{ id: { eq: 1 }`, `{ id: { eq: 1 } } { id: { eq: 2 } }`} {
				qc, _ := newDialectCompilers(t, dialect)
				err := qc.AddRole("restricted", "public", "products", qcode.TRConfig{Query: qcode.QueryConfig{Filters: []string{filter}}})
				if err == nil {
					t.Errorf("accepted malformed role filter %q", filter)
				}
			}
			qc, pc := newDialectCompilers(t, dialect)
			if err := qc.AddRole("user", "public", "products", qcode.TRConfig{Query: qcode.QueryConfig{Filters: []string{`{ id: { eq: 1 } }`}}}); err != nil {
				t.Fatal(err)
			}
			compileWith(t, qc, pc, `query { products { id } }`)
		})
	}
}
