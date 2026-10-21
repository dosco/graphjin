package valid

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func compileConstraint(t *testing.T, dialect, directive string) *qcode.QCode {
	t.Helper()
	cols := []sdata.DBColumn{{Schema: "public", Table: "users", Name: "id", Type: "bigint", PrimaryKey: true}}
	schema, err := sdata.NewDBSchema(sdata.NewDBInfo(dialect, 160000, "public", "db", cols, nil, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema(), Validators: Validators})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile([]byte(`query @constraint(variable: "target", `+directive+`) { users { id } }`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func TestConditionalPresenceTruthTables(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		for _, name := range []string{"requiredWith", "requiredWithAll", "requiredWithout", "requiredWithoutAll"} {
			t.Run(dialect+"/"+name, func(t *testing.T) {
				for _, fields := range []string{`"first"`, `["first", "second"]`} {
					compiled := compileConstraint(t, dialect, name+":"+fields)
					for mask := 0; mask < 4; mask++ {
						vars := qcode.Vars{}
						first, second := mask&1 != 0, mask&2 != 0
						if first {
							vars["first"] = json.RawMessage(`1`)
						}
						if second {
							vars["second"] = json.RawMessage(`2`)
						}
						anyPresent, allPresent := first || second, first && second
						if fields == `"first"` {
							anyPresent, allPresent = first, first
						}
						required := false
						switch name {
						case "requiredWith":
							required = anyPresent
						case "requiredWithAll":
							required = allPresent
						case "requiredWithout":
							required = !allPresent
						case "requiredWithoutAll":
							required = !anyPresent
						}
						if got := len(compiled.ProcessConstraints(vars)) > 0; got != required {
							t.Errorf("%s mask%d: rejected=%v want%v", fields, mask, got, required)
						}
						vars["target"] = json.RawMessage(`"present"`)
						if errs := compiled.ProcessConstraints(vars); len(errs) != 0 {
							t.Errorf("present target rejected: %v", errs)
						}
					}
				}
			})
		}
	}
}

func TestConditionalValueMultipleFields(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		for _, name := range []string{"requiredIf", "requiredUnless"} {
			t.Run(dialect+"/"+name, func(t *testing.T) {
				compiled := compileConstraint(t, dialect, fmt.Sprintf(`%s:{first:1,second:2}`, name))
				for _, match := range []bool{false, true} {
					vars := qcode.Vars{"first": json.RawMessage(`1`), "second": json.RawMessage(`2`)}
					if !match {
						vars["second"] = json.RawMessage(`3`)
					}
					required := match
					if name == "requiredUnless" {
						required = !match
					}
					if got := len(compiled.ProcessConstraints(vars)) > 0; got != required {
						t.Errorf("match%v rejected%v want%v", match, got, required)
					}
				}
			})
		}
	}
}
