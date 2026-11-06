package valid

import (
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
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

func TestConstraintStringsUseDecodedJSONValues(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		for _, tc := range []struct{ value, raw string }{
			{"<tag>", `"<tag>"`},
			{"A&B", `"A&B"`},
			{"a>b", `"a>b"`},
			{"Ada", `"\u0041da"`},
			{"a/b", `"a\/b"`},
			{"line\nfeed", `"line\u000afeed"`},
			{"café🙂", `"caf\u00e9\ud83d\ude42"`},
			{"a\u2028b", "\"a\u2028b\""},
		} {
			t.Run(dialect+"/"+tc.value, func(t *testing.T) {
				literal, _ := json.Marshal(tc.value)
				for _, validator := range []string{"equals", "notEquals", "oneOf", "equalsField", "notEqualsField", "requiredIf", "requiredUnless"} {
					t.Run(validator, func(t *testing.T) {
						argument := string(literal)
						vars := qcode.Vars{"target": json.RawMessage(tc.raw)}
						wantError := validator == "notEquals" || validator == "notEqualsField" || validator == "requiredIf"
						switch validator {
						case "oneOf":
							argument = "[" + argument + "]"
						case "equalsField", "notEqualsField":
							argument = `"other"`
							vars["other"] = literal
						case "requiredIf", "requiredUnless":
							argument = "{other:" + argument + "}"
							delete(vars, "target")
							vars["other"] = json.RawMessage(tc.raw)
						}
						compiled := compileStringConstraint(t, dialect, validator+":"+argument)
						if got := len(compiled.ProcessConstraints(vars)) != 0; got != wantError {
							t.Fatalf("equivalent JSON encodings: rejected=%v, want %v", got, wantError)
						}
					})
				}
			})
		}
	}
}

func TestConstraintStringComparisonKeepsValueAndTypeDistinct(t *testing.T) {
	for _, directive := range []string{`equals:"1"`, `oneOf:["1"]`} {
		compiled := compileStringConstraint(t, "postgres", directive)
		for _, raw := range []string{`1`, `null`, `true`, `"2"`, `"\u0031!"`} {
			if errs := compiled.ProcessConstraints(qcode.Vars{"target": json.RawMessage(raw)}); len(errs) != 1 {
				t.Fatalf("%s accepted distinct value %s: %v", directive, raw, errs)
			}
		}
	}
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
