package translate

import (
	"database/sql"
	"database/sql/driver"
	"os"
	"strings"
	"testing"

	"github.com/dosco/graphjin/tests/v3/hostedemu/snowflake/catalog"
	_ "github.com/duckdb/duckdb-go/v2"
)

func TestTranslateSetupCreatesExecutableDuckDBSeed(t *testing.T) {
	schema, err := catalog.ParseSeed("../../../snowflake.sql")
	if err != nil {
		t.Fatal(err)
	}
	seedSQL, err := os.ReadFile("../../../snowflake.sql")
	if err != nil {
		t.Fatal(err)
	}
	stmts, err := TranslateSetup(string(seedSQL), schema)
	if err != nil {
		t.Fatal(err)
	}

	var productsDDL string
	for _, stmt := range stmts {
		if strings.HasPrefix(stmt, "CREATE TABLE products ") {
			productsDDL = stmt
			break
		}
	}
	if productsDDL == "" {
		t.Fatal("products DDL not generated")
	}
	if !strings.Contains(productsDDL, "tags JSON") || !strings.Contains(productsDDL, "metadata JSON") || !strings.Contains(productsDDL, "price DECIMAL(10,2)") {
		t.Fatalf("JSON types not mapped in products DDL: %s", productsDDL)
	}
	if !strings.Contains(productsDDL, "created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL") {
		t.Fatalf("created_at default not preserved in products DDL: %s", productsDDL)
	}
	if strings.Contains(strings.ToUpper(productsDDL), "CLUSTER BY") {
		t.Fatalf("Snowflake CLUSTER BY leaked into DuckDB DDL: %s", productsDDL)
	}

	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%v\nSQL: %s", err, stmt)
		}
	}

	var productCount, hotProductCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM products").Scan(&productCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM hot_products").Scan(&hotProductCount); err != nil {
		t.Fatal(err)
	}
	if productCount != 100 || hotProductCount != 50 {
		t.Fatalf("seed counts = products:%d hot_products:%d", productCount, hotProductCount)
	}
}

func TestParseSeedStatements(t *testing.T) {
	seedSQL, err := os.ReadFile("../../../snowflake.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range catalog.SplitStatements(catalog.StripSQLComments(string(seedSQL))) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := ParseStatement(parseReadySQL(stmt)); err != nil {
			t.Fatalf("%v\nSQL:\n%s", err, stmt)
		}
	}
}

