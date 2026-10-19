package graph

import (
	"encoding/json"
	"reflect"
	"testing"
)

func Test_lex_JSON(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		json     string
		jsonItem int
	}{
		{
			name:     "json",
			input:    []byte("{\"id\":1,\"json\":\"{\\\"field1\\\":\\\"value1\\\", \\\"field2\\\":\\\"value2\\\"}\"}"),
			json:     "{\"field1\":\"value1\", \"field2\":\"value2\"}",
			jsonItem: 6,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lex(tt.input)
			if err != nil {
				t.Fatalf("lex() error = %v", err)
			}
			if len(got.items) < tt.jsonItem {
				t.Fatal("lex() invalid items count")
			}
			j := got.items[tt.jsonItem]
			if j.String() != "string" {
				t.Fatal("lex() invalid type")
			}
			mapStructure := map[string]interface{}{}
			err = json.Unmarshal(tt.input, &mapStructure)
			if err != nil {
				t.Fatalf("json.Unmarshal error = %v", err)
			}
			if !reflect.DeepEqual(mapStructure["json"], tt.json) {
				t.Fatalf("lex() JSON = %v, want %v", got, tt.json)
			}
		})
	}
}

// TestLexStringValueUnescapes pins the defect behind DeepORG watch creation.
// A watch's subscription is passed as a string inside a gj_watch mutation, so a
// filter must be written eq: \"failed\". lexString tracked the escape well
// enough to find the closing quote but emitted the raw slice, so the embedded
// subscription reached its parser with literal backslashes and failed with
// `gj_watch subscription probe failed: unrecognized character in action: U+005C`.
func TestLexStringValueUnescapes(t *testing.T) {
	for _, tc := range []struct {
		name, query, want string
	}{{
		name:  "escaped quotes inside an embedded subscription",
		query: `query { watches(where: {q: "subscription s { invoices(where: {status: {eq: \"failed\"}}) { id } }"}) { id } }`,
		want:  `subscription s { invoices(where: {status: {eq: "failed"}}) { id } }`,
	}, {
		name:  "escaped backslash stays one backslash",
		query: `query { a(where: {p: "c:\\tmp"}) { id } }`,
		want:  `c:\tmp`,
	}, {
		name:  "control escapes resolve",
		query: `query { a(where: {p: "one\ttwo\nthree"}) { id } }`,
		want:  "one\ttwo\nthree",
	}, {
		name:  "unicode escape resolves",
		query: `query { a(where: {p: "caf\u00e9"}) { id } }`,
		want:  "café",
	}, {
		name:  "unknown escape is preserved verbatim",
		query: `query { a(where: {p: "keep\\q"}) { id } }`,
		want:  `keep\q`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			l, err := lex([]byte(tc.query))
			if err != nil {
				t.Fatalf("lex: %v", err)
			}
			var got []string
			for _, it := range l.items {
				if it._type == itemStringVal {
					got = append(got, string(it.val))
				}
			}
			if len(got) != 1 {
				t.Fatalf("expected one string value, got %#v", got)
			}
			if got[0] != tc.want {
				t.Fatalf("string value not unescaped\n got: %q\nwant: %q", got[0], tc.want)
			}
		})
	}
}

// TestLexStringValueWithoutEscapesIsNotCopied keeps the common path allocation
// free: a value with no backslash must still alias the input buffer.
func TestLexStringValueWithoutEscapesIsNotCopied(t *testing.T) {
	query := []byte(`query { a(where: {p: "plain"}) { id } }`)
	l, err := lex(query)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range l.items {
		if it._type != itemStringVal {
			continue
		}
		if string(it.val) != "plain" {
			t.Fatalf("unexpected value %q", it.val)
		}
		if &it.val[0] != &query[int(it.pos)] {
			t.Fatal("unescaped value was copied instead of aliasing the input")
		}
	}
}
