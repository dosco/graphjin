package psql_test

import (
	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"strings"
	"testing"
)

func TestKeywordColumnNamesAcrossDialects(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dialect, func(t *testing.T) {
			cols := []sdata.DBColumn{}
			for _, name := range []string{"id", "on", "true", "false"} {
				cols = append(cols, sdata.DBColumn{Schema: "public", Table: "switches", Name: name, Type: "text", PrimaryKey: name == "id"})
			}
			schema, err := sdata.NewDBSchema(sdata.NewDBInfo(dialect, 0, "public", "test", cols, nil, nil), nil)
			if err != nil {
				t.Fatal(err)
			}
			qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: "public"})
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := qc.Compile([]byte(`{ switches { id on true false } }`), nil, "user", "")
			if err != nil {
				t.Fatal(err)
			}
			_, sql, err := psql.NewCompiler(psql.Config{DBType: dialect}).CompileEx(compiled)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"on", "true", "false"} {
				quote := `"`
				if dialect == "mysql" {
					quote = "`"
				}
				if !strings.Contains(string(sql), quote+name+quote) {
					t.Fatalf("column %q missing: %s", name, sql)
				}
			}
		})
	}
}
