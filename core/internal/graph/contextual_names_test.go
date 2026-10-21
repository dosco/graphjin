package graph

import "testing"

func TestKeywordNamesInFieldContexts(t *testing.T) {
	op, err := Parse([]byte(`query on { products { on true false ON True alias: on } }`))
	if err != nil {
		t.Fatal(err)
	}
	if op.Name != "on" {
		t.Fatalf("operation name %q", op.Name)
	}
	for i, want := range []string{"products", "on", "true", "false", "ON", "True", "on"} {
		if op.Fields[i].Name != want {
			t.Fatalf("field %d got %q want %q", i, op.Fields[i].Name, want)
		}
	}
}

func TestKeywordObjectKeysAndValues(t *testing.T) {
	op, err := Parse([]byte(`{ products(where: {on: {eq: on}, true: {eq: true}, false: {eq: false}}) { id } }`))
	if err != nil {
		t.Fatal(err)
	}
	where := op.Fields[0].Args[0].Val
	if where.Children[0].Name != "on" || where.Children[0].Children[0].Type != NodeLabel || where.Children[0].Children[0].Val != "on" {
		t.Fatalf("enum on: %#v", where.Children[0])
	}
	if where.Children[1].Children[0].Type != NodeBool || where.Children[2].Children[0].Type != NodeBool {
		t.Fatal("boolean semantics changed")
	}
}

func TestKeywordNameHandlingPreservesBooleans(t *testing.T) {
	op, err := Parse([]byte(`{ products(where: {available: {eq: TRUE}}) { id } }`))
	if err != nil {
		t.Fatal(err)
	}
	value := op.Fields[0].Args[0].Val.Children[0].Children[0]
	if value.Type != NodeBool || value.Val != "true" {
		t.Fatalf("boolean %#v", value)
	}
	if _, err = Parse([]byte(`fragment on on products { id } query { products { id } }`)); err == nil {
		t.Fatal("accepted reserved fragment name on")
	}
	if _, err = Parse([]byte(`fragment true on products { id } query { products { ...true } }`)); err != nil {
		t.Fatal(err)
	}
}
