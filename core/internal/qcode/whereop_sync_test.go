package qcode

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/dosco/graphjin/core/v3/whereop"
)

// TestWhereOpAliasesMatchCompiler keeps core/whereop, which the agent and MCP
// validators use to answer "which operators can I use here", in step with the
// operators the compiler actually accepts.
func TestWhereOpAliasesMatchCompiler(t *testing.T) {
	ex := &aexpst{}
	for _, op := range whereop.Aliases() {
		if ok, _ := ex.isOperator(op); !ok {
			t.Errorf("whereop knows %q but the compiler does not accept it", op)
		}
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "exp.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "isOperator" {
			return true
		}
		found = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				lit, ok := e.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				op, _ := strconv.Unquote(lit.Value)
				if whereop.Canonical(op) == "" {
					t.Errorf("the compiler accepts %q but whereop does not know it", op)
				}
			}
			return true
		})
		return false
	})
	if !found {
		t.Fatal("isOperator not found in exp.go")
	}
}
