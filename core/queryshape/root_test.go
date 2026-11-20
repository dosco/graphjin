package queryshape

import (
	"reflect"
	"testing"
)

func TestInspectDescribesTopLevelSelections(t *testing.T) {
	shape, err := Inspect(`{ top: accounts(where: {status: {in: ["active", "churn_risk"]}}, order_by: [{count_id: desc}, {plan: asc}], limit: 1) { plan n: count_id owner { name } } }`)
	if err != nil {
		t.Fatal(err)
	}
	if shape.Operation != "query" || shape.Variables || !reflect.DeepEqual(shape.Literals, []string{"active", "churn_risk"}) || len(shape.Roots) != 1 {
		t.Fatalf("shape = %+v", shape)
	}
	root := shape.Roots[0]
	want := Root{Name: "accounts", Key: "top", Limit: 1, OrderBy: []string{"count_id", "plan"},
		Fields: []Field{{Name: "plan", Key: "plan"}, {Name: "count_id", Key: "n"}}, Nested: true}
	if !reflect.DeepEqual(root, want) {
		t.Fatalf("root = %+v, want %+v", root, want)
	}

	shape, err = Inspect(`query Q($id: Int!) { accounts(where: {id: {eq: $id}}) { name } invoices { count_id } }`)
	if err != nil || !shape.Variables || len(shape.Roots) != 2 || shape.Roots[0].Limit != -1 {
		t.Fatalf("shape = %+v err = %v", shape, err)
	}
	shape, err = Inspect(`mutation { support_tickets(where: {id: {eq: 3}}, update: {status: "closed"}) { id } }`)
	if err != nil || shape.Operation != "mutation" {
		t.Fatalf("shape = %+v err = %v", shape, err)
	}
	if _, err := Inspect(`query { accounts {`); err == nil {
		t.Fatal("a broken query parsed")
	}
}
