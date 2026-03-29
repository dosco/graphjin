package tests_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/require"
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
	if !strings.Contains(sql, "CREATE OR REPLACE TEMP TABLE _gj_ids_") {
		t.Fatalf("expected scoped _gj_ids temp table, got SQL: %s", sql)
	}
	if !strings.Contains(sql, "CREATE OR REPLACE TEMP TABLE _gj_prev_ids_") {
		t.Fatalf("expected scoped _gj_prev_ids temp table, got SQL: %s", sql)
	}
}

func TestSnowflakeMutationTempTablesAreRetrySafe(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	gql := `mutation {
		users(id: 90, update: { full_name: "retry-safe" }) {
			id
		}
	}`

	exp, err := gj.ExplainQuery(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	sql := exp.CompiledQuery
	if !strings.Contains(sql, "CREATE OR REPLACE TEMP TABLE _gj_ids_") {
		t.Fatalf("expected retry-safe _gj_ids temp table setup, got SQL: %s", sql)
	}
	if !strings.Contains(sql, "CREATE OR REPLACE TEMP TABLE _gj_prev_ids_") {
		t.Fatalf("expected retry-safe _gj_prev_ids temp table setup, got SQL: %s", sql)
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

func TestSnowflakeMutationUpdateConnectDisconnectStability(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	gql := `mutation {
		users(id: $id, update: $data) {
			full_name
			products {
				id
			}
		}
	}`

	vars := json.RawMessage(`{
		"id": 100,
		"data": {
			"full_name": "Updated user 100",
			"products": {
				"connect": { "id": 99 },
				"disconnect": { "id": 100 }
			}
		}
	}`)

	runSnowflakeMutationStabilityTest(t, gql, vars,
		`{"users":{"full_name":"Updated user 100","products":[{"id":99}]}}`,
		func() {
			_, _ = db.Exec(`UPDATE products SET owner_id = 99 WHERE id = 99`)
			_, _ = db.Exec(`UPDATE products SET owner_id = 100 WHERE id = 100`)
			_, _ = db.Exec(`UPDATE users SET full_name = 'User 100' WHERE id = 100`)
		})
}

func TestSnowflakeMutationConnectDisconnectSkipsDeadIDCapture(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	gql := `mutation {
		users(id: $id, update: $data) {
			full_name
			products {
				id
			}
		}
	}`

	vars := json.RawMessage(`{
		"id": 100,
		"data": {
			"full_name": "Updated user 100",
			"products": {
				"connect": { "id": 99 },
				"disconnect": { "id": 100 }
			}
		}
	}`)

	exp, err := gj.ExplainQuery(gql, vars, "user")
	if err != nil {
		t.Fatal(err)
	}

	sql := exp.CompiledQuery
	disconnectCapture := `SELECT 'products_2', "products"."owner_id" FROM "products" WHERE (("products"."id") = 100)`
	connectCapture := `SELECT 'products_3', "products"."owner_id" FROM "products" WHERE (("products"."id") = 99)`
	disconnectUpdate := `UPDATE "main"."products" SET "owner_id" = NULL WHERE (("products"."id") = 100)`
	connectUpdate := `UPDATE "main"."products" SET "owner_id" = (SELECT id FROM _gj_ids_`

	if strings.Contains(sql, disconnectCapture) {
		t.Fatalf("expected Snowflake disconnect to skip dead _gj_ids capture, got SQL: %s", sql)
	}
	if strings.Contains(sql, connectCapture) {
		t.Fatalf("expected Snowflake connect to skip dead _gj_ids capture, got SQL: %s", sql)
	}
	if !strings.Contains(sql, disconnectUpdate) {
		t.Fatalf("expected Snowflake disconnect update to remain, got SQL: %s", sql)
	}
	if !strings.Contains(sql, connectUpdate) {
		t.Fatalf("expected Snowflake connect update to remain, got SQL: %s", sql)
	}
}

func TestSnowflakeMutationUpdateRelatedTableStability(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	gql := `mutation {
		users(id: $id, update: $data) {
			full_name
			products {
				id
			}
		}
	}`

	vars := json.RawMessage(`{
		"id": 90,
		"data": {
			"full_name": "Updated user 90",
			"products": {
				"where": { "id": { "gt": 1 } },
				"name": "Updated Product 90"
			}
		}
	}`)

	runSnowflakeMutationStabilityTest(t, gql, vars,
		`{"users":{"full_name":"Updated user 90","products":[{"id":90}]}}`,
		func() {
			_, _ = db.Exec(`UPDATE users SET full_name = 'User 90' WHERE id = 90`)
			_, _ = db.Exec(`UPDATE products SET name = 'Product 90' WHERE id = 90`)
		})
}

func TestSnowflakeMutationUpdateMultipleRelatedTablesStability(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
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

	runSnowflakeMutationStabilityTest(t, gql, vars,
		`{"purchases":{"customer":{"full_name":"Updated user related to purchase 100"},"product":{"description":"Updated product related to purchase 100"},"quantity":6}}`,
		func() {
			_, _ = db.Exec(`UPDATE purchases SET quantity = 1000 WHERE id = 100`)
			_, _ = db.Exec(`UPDATE users SET full_name = 'User 1' WHERE id = 1`)
			_, _ = db.Exec(`UPDATE products SET description = 'Description for product 100' WHERE id = 100`)
		})
}

func TestSnowflakeMutationChildUpdateCaptureSkipsUnusedJSONJoin(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	gql := `mutation {
		users(id: $id, update: $data) {
			full_name
			products {
				id
			}
		}
	}`

	vars := json.RawMessage(`{
		"id": 90,
		"data": {
			"full_name": "Updated user 90",
			"products": {
				"where": { "id": { "gt": 1 } },
				"name": "Updated Product 90"
			}
		}
	}`)

	exp, err := gj.ExplainQuery(gql, vars, "user")
	if err != nil {
		t.Fatal(err)
	}

	sql := exp.CompiledQuery
	childInsertNoJoin := `SELECT 'products_1', "products"."id" FROM "main"."products" AS "products" WHERE`
	childInsertWithJoin := `SELECT 'products_1', "products"."id" FROM "main"."products" AS "products", (SELECT`

	if !strings.Contains(sql, childInsertNoJoin) {
		t.Fatalf("expected child update ID capture without JSON join, got SQL: %s", sql)
	}
	if strings.Contains(sql, childInsertWithJoin) {
		t.Fatalf("expected child update ID capture to skip JSON join, got SQL: %s", sql)
	}
	if strings.Contains(sql, `"products"."owner_id" = (SELECT "id" FROM "users" WHERE "id" =`) {
		t.Fatalf("expected child update ID capture to use captured parent ID directly, got SQL: %s", sql)
	}
	if !strings.Contains(sql, `"products"."owner_id" = (SELECT id FROM _gj_ids_`) {
		t.Fatalf("expected child update ID capture to use the captured Snowflake parent ID directly, got SQL: %s", sql)
	}
}

func TestSnowflakeMutationChildUpdateStringExpressionIsCasted(t *testing.T) {
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
	castedUpdate := `"full_name" = CAST(json_extract_string(?, '$.customer.full_name') AS VARCHAR)`
	bareUpdate := `"full_name" = json_extract_string(?, '$.customer.full_name')`

	if !strings.Contains(sql, castedUpdate) {
		t.Fatalf("expected child string update to use CAST(json_extract_string(... ) AS VARCHAR), got SQL: %s", sql)
	}
	if strings.Contains(sql, bareUpdate) {
		t.Fatalf("expected child string update to avoid bare json_extract_string in SET, got SQL: %s", sql)
	}
}

func TestSnowflakeMutationChildUpdateNonPKParentJoinUsesExists(t *testing.T) {
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
	scalarJoin := `"users"."id" = (SELECT "customer_id" FROM "purchases" WHERE "id" =`
	existsJoin := `EXISTS (SELECT 1 FROM "purchases" WHERE "users"."id" = "purchases"."customer_id" AND "purchases"."id" = (SELECT id FROM _gj_ids_`

	if strings.Contains(sql, scalarJoin) {
		t.Fatalf("expected Snowflake child update to avoid scalar parent-column subquery, got SQL: %s", sql)
	}
	if !strings.Contains(sql, existsJoin) {
		t.Fatalf("expected Snowflake child update to use EXISTS for non-PK parent join, got SQL: %s", sql)
	}
}

func TestSnowflakeMutationSetArrayEmptyStability(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	gql := `mutation {
		products(where: { id: 100 }, update: { tags: [] }) {
			id
			tags
		}
	}`

	runSnowflakeMutationStabilityTest(t, gql, nil,
		`{"products":[{"id":100,"tags":[]}]}`,
		func() {
			_, _ = db.Exec(`UPDATE products SET tags = list_value('Tag 1', 'Tag 2', 'Tag 3', 'Tag 4', 'Tag 5') WHERE id = 100`)
		})
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

func runSnowflakeMutationStabilityTest(
	t *testing.T,
	gql string,
	vars json.RawMessage,
	expected string,
	cleanup func(),
) {
	t.Helper()

	const attempts = 5

	for i := 0; i < attempts; i++ {
		conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
		gj, err := core.NewGraphJin(conf, db)
		require.NoError(t, err, "iteration %d: new graphjin", i+1)

		ctx := context.WithValue(context.Background(), core.UserIDKey, 3)
		res, gqlErr := gj.GraphQL(ctx, gql, vars, nil)
		cleanup()

		if gqlErr != nil {
			logSnowflakeQueryFailure(gj, gql, vars, gqlErr)
		}

		require.NoError(t, gqlErr, "iteration %d: graphql execution", i+1)
		require.JSONEq(t, expected, string(res.Data), "iteration %d: response mismatch", i+1)
	}
}
