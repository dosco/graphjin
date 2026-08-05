package core

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

func TestReshapeHasuraAggregateData(t *testing.T) {
	data := []byte(`{
		"subscriptions_aggregate":[{"count_id":5,"max_renews_at":"2027-02-19"}],
		"money":[{"sum_price":12345678901234567890}],
		"untouched":[{"id":1}]
	}`)
	plans := []qcode.HasuraAggregateRoot{
		{ResponseKey: "subscriptions_aggregate", Fields: []qcode.HasuraAggregateField{
			{NativeField: "count_id", Path: []string{"aggregate", "count"}},
			{NativeField: "max_renews_at", Path: []string{"aggregate", "max", "renews_at"}},
		}},
		{ResponseKey: "money", Fields: []qcode.HasuraAggregateField{
			{NativeField: "sum_price", Path: []string{"aggregate", "sum", "price"}},
		}},
	}
	got, err := reshapeHasuraAggregateData(data, plans)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{
		"subscriptions_aggregate":{"aggregate":{"count":5,"max":{"renews_at":"2027-02-19"}}},
		"money":{"aggregate":{"sum":{"price":12345678901234567890}}},
		"untouched":[{"id":1}]
	}`)
	assertJSONEqual(t, got, want)
}

func TestReshapeHasuraAggregateDataPreservesNullRoot(t *testing.T) {
	plan := []qcode.HasuraAggregateRoot{{ResponseKey: "private_aggregate", Fields: []qcode.HasuraAggregateField{
		{NativeField: "count_id", Path: []string{"aggregate", "count"}},
	}}}
	got, err := reshapeHasuraAggregateData([]byte(`{"private_aggregate":null}`), plan)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, got, []byte(`{"private_aggregate":null}`))
}

func assertJSONEqual(t *testing.T, got, want []byte) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("invalid got JSON: %v", err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("invalid want JSON: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON mismatch\ngot:  %s\nwant: %s", got, want)
	}
}