func TestTranslateRuntimeGraphJinJSONQuery(t *testing.T) {
	sql := `SELECT CAST(OBJECT_CONSTRUCT_KEEP_NULL('products', (SELECT COALESCE(ARRAY_AGG("__sj_0"."json"), ARRAY_CONSTRUCT()) AS json FROM (SELECT OBJECT_CONSTRUCT_KEEP_NULL('id', "__sr_0"."ID", 'count_likes', "__sr_0"."COUNT_LIKES") AS "json"FROM (SELECT "PRODUCTS"."ID", "PRODUCTS"."COUNT_LIKES" FROM "PUBLIC"."PRODUCTS" AS "PRODUCTS" ORDER BY "PRODUCTS"."ID" ASC LIMIT 3) AS "__sr_0") AS "__sj_0")) AS VARCHAR) AS "__root" FROM ((SELECT true)) AS "__root_x"`
	got, args, err := TranslateRuntime(sql, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 0 {
		t.Fatalf("args changed: %#v", args)
	}
	for _, want := range []string{
		"json_object('products'",
		`COALESCE(json_group_array("__sj_0"."json"), CAST('[]' AS JSON))`,
		"products.id",
		"products.count_likes",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("translated SQL missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"PUBLIC"`) || strings.Contains(got, `"PRODUCTS"`) || strings.Contains(got, "OBJECT_CONSTRUCT_KEEP_NULL") {
		t.Fatalf("Snowflake syntax leaked into translation:\n%s", got)
	}
}

func TestTranslateDirectListAggAndCasts(t *testing.T) {
	in := `SELECT LISTAGG("fk_column_name", ',') WITHIN GROUP (ORDER BY "key_sequence") AS "cols", TO_VARCHAR("ID"), PARSE_JSON(?), ?::VARIANT FROM TABLE(RESULT_SCAN(?))`
	args := []driver.NamedValue{
		{Ordinal: 1, Value: `{"id":1}`},
		{Ordinal: 2, Value: `{"x":2}`},
		{Ordinal: 3, Value: "QID_1"},
	}
	got, outArgs, err := TranslateDirect(in, args, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`string_agg("fk_column_name", ',' ORDER BY "key_sequence")`,
		"CAST(id AS VARCHAR)",
		"CAST(? AS JSON)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("translated SQL missing %q:\n%s", want, got)
		}
	}
	if len(outArgs) != len(args) {
		t.Fatalf("args length = %d, want %d", len(outArgs), len(args))
	}
}

func TestTranslateArraySliceAndFlatten(t *testing.T) {
	in := `SELECT ARRAY_SLICE(ARRAY_AGG("__sj_1"."json"), 0, 20), _gj.value::BIGINT FROM LATERAL FLATTEN(input => PARSE_JSON('[1,2,3]')) _gj`
	got, _, err := TranslateRuntime(in, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`json_group_array("__sj_1"."json")`,
		`json_each(CAST('[1,2,3]' AS JSON)) AS _gj`,
		`CAST(_gj.value AS BIGINT)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("translated SQL missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ARRAY_SLICE") || strings.Contains(got, "FLATTEN") {
		t.Fatalf("Snowflake array/table-function syntax leaked into translation:\n%s", got)
	}
}

func TestTranslateJSONPathAndGetPath(t *testing.T) {
	in := `SELECT TRY_PARSE_JSON("PAYLOAD"):id::NUMBER AS id, GET_PATH(PARSE_JSON(?), 'profile.name')::VARCHAR AS name FROM "PUBLIC"."EVENTS"`
	got, _, err := TranslateDirect(in, []driver.NamedValue{{Ordinal: 1, Value: `{"profile":{"name":"Ada"}}`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`CAST(json_extract(CAST(payload AS JSON), '$.id') AS DECIMAL) AS id`,
		`json_extract_string(CAST(? AS JSON), '$.profile.name') AS name`,
		`FROM events`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("translated SQL missing %q:\n%s", want, got)
		}
	}
}

func TestTranslateSnowflakeFunctions(t *testing.T) {
	in := `SELECT OBJECT_CONSTRUCT('json', ARRAY_CONSTRUCT()), ARRAYS_OVERLAP("TAGS", ARRAY_CONSTRUCT('Tag 1')), REGEXP_INSTR("DESCRIPTION", 'Product 3', 1, 1, 0, 'i'), GET(ARRAY_AGG("__cur_0"), COUNT(*) - 1) FROM "PUBLIC"."PRODUCTS"`
	got, _, err := TranslateRuntime(in, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`json_object('json', CAST('[]' AS JSON))`,
		`EXISTS (SELECT 1 FROM json_each(tags) AS _gj_ao_l JOIN json_each(json_array('Tag 1')) AS _gj_ao_r`,
		`regexp_matches(description, 'Product 3', 'i')`,
		`list_extract(list("__cur_0"), CAST(COUNT(*) AS BIGINT))`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("translated SQL missing %q:\n%s", want, got)
		}
	}
}

func TestTranslatePreservesQuotedJSONStringKeysAndContinuesAfterUnknownCast(t *testing.T) {
	in := `SELECT PARSE_JSON('{"UPPER": true}') AS metadata, "ID"::GEOGRAPHY AS geo, "NAME"::TEXT AS name FROM "PUBLIC"."PRODUCTS"`
	got, _, err := TranslateDirect(in, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`CAST('{"UPPER": true}' AS JSON) AS metadata`,
		`id::GEOGRAPHY AS geo`,
		`CAST(name AS VARCHAR) AS name`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("translated SQL missing %q:\n%s", want, got)
		}
	}
}
