package graph

import "testing"

func TestTypedVariableDefaults(t *testing.T) {
	for _, definition := range []string{"$count: Int = 5", "$count: Int! = 5", "$count = 5"} {
		op, err := Parse([]byte(`query q(` + definition + `) { products(limit: $count) { id } }`))
		if err != nil {
			t.Fatal(err)
		}
		if len(op.VarDef) != 1 || op.VarDef[0].Name != "count" || op.VarDef[0].Val.Val != "5" {
			t.Fatalf("%s: defaults %#v", definition, op.VarDef)
		}
	}
	op, err := Parse([]byte(`query q($unused: ID, $ids: [Int!]! = [1, 2]) { products { id } }`))
	if err != nil {
		t.Fatal(err)
	}
	if len(op.VarDef) != 1 || op.VarDef[0].Name != "ids" || len(op.VarDef[0].Val.Children) != 2 {
		t.Fatalf("defaults %#v", op.VarDef)
	}
}
