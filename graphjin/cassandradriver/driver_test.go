package cassandradriver

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"reflect"
	"testing"

	"github.com/gocql/gocql"
	"gopkg.in/inf.v0"
)

func TestPositionalArgs(t *testing.T) {
	args := []driver.NamedValue{
		{Ordinal: 2, Value: "b"},
		{Ordinal: 1, Value: "a"},
	}
	got := positionalArgs(args)
	if !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Fatalf("positionalArgs reorder failed: %#v", got)
	}
}

func TestNamedValuesRoundTrip(t *testing.T) {
	nv := namedValues([]driver.Value{"a", "b"})
	if len(nv) != 2 || nv[0].Ordinal != 1 || nv[1].Ordinal != 2 {
		t.Fatalf("namedValues bad ordinals: %#v", nv)
	}
}

func TestNormalizeValue(t *testing.T) {
	id, _ := gocql.ParseUUID("550e8400-e29b-41d4-a716-446655440000")
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"uuid", id, "550e8400-e29b-41d4-a716-446655440000"},
		{"inet", net.ParseIP("1.2.3.4"), "1.2.3.4"},
		{"decimal", inf.NewDec(12345, 2), "123.45"},
		{"varint", big.NewInt(99), "99"},
		{"float32", float32(1.5), float64(1.5)},
		{"list", []string{"a", "b"}, []any{"a", "b"}},
		{"nilDec", (*inf.Dec)(nil), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeValue(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normalizeValue(%s) = %#v, want %#v", tc.name, got, tc.want)
			}
		})
	}
}

func TestNormalizeValue_Map(t *testing.T) {
	got := normalizeValue(map[string]int{"a": 1})
	m, ok := got.(map[string]any)
	if !ok || m["a"] != 1 {
		t.Fatalf("normalizeValue map = %#v", got)
	}
}

func TestSingleValueRows(t *testing.T) {
	r := NewSingleValueRows([]byte(`{"x":1}`), []string{"__root"})
	if cols := r.Columns(); len(cols) != 1 || cols[0] != "__root" {
		t.Fatalf("columns: %#v", cols)
	}
	dest := make([]driver.Value, 1)
	if err := r.Next(dest); err != nil {
		t.Fatalf("first Next: %v", err)
	}
	if string(dest[0].([]byte)) != `{"x":1}` {
		t.Fatalf("value: %v", dest[0])
	}
	if err := r.Next(dest); err != io.EOF {
		t.Fatalf("second Next should EOF, got %v", err)
	}
}

func TestColumnRows(t *testing.T) {
	r := NewColumnRows([]string{"a", "b"}, [][]any{{1, "x"}, {2, "y"}})
	dest := make([]driver.Value, 2)
	if err := r.Next(dest); err != nil || dest[0] != 1 || dest[1] != "x" {
		t.Fatalf("row 1: %v %v", dest, err)
	}
	if err := r.Next(dest); err != nil || dest[0] != 2 {
		t.Fatalf("row 2: %v %v", dest, err)
	}
	if err := r.Next(dest); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

// Full Conn → Resolver path through a fake Executor: parse DSL, substitute the
// ordinal param, resolve nested data, return one JSON column.
func TestConnQueryContext_EndToEnd(t *testing.T) {
	exec := &fakeExecutor{rows: map[string][]map[string]any{
		"users": {{"id": "u1", "name": "amit"}},
		"posts": {{"user_id": "u1", "id": "p1", "title": "hi"}},
	}}
	conn := &Conn{exec: exec}

	dsl := `{"operation":"query","root":{
		"table":"users","columns":["id","name"],"partition_keys":["id"],
		"field_name":"users","filters":[{"col":"id","op":"eq","param":"$1"}],
		"children":[{"table":"posts","columns":["user_id","id","title"],
			"partition_keys":["user_id"],"clustering_keys":["id"],"field_name":"posts",
			"rel":{"parent_col":"id","child_col":"user_id"}}]}}`

	rows, err := conn.QueryContext(context.Background(), dsl,
		[]driver.NamedValue{{Ordinal: 1, Value: "u1"}})
	if err != nil {
		t.Fatalf("QueryContext: %v", err)
	}
	dest := make([]driver.Value, 1)
	if err := rows.Next(dest); err != nil {
		t.Fatalf("Next: %v", err)
	}
	var res struct {
		Users []struct {
			Name  string `json:"name"`
			Posts []struct {
				Title string `json:"title"`
			} `json:"posts"`
		} `json:"users"`
	}
	if err := json.Unmarshal(dest[0].([]byte), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(res.Users) != 1 || res.Users[0].Name != "amit" || len(res.Users[0].Posts) != 1 {
		t.Fatalf("unexpected result: %s", dest[0])
	}
}

func TestDriverOpenUnsupported(t *testing.T) {
	if _, err := (&Driver{}).Open("x"); err == nil {
		t.Fatalf("Open should be unsupported")
	}
}
