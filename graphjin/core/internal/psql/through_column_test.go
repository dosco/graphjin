package psql_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// Issue #642: the target column names must not make the plain FK hint select
// an origin/destination relationship. Verify the complete composite predicate
// through GraphQL -> QCode -> SQL for both nesting directions and every SQL
// dialect used by the reported bug and the existing relational backends.
func TestThroughColumnSharedTargetCompositeFK(t *testing.T) {
	for _, dbType := range []string{"postgres", "mysql", "sqlite", "oracle", "mssql"} {
		t.Run(dbType, func(t *testing.T) {
			di := sdata.GetTestShipmentFKDBInfo()
			di.Type = dbType
			schema, err := sdata.NewDBSchema(di, nil)
			if err != nil {
				t.Fatal(err)
			}
			qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
			if err != nil {
				t.Fatal(err)
			}
			pc := psql.NewCompiler(psql.Config{DBType: dbType})
			for _, reverse := range []bool{false, true} {
				parent, child, field := "shipments", "warehouses", "name"
				if reverse {
					parent, child, field = "warehouses", "shipments", "id"
				}
				for _, prefix := range []string{"", "origin_", "destination_"} {
					for _, column := range []string{"branch_id", "warehouse_id"} {
						t.Run(fmt.Sprintf("reverse_%t/%s%s", reverse, prefix, column), func(t *testing.T) {
							query := fmt.Sprintf(`query { %s { %s @through(column: %q) { %s } } }`, parent, child, prefix+column, field)
							sql := compileWith(t, qc, pc, query)
							normalized := strings.ToLower(strings.NewReplacer(`"`, "", "`", "", "[", "", "]", "", "(", "", ")", "").Replace(sql))
							for _, key := range []string{"branch_id", "warehouse_id"} {
								// Dialects use either the base table alias or a numbered
								// selection alias inside the correlated join predicate.
								local := `shipments(?:_[0-9]+)?\.` + prefix + key
								foreign := `warehouses(?:_[0-9]+)?\.` + key
								predicate := regexp.MustCompile(local + `\s*=\s*` + foreign + `|` + foreign + `\s*=\s*` + local)
								if !predicate.MatchString(normalized) {
									t.Fatalf("missing composite join %s = %s:\n%s", local, foreign, sql)
								}
							}
							for _, other := range []string{"origin_", "destination_"} {
								if other != prefix && regexp.MustCompile(`shipments(?:_[0-9]+)?\.`+other).MatchString(normalized) {
									t.Fatalf("join used another FK %s:\n%s", other, sql)
								}
							}
						})
					}
				}
			}
		})
	}
}
