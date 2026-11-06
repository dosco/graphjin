package valid

import (
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// Field-comparison validators receive the other field's name as a JSON string
// literal; they must look up the variable by its decoded name.
func TestNumericFieldComparisonsResolveOtherField(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		for _, tc := range []struct {
			validator string
			target    string
			pass      bool
		}{
			{"greaterThanField", "6", true},
			{"greaterThanField", "5", false},
			{"greaterThanOrEqualsField", "5", true},
			{"greaterThanOrEqualsField", "4", false},
			{"lessThanField", "4", true},
			{"lessThanField", "5", false},
			{"lessThanOrEqualsField", "5", true},
			{"lessThanOrEqualsField", "6", false},
		} {
			t.Run(dialect+"/"+tc.validator+"/"+tc.target, func(t *testing.T) {
				compiled := compileStringConstraint(t, dialect, tc.validator+`:"other"`)
				vars := qcode.Vars{"target": json.RawMessage(tc.target), "other": json.RawMessage("5")}
				if got := len(compiled.ProcessConstraints(vars)) == 0; got != tc.pass {
					t.Fatalf("accepted=%v, want %v", got, tc.pass)
				}
			})
		}
	}
}
