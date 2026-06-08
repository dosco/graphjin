package clickhousedriver

import (
	"reflect"
	"testing"
)

func TestBuildInsert(t *testing.T) {
	m := &Mutation{Table: "products", Set: []Assignment{
		{Col: "id", Value: int64(1)}, {Col: "name", Value: "X"},
	}}
	got, args, err := BuildInsert(m, "shop")
	if err != nil {
		t.Fatal(err)
	}
	want := "INSERT INTO `shop`.`products` (`id`, `name`) VALUES (?, ?)"
	if got != want {
		t.Errorf("sql = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(args, []any{int64(1), "X"}) {
		t.Errorf("args = %v", args)
	}
}

func TestBuildUpdate(t *testing.T) {
	m := &Mutation{
		Table: "products",
		Set:   []Assignment{{Col: "name", Value: "X"}},
		Where: []Filter{{Col: "id", Op: OpEq, Value: int64(1)}},
	}
	got, args, err := BuildUpdate(m, "shop")
	if err != nil {
		t.Fatal(err)
	}
	want := "ALTER TABLE `shop`.`products` UPDATE `name` = ? WHERE `id` = ? SETTINGS mutations_sync = 1"
	if got != want {
		t.Errorf("sql = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(args, []any{"X", int64(1)}) {
		t.Errorf("args = %v", args)
	}
}

func TestBuildDelete(t *testing.T) {
	m := &Mutation{Table: "products", Where: []Filter{{Col: "id", Op: OpEq, Value: int64(1)}}}
	got, _, err := BuildDelete(m, "shop")
	if err != nil {
		t.Fatal(err)
	}
	want := "DELETE FROM `shop`.`products` WHERE `id` = ?"
	if got != want {
		t.Errorf("sql = %q, want %q", got, want)
	}
}

func TestUpdateDeleteRequireWhere(t *testing.T) {
	if _, _, err := BuildUpdate(&Mutation{Table: "t", Set: []Assignment{{Col: "a", Value: 1}}}, ""); err == nil {
		t.Error("update without where should error")
	}
	if _, _, err := BuildDelete(&Mutation{Table: "t"}, ""); err == nil {
		t.Error("delete without where should error")
	}
}

func TestApplyRawDocInsert(t *testing.T) {
	m := &Mutation{
		Type:        OpInsert,
		PrimaryKey:  "id",
		ColumnTypes: map[string]string{"id": "integer", "price": "double precision", "name": "text"},
		rawDoc:      map[string]any{"id": float64(7), "name": "X", "price": float64(9.5)},
		Returning:   &Node{Table: "products"},
	}
	m.applyRawDoc()
	// id coerced float64→int64; price stays float64; returning filtered by PK.
	vals := map[string]any{}
	for _, a := range m.Set {
		vals[a.Col] = a.Value
	}
	if vals["id"] != int64(7) || vals["price"] != 9.5 || vals["name"] != "X" {
		t.Errorf("set = %v", vals)
	}
	if len(m.Returning.Filters) != 1 || m.Returning.Filters[0].Col != "id" || m.Returning.Filters[0].Value != int64(7) {
		t.Errorf("returning filters = %v", m.Returning.Filters)
	}
}
