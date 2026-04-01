package tests_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

// TestViewCursorPaginationCrossDB verifies that cursor-based pagination works on
// views by detecting PKs from underlying base tables.
//
// SQL-level detection: PostgreSQL (pg_depend), MSSQL (dm_exec_describe_first_result_set),
// Oracle (ALL_DEPENDENCIES), MySQL 8.0+ (VIEW_TABLE_USAGE).
//
// Code-level heuristic (all databases): matches view columns to base table PKs
// by column name overlap. Works well with unique PK names (e.g., "customerid")
// but may not resolve when generic names like "id" appear in many tables.
//
// Skipped: SQLite, MariaDB (heuristic ties on test schema's generic "id" column),
// Snowflake (emulator limitation), MongoDB (document model).
func TestViewCursorPaginationCrossDB(t *testing.T) {
	switch dbType {
	case "sqlite", "mariadb", "snowflake", "mongodb":
		t.Skipf("skipping for %s (heuristic cannot resolve generic PK names in test schema)", dbType)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	// user_products view: SELECT u.id, u.full_name, p.id, p.name FROM users JOIN ...
	// The view should inherit 'id' as PK from the users base table, enabling
	// cursor-based pagination (first/last/after/before).
	res, err := gj.GraphQL(context.Background(),
		`query {
			user_products(first: 3) {
				id
				full_name
				product_name
			}
		}`, nil, nil)
	if err != nil {
		t.Fatalf("cursor pagination on view failed (%s): %v", dbType, err)
	}

	var result struct {
		UserProducts []struct {
			ID          int    `json:"id"`
			FullName    string `json:"full_name"`
			ProductName string `json:"product_name"`
		} `json:"user_products"`
	}
	if err := json.Unmarshal(res.Data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.UserProducts) == 0 {
		t.Fatal("expected non-empty results from view with cursor pagination")
	}
	if len(result.UserProducts) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(result.UserProducts))
	}
	t.Logf("View cursor pagination on %s returned %d rows", dbType, len(result.UserProducts))
}
