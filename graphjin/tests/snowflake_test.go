package tests_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestSnowflakeConnectorInit(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, Debug: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	if gj == nil {
		t.Fatal("expected non-nil graphjin instance")
	}
}

func TestSnowflakeMutationTempTablesUseScopedNames(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	gql := `mutation {
		purchases(id: $id, update: $data) {
			quantity
			customer {
				full_name
			}
			product {
				description
			}
		}
	}`

	vars := json.RawMessage(`{
		"id": 100,
		"data": {
			"quantity": 6,
			"customer": {
				"full_name": "Updated user related to purchase 100"
			},
			"product": {
				"description": "Updated product related to purchase 100"
			}
		}
	}`)

	exp, err := gj.ExplainQuery(gql, vars, "user")
	if err != nil {
		t.Fatal(err)
	}

	sql := exp.CompiledQuery
	if strings.Contains(sql, "CREATE TEMP TABLE _gj_ids (") {
		t.Fatalf("expected scoped Snowflake temp table name, got SQL: %s", sql)
	}
	if strings.Contains(sql, "CREATE TEMP TABLE _gj_prev_ids (") {
		t.Fatalf("expected scoped Snowflake temp table name, got SQL: %s", sql)
	}
	if !strings.Contains(sql, "CREATE TEMP TABLE _gj_ids_") {
		t.Fatalf("expected scoped _gj_ids temp table, got SQL: %s", sql)
	}
	if !strings.Contains(sql, "CREATE TEMP TABLE _gj_prev_ids_") {
		t.Fatalf("expected scoped _gj_prev_ids temp table, got SQL: %s", sql)
	}
}

func TestSnowflakeMutationTempTablesDifferAcrossInstances(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})

	gj1, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	gj2, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	gql := `mutation {
		products(where: { id: 100 }, update: { tags: ["super", "great", "wow"] }) {
			id
			tags
		}
	}`

	exp1, err := gj1.ExplainQuery(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}
	exp2, err := gj2.ExplainQuery(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	name1 := extractSnowflakeTempTable(exp1.CompiledQuery, "_gj_ids_")
	name2 := extractSnowflakeTempTable(exp2.CompiledQuery, "_gj_ids_")
	if name1 == "" || name2 == "" {
		t.Fatalf("expected scoped Snowflake temp table names, got %q and %q", name1, name2)
	}
	if name1 == name2 {
		t.Fatalf("expected per-instance Snowflake temp table names to differ, got %q", name1)
	}
}

func extractSnowflakeTempTable(sql, prefix string) string {
	idx := strings.Index(sql, prefix)
	if idx == -1 {
		return ""
	}

	end := idx
	for end < len(sql) {
		ch := sql[end]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			end++
			continue
		}
		break
	}
	return sql[idx:end]
}
