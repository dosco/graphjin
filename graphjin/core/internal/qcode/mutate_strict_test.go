package qcode_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// Unknown SCALAR keys in mutation input used to be silently dropped while
// unknown object keys errored. A benchmark episode inserted a payment with
// created_at (the real column is recorded_at): the key vanished, the insert
// proceeded, and the failure surfaced two steps later as a NOT NULL
// constraint the model could not map back to its mistake. Unknown scalar
// keys now error at compile with the column named, exactly like object keys.

func strictMutationCompiler(t *testing.T) *qcode.Compiler {
	t.Helper()
	qc, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"products", "comments"} {
		if err := qc.AddRole("user", "public", table, qcode.TRConfig{
			Insert: qcode.InsertConfig{},
			Update: qcode.UpdateConfig{},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return qc
}

func TestMutationUnknownScalarColumnErrors(t *testing.T) {
	qc := strictMutationCompiler(t)

	// Inline insert input.
	_, err := qc.Compile([]byte(`mutation { products(insert: { id: 1, name: "a", zzz_missing: "x" }) { id } }`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "zzz_missing") {
		t.Fatalf("unknown scalar insert key must error naming the column, got: %v", err)
	}

	// Inline update input.
	_, err = qc.Compile([]byte(`mutation { products(where: { id: { eq: 1 } }, update: { pricee: 10 }) { id } }`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "pricee") {
		t.Fatalf("unknown scalar update key must error naming the column, got: %v", err)
	}

	// Variable-bound insert data flows through the same map.
	vars := map[string]json.RawMessage{"data": json.RawMessage(`{"id": 1, "name": "a", "zzz_missing": "x"}`)}
	_, err = qc.Compile([]byte(`mutation ($data: json) { products(insert: $data) { id } }`), vars, "user", "")
	if err == nil || !strings.Contains(err.Error(), "zzz_missing") {
		t.Fatalf("unknown scalar key in variable data must error, got: %v", err)
	}

	// A valid insert still compiles.
	if _, err := qc.Compile([]byte(`mutation { products(insert: { id: 1, name: "a", price: 10 }) { id } }`), nil, "user", ""); err != nil {
		t.Fatalf("valid insert regressed: %v", err)
	}
}

func TestMutationKeywordAndRelationshipKeysStillCompile(t *testing.T) {
	qc := strictMutationCompiler(t)

	// The scalar `find` keyword steers recursive relationship inserts and must
	// never be mistaken for a column.
	if _, err := qc.Compile([]byte(`mutation { comments(insert: { id: 1002, body: "hi", comments: { find: "children", id: 1003, body: "child" } }) { id } }`), nil, "user", ""); err != nil {
		t.Fatalf("recursive find insert regressed: %v", err)
	}

	// Object-valued relationship keys keep flowing to the nested compiler.
	if _, err := qc.Compile([]byte(`mutation { products(insert: { id: 2, name: "b", price: 5, user: { connect: { id: 4 } } }) { id } }`), nil, "user", ""); err != nil {
		t.Fatalf("nested connect regressed: %v", err)
	}

	// Unknown OBJECT keys keep their historical rejection.
	if _, err := qc.Compile([]byte(`mutation { products(insert: { id: 3, name: "c", price: 5, wormhole: { id: 9 } }) { id } }`), nil, "user", ""); err == nil {
		t.Fatal("unknown object key must still error")
	}
}
