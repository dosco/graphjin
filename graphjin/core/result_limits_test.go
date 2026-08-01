package core

import (
	"encoding/json"
	"testing"
)

func limitedResult(data string, limits ...RootLimitInfo) *Result {
	return &Result{Data: json.RawMessage(data), rootLimits: limits}
}

func TestTruncatedRootsFlagsFullPages(t *testing.T) {
	res := limitedResult(
		`{"products":[{"id":1},{"id":2},{"id":3}]}`,
		RootLimitInfo{FieldName: "products", Path: []string{"products"}, Limit: 3},
	)
	got := res.TruncatedRoots()
	if len(got) != 1 || got[0].Path != "products" || got[0].Rows != 3 || got[0].Limit != 3 {
		t.Fatalf("TruncatedRoots = %+v", got)
	}
}

func TestTruncatedRootsIgnoresPartialPages(t *testing.T) {
	res := limitedResult(
		`{"products":[{"id":1},{"id":2}]}`,
		RootLimitInfo{FieldName: "products", Path: []string{"products"}, Limit: 3},
	)
	if got := res.TruncatedRoots(); len(got) != 0 {
		t.Fatalf("partial page flagged: %+v", got)
	}
}

func TestTruncatedRootsSkipsNoLimitAggregatesAndSingular(t *testing.T) {
	res := limitedResult(
		`{"products":[{"count_id":100}],"account":{"id":1}}`,
		RootLimitInfo{FieldName: "products", Path: []string{"products"}, Limit: 0, NoLimit: true, Aggregate: true},
		RootLimitInfo{FieldName: "account", Path: []string{"account"}, Limit: 20, Singular: true},
	)
	if got := res.TruncatedRoots(); len(got) != 0 {
		t.Fatalf("no-limit aggregate or singular flagged: %+v", got)
	}
}

func TestTruncatedRootsWalksNestedChildLists(t *testing.T) {
	res := limitedResult(
		`{"customers":[
			{"id":1,"products":[{"id":1},{"id":2}]},
			{"id":2,"products":[{"id":3},{"id":4},{"id":5}]}
		]}`,
		RootLimitInfo{FieldName: "customers", Path: []string{"customers"}, Limit: 20},
		RootLimitInfo{FieldName: "products", Path: []string{"customers", "products"}, Limit: 3},
	)
	got := res.TruncatedRoots()
	if len(got) != 1 || got[0].Path != "customers.products" || got[0].Rows != 3 {
		t.Fatalf("nested truncation = %+v", got)
	}
}

func TestTruncatedRootsEmptyInputs(t *testing.T) {
	if got := (&Result{}).TruncatedRoots(); got != nil {
		t.Fatalf("empty result flagged: %+v", got)
	}
	res := limitedResult(`{"products":"not-a-list"}`,
		RootLimitInfo{FieldName: "products", Path: []string{"products"}, Limit: 3})
	if got := res.TruncatedRoots(); len(got) != 0 {
		t.Fatalf("non-list leaf flagged: %+v", got)
	}
}
