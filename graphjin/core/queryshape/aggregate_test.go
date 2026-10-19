package queryshape

import (
	"reflect"
	"testing"
)

func TestAggregateSelections(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []Aggregate
	}{
		{`query { accounts { total: sum_mrr_cents } }`, []Aggregate{{"accounts", "sum", "mrr_cents"}}},
		{`query { stats: accounts_aggregate { aggregate { total: sum { mrr_cents } count } } }`, []Aggregate{{"accounts", "sum", "mrr_cents"}, {"accounts", "count", ""}}},
		{`{ usage_events_aggregate { sum { quantity } } }`, []Aggregate{{"usage_events", "sum", "quantity"}}},
		{`{ accounts { sum(column: mrr_cents) } }`, []Aggregate{{"accounts", "sum", "mrr_cents"}}},
		{`{ accounts { sum(expr: {mul: [{col: mrr_cents}, {num: 2}]}) } }`, []Aggregate{{"accounts", "sum", ""}}},
		{`query { accounts { ...Totals } } fragment Totals on accounts { sum_mrr_cents }`, []Aggregate{{"accounts", "sum", "mrr_cents"}}},
		{`{ values_aggregate { aggregate { sum { sum_price } } } }`, []Aggregate{{"values", "sum", "sum_price"}}},
		{`query sum_mrr_cents { accounts(where: {name: {eq: "sum_mrr_cents"}}) { sum_mrr_cents: mrr_cents } } # sum_mrr_cents`, nil},
		{`{ accounts { aggregate { sum { mrr_cents } } } }`, nil},
		{`{ accounts { mrr_cents } }`, nil},
	} {
		got, err := Aggregates(tc.query)
		if err != nil {
			t.Fatalf("%s: %v", tc.query, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: got %+v, want %+v", tc.query, got, tc.want)
		}
	}
}

func TestMalformedQueryCannotProveAggregate(t *testing.T) {
	if facts, err := Aggregates(`{ accounts { sum_mrr_cents`); err == nil || len(facts) != 0 {
		t.Fatalf("facts = %v, err = %v", facts, err)
	}
}
