package qcode_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

func TestMultiOperatorJSONMutationFilterPaths(t *testing.T) {
	qc, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]json.RawMessage{"data": json.RawMessage(`{
		"full_name":"Updated", "products":{"where":{"id":{"gte":2,"lte":4}},"name":"Updated product"}
	}`)}
	compiled, err := qc.Compile([]byte(`mutation { users(id: 90, update: $data) { id products { id } } }`), vars, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range compiled.Mutates {
		if m.Ti.Name != "products" {
			continue
		}
		var paths []string
		var visit func(*qcode.Exp)
		visit = func(ex *qcode.Exp) {
			if ex == nil {
				return
			}
			if len(ex.Right.Path) > 0 && (ex.Op == qcode.OpGreaterOrEquals || ex.Op == qcode.OpLesserOrEquals) {
				paths = append(paths, strings.Join(ex.Right.Path, "."))
			}
			for _, child := range ex.Children {
				visit(child)
			}
		}
		visit(m.Where.Exp)
		if len(paths) != 2 || paths[0] != "id.lte" || paths[1] != "id.gte" {
			t.Fatalf("JSON filter binding paths = %v, want [id.lte id.gte]", paths)
		}
		return
	}
	t.Fatal("missing related product mutation")
}

func TestMultiOperatorFilterStillValidatesEveryPredicate(t *testing.T) {
	qc, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ query, errorText string }{
		{`{ products(where: { price: { gte: 10, lte: 20 } }) { id } }`, "db column blocked"},
		{`{ products(where: { id: { gte: 10, invented: 20 } }) { id } }`, "unknown operator: invented"},
		{`{ products(where: { id: { gte: 10, neq: null } }) { id } }`, "not a valid null comparison"},
		{`{ products(where: { id: { gte: 10, lte: { max: id } } }) { id } }`, "cannot be embedded"},
	} {
		if _, err := qc.Compile([]byte(tc.query), nil, "user", ""); err == nil || !strings.Contains(err.Error(), tc.errorText) {
			t.Fatalf("query %s: got %v, want %q", tc.query, err, tc.errorText)
		}
	}
}
