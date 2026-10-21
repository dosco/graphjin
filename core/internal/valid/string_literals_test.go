package valid

import (
	"encoding/json"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"testing"
)

func compileStringConstraint(t *testing.T, dialect, directive string) *qcode.QCode {
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

func TestConstraintStringLiteralsMatchJSONVariables(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		for _, value := range []string{"Ada \"Dev\"", "line\nfeed", "C:\\temp", "tab\tvalue"} {
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			for _, validator := range []string{"equals", "oneOf"} {
				t.Run(dialect+"/"+validator+"/"+value, func(t *testing.T) {
					directive := validator + ":" + string(encoded)
					if validator == "oneOf" {
						directive = validator + ": [" + string(encoded) + "]"
					}
					compiled := compileStringConstraint(t, dialect, directive)
					if errs := compiled.ProcessConstraints(qcode.Vars{"target": encoded}); len(errs) != 0 {
						t.Fatalf("equal JSON string rejected: %v", errs)
					}
					wrong, _ := json.Marshal(value + "!")
					if errs := compiled.ProcessConstraints(qcode.Vars{"target": wrong}); len(errs) != 1 {
						t.Fatalf("different JSON string accepted: %v", errs)
					}
				})
			}
		}
	}
}
