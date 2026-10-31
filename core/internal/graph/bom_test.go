package graph

import "testing"

func TestParseIgnoresByteOrderMark(t *testing.T) {
	for _, query := range []string{"\ufeff{ users { id } }", "{\ufeffusers\ufeff{ id } }"} {
		op, err := Parse([]byte(query))
		if err != nil {
			t.Fatal(err)
		}
		if op.Fields[0].Name != "users" || op.Fields[1].Name != "id" {
			t.Fatalf("unexpected fields %#v", op.Fields)
		}
	}
	op, err := Parse([]byte("{ users(where: {name: {eq: \"a\ufeffb\"}}) { id } }"))
	if err != nil {
		t.Fatal(err)
	}
	if got := op.Fields[0].Args[0].Val.Children[0].Children[0].Val; got != "a\ufeffb" {
		t.Fatalf("string BOM lost: %q", got)
	}
}
