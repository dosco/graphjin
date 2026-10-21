package queryshape

import (
	"fmt"
	"reflect"
	"testing"
)

func TestStatisticalAggregateSelections(t *testing.T) {
	for _, operation := range []string{"stddev_pop", "stddev_samp", "var_pop", "var_samp"} {
		for _, query := range []string{
			fmt.Sprintf(`{ measurements { %s_sample_value } }`, operation),
			fmt.Sprintf(`{ measurements { %s(column: sample_value) } }`, operation),
		} {
			got, err := Aggregates(query)
			want := []Aggregate{{Table: "measurements", Operation: operation, Column: "sample_value"}}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("%s: got %+v, %v; want %+v", query, got, err, want)
			}
		}
	}
}

func TestStatisticalAggregateNamesInAliasesAreNotSelections(t *testing.T) {
	got, err := Aggregates(`{ measurements { stddev_pop_sample_value: sample_value } }`)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}
