package snowflakeemu

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestParseSeedSnowflakeDDL(t *testing.T) {
	schema, err := ParseSeed("../snowflake.sql")
	if err != nil {
		t.Fatal(err)
	}

	users := schema.Table("users")
	if users == nil {
		t.Fatal("users table not parsed")
	}
	if col := users.Column("email"); col == nil || !col.UniqueKey || !col.NotNull {
		t.Fatalf("email metadata not parsed: %#v", col)
	}

	products := schema.Table("products")
	if products == nil {
		t.Fatal("products table not parsed")
	}
	if col := products.Column("owner_id"); col == nil || col.FKeyTable != "users" || col.FKeyColumn != "id" {
		t.Fatalf("owner_id FK not parsed: %#v", col)
	}

	orderItems := schema.Table("order_items")
	if orderItems == nil {
		t.Fatal("order_items table not parsed")
	}
	if col := orderItems.Column("variant_id"); col == nil || col.FKeyTable != "product_variants" || col.FKeyColumn != "variant_id" {
		t.Fatalf("composite FK not parsed: %#v", col)
	}

	events := schema.Table("events")
	if events == nil || strings.Join(events.ClusteringKeys, ",") != "event_time,region" {
		t.Fatalf("clustering keys not parsed: %#v", events)
	}

	view := schema.Table("hot_products")
	if view == nil || !view.IsView || len(view.Columns) != 2 || view.Columns[0].Name != "product_id" {
		t.Fatalf("hot_products view not parsed: %#v", view)
	}
}

func TestConnectorSatisfiesSnowflakeDiscovery(t *testing.T) {
	db := sql.OpenDB(NewConnector(Config{
		SeedPath:   "../snowflake.sql",
		CaptureDir: t.TempDir(),
		TestName:   "discovery",
		RunID:      "unit",
		Backend:    BackendDuckDB,
		Fallback:   FallbackStrict,
	}))
	defer db.Close()

	assertSnowflakeDiscovery(t, db)
}

func assertSnowflakeDiscovery(t *testing.T, db *sql.DB) {
	t.Helper()

	conf := &core.Config{
		DBType:               "snowflake",
		DisableAllowList:     true,
		DBSchemaPollDuration: -1,
	}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	if !gj.SchemaReady() {
		t.Fatal("schema not ready")
	}
	products, err := gj.GetTableSchema("products")
	if err != nil {
		t.Fatal(err)
	}
	if products.PrimaryKey != "id" {
		t.Fatalf("products primary key = %q, want id", products.PrimaryKey)
	}
	var foundOwnerFK bool
	for _, col := range products.Columns {
		if col.Name == "owner_id" && strings.Contains(col.ForeignKey, "users.id") {
			foundOwnerFK = true
		}
	}
	if !foundOwnerFK {
		t.Fatalf("products owner_id FK missing: %#v", products.Columns)
	}

	events, err := gj.GetTableSchema("events")
	if err != nil {
		t.Fatal(err)
	}
	if events.PartitionKey != "event_time" {
		t.Fatalf("events partition key = %q, want event_time", events.PartitionKey)
	}
}

func TestDuckDBBackendStrictExampleQuery(t *testing.T) {
	db := sql.OpenDB(NewConnector(Config{
		SeedPath: "../snowflake.sql",
		Backend:  BackendDuckDB,
		Fallback: FallbackStrict,
		TestName: "strict-example",
		RunID:    "unit",
	}))
	defer db.Close()

	conf := &core.Config{
		DBType:               "snowflake",
		DisableAllowList:     true,
		DBSchemaPollDuration: -1,
	}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(), `
	query {
		products(limit: 3, order_by: { id: asc }) {
			id
			count_likes
			owner {
				id
				fullName: full_name
			}
		}
	}`, nil, nil)
	if err != nil {
		t.Fatalf("%v\nSQL:\n%s", err, res.SQL())
	}
	if !bytes.Contains(res.Data, []byte(`"products"`)) || !bytes.Contains(res.Data, []byte(`"fullName":"User 1"`)) {
		t.Fatalf("unexpected result: %s", res.Data)
	}
}

func TestCaptureJSONL(t *testing.T) {
	dir := t.TempDir()
	db := sql.OpenDB(NewConnector(Config{
		SeedPath:   "../snowflake.sql",
		CaptureDir: dir,
		TestName:   "capture test",
		RunID:      "unit",
		Backend:    BackendDuckDB,
	}))
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), "SHOW IMPORTED KEYS IN SCHEMA"); err != nil {
		t.Fatal(err)
	}
	var qid string
	if err := db.QueryRowContext(context.Background(), "SELECT LAST_QUERY_ID()").Scan(&qid); err != nil {
		t.Fatal(err)
	}
	if qid == "" {
		t.Fatal("empty query id")
	}
	var b []byte
	if err := db.QueryRowContext(context.Background(), `SELECT OBJECT_CONSTRUCT_KEEP_NULL('id', 1)`).Scan(&b); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "capture_test.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("capture line count = %d, want 3\n%s", len(lines), data)
	}
	var ev struct {
		RunID         string `json:"run_id"`
		Test          string `json:"test"`
		Seq           int64  `json:"seq"`
		Op            string `json:"op"`
		Phase         string `json:"phase"`
		NormalizedSQL string `json:"normalized_sql"`
		SQLHash       string `json:"sql_hash"`
		Backend       string `json:"backend"`
		Result        string `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.RunID != "unit" || ev.Test != "capture test" || ev.Seq != 3 {
		t.Fatalf("unexpected capture metadata: %#v", ev)
	}
	if ev.Op != "query" || ev.Phase != "runtime" || ev.Backend != BackendDuckDB || ev.Result != "rows:1" || ev.SQLHash == "" {
		t.Fatalf("unexpected capture event: %#v", ev)
	}
}

func TestCaptureJSONLDuckDBExecutionError(t *testing.T) {
	dir := t.TempDir()
	db := sql.OpenDB(NewConnector(Config{
		SeedPath:   "../snowflake.sql",
		CaptureDir: dir,
		TestName:   "duck capture",
		RunID:      "unit",
		Backend:    BackendDuckDB,
		Fallback:   FallbackPlaceholderOnError,
	}))
	defer db.Close()

	var value string
	if err := db.QueryRowContext(context.Background(), `SELECT "MISSING"."ID" FROM "PUBLIC"."MISSING" AS "MISSING" WHERE "MISSING"."ID" = ?`, 1).Scan(&value); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "duck_capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var ev struct {
		Backend        string `json:"backend"`
		TranslatedSQL  string `json:"translated_sql"`
		TranslatedArgs []any  `json:"translated_args"`
		ExecutionError string `json:"execution_error"`
		Result         string `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(data), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Backend != BackendDuckDB || ev.TranslatedSQL == "" || len(ev.TranslatedArgs) != 1 || ev.ExecutionError == "" || ev.Result != "rows:1" {
		t.Fatalf("unexpected duckdb capture event: %#v", ev)
	}
}
