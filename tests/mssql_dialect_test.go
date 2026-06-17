package tests_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mssqlPascalFixture creates a small PascalCase multi-word schema
// (OrderGroups 1—* OrderLines) used to exercise MSSQL identifier
// normalization that the snake_case shared fixtures cannot. Group 1 has 5
// lines, group 2 has 1 line. Returns a cleanup func.
func mssqlPascalFixture(t *testing.T, db *sql.DB) func() {
	t.Helper()
	stmts := []string{
		`IF OBJECT_ID('OrderLines','U') IS NOT NULL DROP TABLE OrderLines`,
		`IF OBJECT_ID('OrderGroups','U') IS NOT NULL DROP TABLE OrderGroups`,
		`CREATE TABLE OrderGroups (GroupKey INT PRIMARY KEY, GroupName NVARCHAR(100) NOT NULL)`,
		`CREATE TABLE OrderLines (
			LineKey INT PRIMARY KEY,
			GroupKey INT NOT NULL,
			LineLabel NVARCHAR(100) NOT NULL,
			CONSTRAINT order_lines_group_fk FOREIGN KEY (GroupKey) REFERENCES OrderGroups(GroupKey)
		)`,
		`INSERT INTO OrderGroups (GroupKey, GroupName) VALUES (1, N'Group One'), (2, N'Group Two')`,
		`INSERT INTO OrderLines (LineKey, GroupKey, LineLabel) VALUES
			(1,1,N'L1'),(2,1,N'L2'),(3,1,N'L3'),(4,1,N'L4'),(5,1,N'L5'),(6,2,N'L6')`,
	}
	for _, s := range stmts {
		_, err := db.Exec(s)
		require.NoError(t, err, "fixture setup: %s", s)
	}
	return func() {
		_, _ = db.Exec(`IF OBJECT_ID('OrderLines','U') IS NOT NULL DROP TABLE OrderLines`)
		_, _ = db.Exec(`IF OBJECT_ID('OrderGroups','U') IS NOT NULL DROP TABLE OrderGroups`)
	}
}

// TestMSSQLPascalCaseNaming verifies that multi-word PascalCase tables and
// columns are reachable under their snake_case GraphQL names and that the FK
// relationship resolves. Without consistent ToSnake table normalization,
// OrderGroups registers as "ordergroups" and the snake_case root is not found.
func TestMSSQLPascalCaseNaming(t *testing.T) {
	if dbType != "mssql" {
		t.Skipf("skipping MSSQL naming test for %s", dbType)
	}
	cleanup := mssqlPascalFixture(t, db)
	defer cleanup()

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	ctx := context.Background()

	t.Run("table_and_column_roots_resolve", func(t *testing.T) {
		res, err := gj.GraphQL(ctx, `query {
			g: order_groups(order_by: { group_key: asc }) { gk: group_key gn: group_name }
			l: order_lines(limit: 3, order_by: { line_key: asc }) { lk: line_key }
		}`, nil, nil)
		require.NoError(t, err, "snake_case roots for PascalCase tables must resolve")

		var out struct {
			G []struct {
				GK int    `json:"gk"`
				GN string `json:"gn"`
			} `json:"g"`
			L []struct {
				LK int `json:"lk"`
			} `json:"l"`
		}
		require.NoError(t, json.Unmarshal(res.Data, &out))
		require.Len(t, out.G, 2)
		assert.Equal(t, "Group One", out.G[0].GN, "group_name -> GroupName round-trip")
		assert.Len(t, out.L, 3, "order_lines root resolves and limit applies")
	})

	t.Run("fk_relationship_join", func(t *testing.T) {
		res, err := gj.GraphQL(ctx, `query {
			g: order_groups(where: { group_key: { eq: 1 } }) {
				gk: group_key
				lines: order_lines(order_by: { line_key: asc }) { lk: line_key }
			}
		}`, nil, nil)
		require.NoError(t, err, "FK join order_groups -> order_lines must resolve")

		var out struct {
			G []struct {
				GK    int `json:"gk"`
				Lines []struct {
					LK int `json:"lk"`
				} `json:"lines"`
			} `json:"g"`
		}
		require.NoError(t, json.Unmarshal(res.Data, &out))
		require.Len(t, out.G, 1)
		assert.Len(t, out.G[0].Lines, 5, "group 1 has 5 child lines (ground truth)")
	})

	// Guards #605: a child-relationship limit must cap nested rows.
	t.Run("nested_child_limit", func(t *testing.T) {
		res, err := gj.GraphQL(ctx, `query {
			g: order_groups(where: { group_key: { eq: 1 } }) {
				lines: order_lines(limit: 2, order_by: { line_key: asc }) { lk: line_key }
			}
		}`, nil, nil)
		require.NoError(t, err)

		var out struct {
			G []struct {
				Lines []struct {
					LK int `json:"lk"`
				} `json:"lines"`
			} `json:"g"`
		}
		require.NoError(t, json.Unmarshal(res.Data, &out))
		require.Len(t, out.G, 1)
		assert.Len(t, out.G[0].Lines, 2, "nested limit:2 must cap the 5 child lines to 2")
	})
}

// TestMSSQLVariableLimitUnderAnalytics guards #604: with analytics_mode on, a
// variable limit (limit: $n) must still apply instead of returning all rows.
func TestMSSQLVariableLimitUnderAnalytics(t *testing.T) {
	if dbType != "mssql" {
		t.Skipf("skipping MSSQL variable-limit test for %s", dbType)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, AnalyticsMode: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	var total int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM products`).Scan(&total))
	require.Greater(t, total, 3, "need more than 3 products for the limit to matter")

	res, err := gj.GraphQL(context.Background(), `query ($l: Int) {
		p: products(unrestricted: true, limit: $l, order_by: { id: asc }) { id }
	}`, json.RawMessage(`{"l":3}`), nil)
	require.NoError(t, err)

	var out struct {
		P []struct {
			ID int `json:"id"`
		} `json:"p"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &out))
	assert.Len(t, out.P, 3, "variable limit must apply under analytics_mode, not return all rows")
}
