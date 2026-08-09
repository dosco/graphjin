package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"testing"
)

func TestResponseEvidenceHelpersAcceptBareAndWrappedProtocolShapes(t *testing.T) {
	protocol := map[string]any{
		"catalog_detail_ids": []any{"table:app:public.orders", "saved_query:late_orders"},
		"violations": []any{
			map[string]any{"code": "catalog_detail_required"},
			map[string]any{"code": "mutation_evidence_required"},
		},
	}
	for _, tc := range []struct {
		name     string
		evidence any
	}{
		{name: "bare", evidence: protocol},
		{name: "wrapped", evidence: map[string]any{"protocol": protocol, "model": map[string]any{"note": "advisory"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := Response{Evidence: tc.evidence}
			if got, want := CatalogDetailIDs(resp), []string{"table:app:public.orders", "saved_query:late_orders"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("CatalogDetailIDs = %v, want %v", got, want)
			}
			if got, want := ProtocolViolationCodes(resp), []string{"catalog_detail_required", "mutation_evidence_required"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("ProtocolViolationCodes = %v, want %v", got, want)
			}
		})
	}
}

func TestResponseEvidenceHelpersIgnoreMalformedValues(t *testing.T) {
	resp := Response{Evidence: map[string]any{
		"catalog_detail_ids": "not-a-list",
		"violations":         map[string]any{"code": "not-a-list"},
	}}
	if got := CatalogDetailIDs(resp); got != nil {
		t.Fatalf("CatalogDetailIDs malformed = %v, want nil", got)
	}
	if got := ProtocolViolationCodes(resp); got != nil {
		t.Fatalf("ProtocolViolationCodes malformed = %v, want nil", got)
	}
}

func TestBlockingGuardRegistryCoversProtocolEmitters(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "protocol.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var unclassified []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 4 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "addViolation" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			unclassified = append(unclassified, "<non-literal guard code>")
			return true
		}
		code, err := strconv.Unquote(literal.Value)
		if err != nil {
			unclassified = append(unclassified, literal.Value)
			return true
		}
		blocking, ok := call.Args[3].(*ast.Ident)
		if !ok || (blocking.Name != "true" && blocking.Name != "false") {
			unclassified = append(unclassified, code+"=<non-literal classification>")
			return true
		}
		registered := IsBlockingGuardViolationCode(code)
		if (blocking.Name == "true") != registered {
			unclassified = append(unclassified, code+"="+blocking.Name)
		}
		return true
	})
	if len(unclassified) != 0 {
		t.Fatalf("protocol guard classifications drifted from BlockingGuardViolationCodes: %v", unclassified)
	}
}
