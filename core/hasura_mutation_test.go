package core

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// GraphJin returns written rows directly under the root; Hasura nests them
// under returning and reports affected_rows beside them. These pin the
// restoration, including that raw JSON leaves survive untouched — a payment
// amount must not round-trip through float64.
func TestReshapeHasuraMutationData(t *testing.T) {
	data := []byte(`{
		"update_products":[{"id":1,"price":12345678901234567890},{"id":2,"price":2}],
		"untouched":[{"id":9}]
	}`)
	plans := []qcode.HasuraMutationRoot{{ResponseKey: "update_products", Returning: true, AffectedRows: true}}
	got, err := reshapeHasuraMutationData(data, plans)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{
		"update_products":{"returning":[{"id":1,"price":12345678901234567890},{"id":2,"price":2}],"affected_rows":2},
		"untouched":[{"id":9}]
	}`)
	assertJSONEqual(t, got, want)
}

func TestReshapeHasuraMutationAffectedRowsOnly(t *testing.T) {
	data := []byte(`{"update_products":[{"id":1},{"id":2},{"id":3}]}`)
	plans := []qcode.HasuraMutationRoot{{ResponseKey: "update_products", AffectedRows: true}}
	got, err := reshapeHasuraMutationData(data, plans)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, got, []byte(`{"update_products":{"affected_rows":3}}`))
}

// A _by_pk root addresses one row, and Hasura returns one object rather than
// a list.
func TestReshapeHasuraMutationByPKReturnsAnObject(t *testing.T) {
	data := []byte(`{"update_products_by_pk":[{"id":3,"name":"a"}]}`)
	plans := []qcode.HasuraMutationRoot{{ResponseKey: "update_products_by_pk", Single: true}}
	got, err := reshapeHasuraMutationData(data, plans)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, got, []byte(`{"update_products_by_pk":{"id":3,"name":"a"}}`))

	// A write that matched nothing is null, not an empty object.
	got, err = reshapeHasuraMutationData([]byte(`{"update_products_by_pk":[]}`), plans)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, got, []byte(`{"update_products_by_pk":null}`))
}

// A caller that selected columns directly gets exactly what GraphJin returned.
func TestReshapeHasuraMutationLeavesDirectSelectionsAlone(t *testing.T) {
	data := []byte(`{"update_products":[{"id":1}]}`)
	plans := []qcode.HasuraMutationRoot{{ResponseKey: "update_products"}}
	got, err := reshapeHasuraMutationData(data, plans)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, got, data)
}

func TestReshapeHasuraMutationPreservesNullRoot(t *testing.T) {
	plans := []qcode.HasuraMutationRoot{{ResponseKey: "update_private", Returning: true}}
	got, err := reshapeHasuraMutationData([]byte(`{"update_private":null}`), plans)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, got, []byte(`{"update_private":null}`))
}

// A single-object native response still counts as one written row.
func TestReshapeHasuraMutationAcceptsASingleObjectResponse(t *testing.T) {
	plans := []qcode.HasuraMutationRoot{{ResponseKey: "insert_products", Returning: true, AffectedRows: true}}
	got, err := reshapeHasuraMutationData([]byte(`{"insert_products":{"id":1}}`), plans)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, got, []byte(`{"insert_products":{"returning":[{"id":1}],"affected_rows":1}}`))
}
