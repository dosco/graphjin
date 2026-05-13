package psql_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// TestWindow_RendersOverClause confirms that an aggregate field carrying
// @window emits `<func>(...) OVER (PARTITION BY ... ORDER BY ...)` in the
// generated SQL and does NOT trigger a GROUP BY (window functions return
// one row per input row).
func TestWindow_RendersOverClause(t *testing.T) {
	gql := `query {
		products {
			id
			price
			running: sum_price @window(partition: ["id"], order: ["price desc"], frame: "rows unbounded preceding")
		}
	}`

	sql := compileGQLToPSQLString(t, gql, nil, "user")

	if !strings.Contains(sql, "OVER (PARTITION BY") {
		t.Errorf("expected SQL to contain 'OVER (PARTITION BY', got:\n%s", sql)
	}
	if !strings.Contains(sql, "ORDER BY") {
		t.Errorf("expected SQL to contain 'ORDER BY' inside OVER, got:\n%s", sql)
	}
	if !strings.Contains(sql, "ROWS UNBOUNDED PRECEDING") {
		t.Errorf("expected canonical frame in SQL, got:\n%s", sql)
	}
	// Pure window aggregate without a sibling pure-aggregate must NOT
	// inject a GROUP BY.
	if strings.Contains(sql, "GROUP BY") {
		t.Errorf("did not expect GROUP BY for pure-window query, got:\n%s", sql)
	}
}

func TestWindow_PartitionOnlyOmitsOrderBy(t *testing.T) {
	gql := `query {
		products {
			id
			running: sum_price @window(partition: ["id"])
		}
	}`

	sql := compileGQLToPSQLString(t, gql, nil, "user")
	if !strings.Contains(sql, "OVER (PARTITION BY") {
		t.Errorf("expected OVER (PARTITION BY ...) in SQL, got:\n%s", sql)
	}
	// Ensure we didn't accidentally emit `ORDER BY ` immediately after the
	// PARTITION BY columns.
	if strings.Contains(sql, "PARTITION BY \"products\".\"id\" ORDER BY") {
		t.Errorf("ORDER BY should be absent when only partition is set, got:\n%s", sql)
	}
}

// TestWindow_NumericFrameAndNulls confirms the parametric frame
// patterns (e.g. ROWS BETWEEN <n> PRECEDING AND <n> FOLLOWING) reach
// the SQL output verbatim, alongside NULLS FIRST/LAST in the OVER's
// ORDER BY. These shapes are what Snowflake / Postgres / Oracle
// analytics queries use most.
func TestWindow_NumericFrameAndNulls(t *testing.T) {
	gql := `query {
		products {
			id
			running: sum_price @window(
				partition: ["id"],
				order: ["price desc nulls last"],
				frame: "rows between 5 preceding and 5 following"
			)
		}
	}`
	sql := compileGQLToPSQLString(t, gql, nil, "user")

	wants := []string{
		"OVER (PARTITION BY",
		"ORDER BY",
		"DESC NULLS LAST",
		"ROWS BETWEEN 5 PRECEDING AND 5 FOLLOWING",
	}
	for _, w := range wants {
		if !strings.Contains(sql, w) {
			t.Errorf("SQL missing %q\n----\n%s", w, sql)
		}
	}
}

// TestWindow_EmptyDirectiveEmitsBareOver: @window with no args is the
// canonical OVER() form — a single SQL pair of parens, nothing inside.
func TestWindow_EmptyDirectiveEmitsBareOver(t *testing.T) {
	gql := `query {
		products {
			id
			total: sum_price @window
		}
	}`
	sql := compileGQLToPSQLString(t, gql, nil, "user")
	if !strings.Contains(sql, "OVER ()") {
		t.Errorf("expected bare OVER () in SQL, got:\n%s", sql)
	}
}

func TestWindow_DialectRendering(t *testing.T) {
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
				rank: row_number @window(partition: ["user_id"], order: ["price desc"])
				dense: dense_rank @window(partition: ["user_id"], order: ["price desc"])
				prev: lag_price @window(partition: ["user_id"], order: ["created_at"])
				next: lead_price @window(partition: ["user_id"], order: ["created_at"])
				first: first_value_price @window(partition: ["user_id"], order: ["created_at"])
				last: last_value_price @window(partition: ["user_id"], order: ["created_at"])
				running: sum_price @window(partition: ["user_id"], order: ["created_at"], frame: "rows unbounded preceding")
			}
		}`)
		for _, want := range []string{
			"row_number() OVER",
			"dense_rank() OVER",
			"lag(",
			"lead(",
			"first_value(",
			"last_value(",
			"sum(",
			"OVER (PARTITION BY",
			c.wantCol,
		} {
			if !strings.Contains(sql, want) {
				t.Errorf("%s: SQL missing %q\n%s", c.name, want, sql)
			}
		}
	}
}

func TestWindow_DialectUnsupported(t *testing.T) {
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
				running: sum_price @window(partition: ["user_id"], order: ["created_at"])
			}
		}`)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.want, err)
		}
	}
}

func TestWindow_NullsDialectUnsupported(t *testing.T) {
	cases := []struct {
		name    string
		dbType  string
		version int
		want    string
	}{
		{"mysql", "mysql", 0, "NULLS FIRST/LAST"},
		{"mariadb", "mariadb", 0, "NULLS FIRST/LAST"},
		{"mssql", "mssql", 0, "NULLS FIRST/LAST"},
		{"sqlite old nulls", "sqlite", 32900, "SQLite 3.30+"},
	}
	for _, c := range cases {
		err := compileWindowGQLToSQLError(c.dbType, c.version, `query {
			products {
				running: sum_price @window(partition: ["user_id"], order: ["price desc nulls last"])
			}
		}`)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.want, err)
		}
	}
}

func TestWindow_NullsDialectSupported(t *testing.T) {
	for _, dbType := range []string{"", "oracle", "snowflake"} {
		sql := compileWindowGQLToSQLString(t, dbType, 0, `query {
			products {
				running: sum_price @window(partition: ["user_id"], order: ["price desc nulls last"])
			}
		}`)
		if !strings.Contains(sql, "NULLS LAST") {
			t.Errorf("%s: expected NULLS LAST in SQL, got:\n%s", dbType, sql)
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
