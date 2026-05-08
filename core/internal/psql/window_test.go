package psql_test

import (
	"strings"
	"testing"
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
