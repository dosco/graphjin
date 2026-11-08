package psql_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// A GraphQL null literal is the label null, not the string "null". Every
// dialect must write SQL NULL for it; the linear path used to quote it.
// TestNullLiteralWritesSQLNullSQLite (core) runs the same writes on SQLite.
func TestNullLiteralMutationsRenderSQLNull(t *testing.T) {
	schema, err := sdata.GetTestSchema()
	if err != nil {
		t.Fatal(err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	qc.AddRole("user", "public", "users", qcode.TRConfig{
		Query:  qcode.QueryConfig{Columns: []string{"id", "full_name", "email"}},
		Insert: qcode.InsertConfig{},
		Update: qcode.UpdateConfig{},
	})
	for _, dbType := range []string{"postgres", "mysql", "sqlite", "mssql", "oracle"} {
		for _, gql := range []string{
			`mutation { users(where: { id: { eq: 1 } }, update: { full_name: null }) { id } }`,
			`mutation { users(insert: { full_name: null, email: "a@b.c" }) { id } }`,
		} {
			reqQC, err := qc.Compile([]byte(gql), nil, "user", "")
			if err != nil {
				t.Fatalf("%s: compile qcode: %v", dbType, err)
			}
			_, sqlBytes, err := psql.NewCompiler(psql.Config{DBType: dbType}).CompileEx(reqQC)
			if err != nil {
				t.Fatalf("%s: compile sql: %v", dbType, err)
			}
			if sql := string(sqlBytes); strings.Contains(strings.ToLower(sql), "'null'") {
				t.Errorf("%s renders a null literal as the string 'null':\n%s\n%s", dbType, gql, sql)
			}
		}
	}
}
