package clickhousedriver

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEncodeNormalizeCursorRoundtrip(t *testing.T) {
	ks := &Keyset{SelID: 12, Prefix: "gj-abc:", Columns: []OrderBy{
		{Col: "price", Order: "desc"}, {Col: "id", Order: "asc"},
	}}
	tok := encodeCursor(ks, map[string]any{"price": float64(10.5), "id": int32(7)})
	if tok != "gj-abc:c:10.5:7" { // 12 == 0xc
		t.Errorf("encode = %q, want gj-abc:c:10.5:7", tok)
	}
	if got := normalizeCursor(ks.Prefix, tok); got != "c:10.5:7" {
		t.Errorf("normalize = %q, want c:10.5:7", got)
	}
	// prefix-less and gj- fallback both reduce to the payload.
	if got := normalizeCursor("", "0:5"); got != "0:5" {
		t.Errorf("normalize(no prefix) = %q", got)
	}
	if got := normalizeCursor("", "gj-zz:3:9"); got != "3:9" {
		t.Errorf("normalize(gj- fallback) = %q", got)
	}
}

func TestBuildSeekClauseTwoCol(t *testing.T) {
	ks := &Keyset{
		Columns:        []OrderBy{{Col: "price", Order: "desc"}, {Col: "id", Order: "asc"}},
		resolvedCursor: "0:10.5:7",
	}
	var args []any
	clause, ok := buildSeekClause(ks, &args)
	if !ok {
		t.Fatal("expected a seek clause")
	}
	want := "(`price` < ? OR (`price` = ? AND `id` > ?))"
	if clause != want {
		t.Errorf("clause = %q, want %q", clause, want)
	}
	if !reflect.DeepEqual(args, []any{float64(10.5), float64(10.5), int64(7)}) {
		t.Errorf("args = %v", args)
	}
}

func TestBuildSeekClauseBackwardFlipsOp(t *testing.T) {
	ks := &Keyset{Columns: []OrderBy{{Col: "id", Order: "asc"}}, Backward: true, resolvedCursor: "0:5"}
	var args []any
	clause, _ := buildSeekClause(ks, &args)
	if clause != "(`id` < ?)" { // asc flipped to < when paging backward
		t.Errorf("clause = %q, want (`id` < ?)", clause)
	}
}

func TestBuildSeekClauseEmptyCursor(t *testing.T) {
	ks := &Keyset{Columns: []OrderBy{{Col: "id", Order: "asc"}}, resolvedCursor: ""}
	var args []any
	if _, ok := buildSeekClause(ks, &args); ok {
		t.Error("empty cursor should produce no seek (first page)")
	}
}

func TestBuildSelectWithKeyset(t *testing.T) {
	n := &Node{
		Table:   "products",
		Columns: []string{"id"},
		Filters: []Filter{{Col: "id", Op: OpLte, Value: int64(6)}},
		OrderBy: []OrderBy{{Col: "id", Order: "asc"}},
		Limit:   2,
		Keyset:  &Keyset{Columns: []OrderBy{{Col: "id", Order: "asc"}}, resolvedCursor: "0:2"},
	}
	got, args, err := BuildSelect(n, "shop", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT `id` FROM `shop`.`products` WHERE `id` <= ? AND (`id` > ?) ORDER BY `id` ASC LIMIT 2"
	if got != want {
		t.Errorf("sql = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(args, []any{int64(6), int64(2)}) {
		t.Errorf("args = %v", args)
	}
}

func TestMarshalRootEmitsCursor(t *testing.T) {
	n := &Node{
		FieldName: "products",
		Keyset:    &Keyset{Prefix: "gj-x:", Columns: []OrderBy{{Col: "id", Order: "asc"}}},
	}
	data, err := marshalRoot(n, []map[string]any{{"id": int32(1)}, {"id": int32(2)}})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if string(out["products_cursor"]) != `"gj-x:0:2"` { // last row id=2, selID 0
		t.Errorf("products_cursor = %s, want \"gj-x:0:2\"", out["products_cursor"])
	}
}
