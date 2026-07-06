package serv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestControlStoreRuntimeDoesNotUseDirectSQL(t *testing.T) {
	allowed := map[string]map[string]bool{
		"artifacts.go": {
			"initArtifactsBeforeCore": true,
			"migrateArtifactSchema":   true,
			"ensureSQLColumn":         true,
			"sqliteColumnExists":      true,
		},
		"watches.go": {
			"migrateWatchSchema": true,
		},
	}
	for _, name := range []string{"artifacts.go", "watches.go", "watch_runner.go", "watch_delivery.go", "projection_poll.go"} {
		path := filepath.Join(".", name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "ExecContext", "QueryContext", "QueryRowContext":
					if allowed[name][fn.Name.Name] {
						return true
					}
					pos := fset.Position(sel.Pos())
					t.Fatalf("runtime control-store code must use internal GraphQL, found %s in %s:%d (%s)", sel.Sel.Name, name, pos.Line, fn.Name.Name)
				}
				return true
			})
		}
	}
}
