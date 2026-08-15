package psql_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// A relationship can be declared on an array column: products.tags holds the
// slugs of its tags. Every dialect has to expand that array into a set before
// matching it against the child key — a dialect that compares the child key to
// the array column itself compiles fine and silently returns nothing, so each
// one is pinned to the expansion it is supposed to emit.
func TestArrayColumnRelationshipPerDialect(t *testing.T) {
	gql := `query { products { id tags { id name } } }`

	cases := []struct {
		dbType string
		want   string
		unwant string // the bare-column comparison that matches no rows
	}{
		{
			dbType: "postgres",
			want:   `("tags"."slug") = ANY ("products_0"."tags")`,
		},
		{
			dbType: "mysql",
			want:   "(`tags`.`slug`) IN (SELECT _gj_jt.* FROM (SELECT CAST(`products_0`.`tags` AS JSON) as ids) j, JSON_TABLE(j.ids, '$[*]' COLUMNS(tags text PATH '$' ERROR ON ERROR)) AS _gj_jt)",
			unwant: "(`tags`.`slug`) IN (`products_0`.`tags`)",
		},
		{
			// MariaDB has no LATERAL, so the child is a correlated subquery and
			// the array column has to go into JSON_TABLE directly: a derived
			// table around it cannot see the enclosing query.
			dbType: "mariadb",
			want:   "(`tags_1`.`slug`) IN (SELECT _gj_jt.* FROM JSON_TABLE(`products_0`.`tags`, '$[*]' COLUMNS(tags TEXT PATH '$' ERROR ON ERROR)) AS _gj_jt)",
			unwant: "(`tags_1`.`slug`) IN (`products_0`.`tags`)",
		},
		{
			dbType: "sqlite",
			want:   `("tags"."slug") IN (SELECT value FROM json_each("products_0"."tags"))`,
			unwant: `("tags"."slug") IN ("products_0"."tags")`,
		},
	}

	for _, c := range cases {
		t.Run(c.dbType, func(t *testing.T) {
			sql := compileArraySQL(t, c.dbType, gql)
			if !strings.Contains(sql, c.want) {
				t.Fatalf("[%s] compiled SQL missing %q:\n%s", c.dbType, c.want, sql)
			}
			if c.unwant != "" && strings.Contains(sql, c.unwant) {
				t.Fatalf("[%s] array column used unexpanded (%q):\n%s", c.dbType, c.unwant, sql)
			}
		})
	}
}

func compileArraySQL(t *testing.T, dbType, gql string) string {
	t.Helper()

	schema, err := sdata.GetTestSchema()
	if err != nil {
		t.Fatal(err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	reqQC, err := qc.Compile([]byte(gql), nil, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	_, sqlBytes, err := psql.NewCompiler(psql.Config{DBType: dbType}).CompileEx(reqQC)
	if err != nil {
		t.Fatal(err)
	}
	return string(sqlBytes)
}
