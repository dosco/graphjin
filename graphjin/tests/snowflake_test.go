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
