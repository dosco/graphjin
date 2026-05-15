package psql_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestAnalytics_RendersRunningOverClause(t *testing.T) {
	gql := `query {
		products {
			id
			price
			running: price @running(aggregate: sum, by: "id", order: desc)
		}
	}`

	sql := compileGQLToPSQLString(t, gql, nil, "user")
	for _, want := range []string{
		"sum(",
		"OVER (PARTITION BY",
		"ORDER BY",
		"DESC",
		"ROWS UNBOUNDED PRECEDING",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL missing %q\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "GROUP BY") {
		t.Errorf("did not expect GROUP BY for pure analytic query, got:\n%s", sql)
	}
}

func TestAnalytics_RendersMovingFrame(t *testing.T) {
	gql := `query {
		products {
			id
			moving_avg: price @moving(aggregate: avg, rows: 6, by: "id", orderBy: { created_at: asc })
		}
	}`

	sql := compileGQLToPSQLString(t, gql, nil, "user")
	if !strings.Contains(sql, "avg(") {
		t.Errorf("expected avg() in SQL, got:\n%s", sql)
	}
	if !strings.Contains(sql, "ROWS BETWEEN 5 PRECEDING AND CURRENT ROW") {
		t.Errorf("expected trailing moving frame in SQL, got:\n%s", sql)
	}
}

func TestAnalytics_DialectRendering(t *testing.T) {
	cases := []struct {
		name    string
		dbType  string
		wantCol string
	}{
		{"postgres", "", `"products"."price"`},
		{"mysql", "mysql", "`products`.`price`"},
		{"mariadb", "mariadb", "`products_0`.`price`"},
		{"sqlite", "sqlite", `"products"."price"`},
		{"oracle", "oracle", `"PRODUCTS"."PRICE"`},
		{"mssql", "mssql", `[products_0].[price]`},
		{"snowflake", "snowflake", `"products"."price"`},
	}
	for _, c := range cases {
		sql := compileWindowGQLToSQLString(t, c.dbType, 0, `query {
			products {
				id
				row_num: price @rowNumber(by: "user_id", orderBy: { created_at: asc })
				rank_by_price: price @rank(by: "user_id", order: desc)
				dense_by_price: price @denseRank(by: "user_id", order: desc)
				prev: price @previous(by: "user_id", orderBy: { created_at: asc })
				next: price @next(by: "user_id", orderBy: { created_at: asc })
				first: price @first(by: "user_id", orderBy: { created_at: asc })
				last: price @last(by: "user_id", orderBy: { created_at: asc })
				running: price @running(aggregate: sum, by: "user_id", orderBy: { created_at: asc })
				moving: price @moving(aggregate: avg, rows: 3, by: "user_id", orderBy: { created_at: asc })
			}
		}`)
		for _, want := range []string{
			"row_number() OVER",
			"rank() OVER",
			"dense_rank() OVER",
			"lag(",
			"lead(",
			"first_value(",
			"last_value(",
			"sum(",
			"avg(",
			"OVER (PARTITION BY",
			"ROWS UNBOUNDED PRECEDING",
			"ROWS BETWEEN 2 PRECEDING AND CURRENT ROW",
			c.wantCol,
		} {
			if !strings.Contains(sql, want) {
				t.Errorf("%s: SQL missing %q\n%s", c.name, want, sql)
			}
		}
	}
}

func TestAnalytics_DialectUnsupported(t *testing.T) {
	cases := []struct {
		name    string
		dbType  string
		version int
		want    string
	}{
		{"mongodb", "mongodb", 0, "MongoDB"},
		{"old mysql", "mysql", 50700, "MySQL 8.0+"},
		{"old mariadb", "mariadb", 100100, "MariaDB 10.2+"},
		{"old sqlite", "sqlite", 32400, "SQLite 3.25+"},
		{"old mssql", "mssql", 2008, "SQL Server 2012+"},
	}
	for _, c := range cases {
		err := compileWindowGQLToSQLError(c.dbType, c.version, `query {
			products {
				running: price @running(aggregate: sum, by: "user_id", orderBy: { created_at: asc })
			}
		}`)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.want, err)
		}
		if err != nil && !strings.Contains(err.Error(), "analytics directive") {
			t.Errorf("%s: expected analytics directive error, got %v", c.name, err)
		}
	}
}

func compileWindowGQLToSQLString(t *testing.T, dbType string, version int, gql string) string {
	t.Helper()
	sql, err := compileWindowGQL(dbType, version, gql)
	if err != nil {
		t.Fatal(err)
	}
	return sql
}

func compileWindowGQLToSQLError(dbType string, version int, gql string) error {
	_, err := compileWindowGQL(dbType, version, gql)
	return err
}

func compileWindowGQL(dbType string, version int, gql string) (string, error) {
	schema, err := sdata.GetTestSchema()
	if err != nil {
		return "", err
	}
	qcCompiler, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		return "", err
	}
	if err := qcCompiler.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "price", "user_id", "created_at"},
		},
	}); err != nil {
		return "", err
	}
	reqQC, err := qcCompiler.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		return "", err
	}
	pc := psql.NewCompiler(psql.Config{DBType: dbType, DBVersion: version})
	_, sqlBytes, err := pc.CompileEx(reqQC)
	if err != nil {
		return "", err
	}
	return string(sqlBytes), nil
}
