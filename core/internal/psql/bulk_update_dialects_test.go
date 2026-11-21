package psql_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// A bulk update must join each payload row to the table row with the same
// primary key. PostgreSQL, MySQL and MariaDB once joined only through the
// where clause, so every matched row took the values of an arbitrary row.
func TestBulkUpdateJoinsRowsByPrimaryKey(t *testing.T) {
	cases := []struct {
		db, join, where string
	}{
		{"postgres", `"products"."id" = "t"."id"`, `"products"."id") = ANY`},
		{"mysql", "`products`.`id` = `t`.`id`", "`products`.`id`) IN"},
		{"mariadb", "`products`.`id` = `t`.`id`", "`products`.`id`) IN"},
		{"sqlite", `"products"."id" = t."id"`, `"products"."id") IN`},
	}
	vars := map[string]json.RawMessage{
		"data": json.RawMessage(`[{"id": 1, "name": "a"}, {"id": 2, "name": "b"}]`),
	}
	for _, tc := range cases {
		t.Run(tc.db, func(t *testing.T) {
			withWhere := compileBulkUpdateSQL(t, tc.db,
				`mutation { products(update: $data, where: { id: { in: [1, 2] } }) { id name } }`, vars)
			if !strings.Contains(withWhere, tc.join+" AND (("+tc.where) {
				t.Fatalf("update does not join rows by primary key before the where clause:\n%s", withWhere)
			}

			withoutWhere := compileBulkUpdateSQL(t, tc.db,
				`mutation { products(update: $data) { id name } }`, vars)
			if !strings.Contains(withoutWhere, "WHERE "+tc.join) {
				t.Fatalf("update without where does not join rows by primary key:\n%s", withoutWhere)
			}
			if strings.Contains(withoutWhere, tc.join+" AND") {
				t.Fatalf("update without where renders a dangling AND:\n%s", withoutWhere)
			}
		})
	}
}

func compileBulkUpdateSQL(t *testing.T, dbType, gql string, vars map[string]json.RawMessage) string {
	t.Helper()
	schema, err := sdata.GetTestSchema()
	if err != nil {
		t.Fatal(err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	reqQC, err := qc.Compile([]byte(gql), vars, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	_, sqlBytes, err := psql.NewCompiler(psql.Config{DBType: dbType}).CompileEx(reqQC)
	if err != nil {
		t.Fatal(err)
	}
	return string(sqlBytes)
}
