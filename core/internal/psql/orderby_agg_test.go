package psql_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
)

// compileToSQL compiles a GraphQL query through the shared qcode + psql
// compilers and returns the emitted SQL.
func compileToSQL(t *testing.T, gql string) string {
	t.Helper()
	qc, err := qcompile.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatalf("qcode compile: %v", err)
	}
	_, sqlBytes, err := pcompile.CompileEx(qc)
	if err != nil {
		t.Fatalf("psql compile: %v", err)
	}
	return string(sqlBytes)
}

func compileToDialectSQL(t *testing.T, dbType, gql string) string {
	t.Helper()
	qc, err := qcompile.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatalf("qcode compile: %v", err)
	}
	_, sqlBytes, err := psql.NewCompiler(psql.Config{DBType: dbType}).CompileEx(qc)
	if err != nil {
		t.Fatalf("%s compile: %v", dbType, err)
	}
	return string(sqlBytes)
}

// groupByClause extracts the text of the GROUP BY clause (up to the
// following ORDER BY / LIMIT) so assertions don't accidentally match
// column references elsewhere in the statement.
func groupByClause(t *testing.T, sql string) string {
	t.Helper()
	i := strings.Index(sql, "GROUP BY")
	if i < 0 {
		t.Fatalf("emitted SQL has no GROUP BY:\n%s", sql)
	}
	clause := sql[i:]
	for _, stop := range []string{" ORDER BY ", " LIMIT ", ")"} {
		if j := strings.Index(clause, stop); j >= 0 {
			clause = clause[:j]
		}
	}
	return clause
}

// TestOrderByAggregateSQLKeepsGroupBy is the SQL-level regression for the
// aggregate order_by bug: `order_by: { sum_price: desc }` used to add the
// raw `price` column to the base columns, which put it in GROUP BY and
// collapsed each group to a single source row. The grouped shape must
// survive: GROUP BY on the dimension only, ORDER BY on the aggregate.
func TestOrderByAggregateSQLKeepsGroupBy(t *testing.T) {
	sql := compileToSQL(t, `query {
		products(order_by: { sum_price: desc }, limit: 1) {
			name
			sum_price
		}
	}`)

	gb := groupByClause(t, sql)
	if !strings.Contains(gb, `"products"."name"`) {
		t.Errorf("GROUP BY missing dimension column:\n%s\nfull SQL:\n%s", gb, sql)
	}
	if strings.Contains(gb, `"products"."price"`) {
		t.Errorf("GROUP BY contains the aggregate's source column (degenerate per-row grouping):\n%s\nfull SQL:\n%s", gb, sql)
	}
	if !strings.Contains(sql, `ORDER BY SUM("products"."price") DESC`) {
		t.Errorf("ORDER BY must reference the aggregate expression, SQL:\n%s", sql)
	}
	if !strings.Contains(sql, `sum("products"."price") AS "sum_price"`) {
		t.Errorf("aggregate select column missing, SQL:\n%s", sql)
	}
}

// TestOrderByAliasAggregateSQL covers the SELECT-list-alias variant
// (`order_by: { revenue: desc }` where revenue is an expression
// aggregate). The alias entry has no backing column; it used to inject an
// empty-named column into the base select and GROUP BY.
func TestOrderByAliasAggregateSQL(t *testing.T) {
	sql := compileToSQL(t, `query {
		products(order_by: { revenue: desc }, limit: 1) {
			name
			revenue: sum(expr: { mul: [price, 2] })
		}
	}`)

	gb := groupByClause(t, sql)
	if !strings.Contains(gb, `"products"."name"`) {
		t.Errorf("GROUP BY missing dimension column:\n%s\nfull SQL:\n%s", gb, sql)
	}
	if strings.Contains(sql, `.""`) {
		t.Errorf("emitted SQL references an empty column name (alias order_by leaked a zero-value column):\n%s", sql)
	}
	if !strings.Contains(sql, `ORDER BY "revenue" DESC`) {
		t.Errorf("ORDER BY must reference the select-list alias, SQL:\n%s", sql)
	}
}

// TestOrderByAggregateWithoutSelectionSQL: ordering by an aggregate that
// isn't selected still compiles to a grouped query (GROUP BY the selected
// dimension, ORDER BY the aggregate) rather than an ungrouped ORDER BY
// SUM(...) that the database would reject.
func TestOrderByAggregateWithoutSelectionSQL(t *testing.T) {
	sql := compileToSQL(t, `query {
		products(order_by: { sum_price: desc }, limit: 3) {
			name
		}
	}`)

	gb := groupByClause(t, sql)
	if !strings.Contains(gb, `"products"."name"`) {
		t.Errorf("GROUP BY missing dimension column:\n%s\nfull SQL:\n%s", gb, sql)
	}
	if strings.Contains(gb, `"products"."price"`) {
		t.Errorf("GROUP BY contains the aggregate's source column:\n%s\nfull SQL:\n%s", gb, sql)
	}
	if !strings.Contains(sql, `ORDER BY SUM("products"."price") DESC`) {
		t.Errorf("ORDER BY must reference the aggregate expression, SQL:\n%s", sql)
	}
}

func TestOrderByAggregateMSSQLSQL(t *testing.T) {
	sql := compileToDialectSQL(t, "mssql", `query {
		products(order_by: { sum_price: desc }, limit: 1) {
			name
			sum_price
		}
	}`)

	if !strings.Contains(sql, `ORDER BY SUM([products_0].[price]) DESC`) {
		t.Fatalf("MSSQL ORDER BY must retain the aggregate expression, SQL:\n%s", sql)
	}
}

func TestNestedOrderByAggregateMSSQLSQL(t *testing.T) {
	sql := compileToDialectSQL(t, "mssql", `query {
		users {
			id
			products(order_by: { sum_price: desc }, limit: 1) {
				name
				sum_price
			}
		}
	}`)

	if !strings.Contains(sql, `ORDER BY SUM([products_1].[price]) DESC`) {
		t.Fatalf("nested MSSQL ORDER BY must retain the aggregate expression, SQL:\n%s", sql)
	}
}

func TestGroupedAggregateOracleSQL(t *testing.T) {
	sql := compileToDialectSQL(t, "oracle", `query {
		products(limit: 5) {
			name
			count_id
		}
	}`)

	if !strings.Contains(sql, `GROUP BY "PRODUCTS"."NAME"`) {
		t.Fatalf("Oracle grouped page must retain its dimension GROUP BY, SQL:\n%s", sql)
	}
	if strings.Contains(sql, `ORDER BY "PRODUCTS"."ID"`) {
		t.Fatalf("Oracle grouped page must not order by an ungrouped primary key, SQL:\n%s", sql)
	}
}
