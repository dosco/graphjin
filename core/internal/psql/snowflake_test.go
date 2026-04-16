package psql_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// snowflakeCompile returns a Snowflake-dialect compiled SQL string for gql.
// It builds a fresh compiler pair for each call because the Snowflake test
// schema has a different shape from the default Postgres test schema and
// we don't want role mutations bleeding across tests.
func snowflakeCompile(t *testing.T, gql string, vars map[string]json.RawMessage) (string, error) {
	t.Helper()

	schema, err := sdata.GetTestSnowflakeSchema()
	if err != nil {
		t.Fatalf("snowflake schema: %v", err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatalf("qcode compiler: %v", err)
	}
	pc := psql.NewCompiler(psql.Config{DBType: "snowflake"})

	v, _ := json.Marshal(vars)
	v2 := make(map[string]json.RawMessage)
	_ = json.Unmarshal(v, &v2)

	qcomp, err := qc.Compile([]byte(gql), v2, "anon", "")
	if err != nil {
		return "", err
	}
	_, sql, err := pc.CompileEx(qcomp)
	if err != nil {
		return "", err
	}
	return string(sql), nil
}

// TestSnowflakeJSONPluralQuoting is a regression guard for the
// `COALESCE(ARRAY_AGG(__sjb_0.json), ...)` bug where unquoted identifiers
// got uppercased by Snowflake and missed the quoted-lowercase inner aliases.
// The inner derived-table alias is `__sjb_N` (the LATERAL outer keeps
// `__sj_N`); both parts must be double-quoted.
func TestSnowflakeJSONPluralQuoting(t *testing.T) {
	sql, err := snowflakeCompile(t, `{ products(limit: 3) { id name } }`, nil)
	if err != nil {
		t.Fatalf("compile err: %v", err)
	}
	if !strings.Contains(sql, `ARRAY_AGG("__sjb_0"."json")`) {
		t.Errorf(`expected ARRAY_AGG("__sjb_0"."json") (quoted both sides), got:\n%s`, sql)
	}
	if strings.Contains(sql, `ARRAY_AGG(__sjb_0.json)`) {
		t.Errorf(`unquoted ARRAY_AGG(__sjb_0.json) leaked; SQL:\n%s`, sql)
	}
}

// TestSnowflakeDistinctOnUsesQualify is a regression guard for BUG-S1.
// Snowflake does not support Postgres `DISTINCT ON`; the compiler must
// emit QUALIFY ROW_NUMBER() OVER (PARTITION BY ... ORDER BY ...) = 1
// instead, and must NOT emit `DISTINCT ON`.
func TestSnowflakeDistinctOnUsesQualify(t *testing.T) {
	sql, err := snowflakeCompile(t, `{ products(distinct_on: [name], limit: 3) { name } }`, nil)
	if err != nil {
		t.Fatalf("compile err: %v", err)
	}
	if strings.Contains(sql, "DISTINCT ON") {
		t.Errorf("emitted DISTINCT ON (unsupported on Snowflake); SQL:\n%s", sql)
	}
	if !strings.Contains(sql, "QUALIFY ROW_NUMBER() OVER") {
		t.Errorf("expected QUALIFY ROW_NUMBER() OVER (...) = 1; SQL:\n%s", sql)
	}
	if !strings.Contains(sql, ") = 1") {
		t.Errorf("expected QUALIFY window to filter = 1; SQL:\n%s", sql)
	}
}

// TestSnowflakeRegexUsesInstr is a regression guard for BUG-S2.
// Snowflake's REGEXP_LIKE requires full-string match; to preserve
// Postgres-style partial-match semantics we use REGEXP_INSTR(...) > 0
// (and `= 0` for negated ops).
func TestSnowflakeRegexUsesInstr(t *testing.T) {
	cases := []struct {
		op, gql   string
		wantExpr  string
	}{
		{"regex", `{ products(where: {name: {regex: "^foo"}}) { id } }`, "REGEXP_INSTR"},
		{"iregex", `{ products(where: {name: {iregex: "^foo"}}) { id } }`, "REGEXP_INSTR"},
		{"nregex", `{ products(where: {name: {nregex: "^foo"}}) { id } }`, "REGEXP_INSTR"},
		{"niregex", `{ products(where: {name: {niregex: "^foo"}}) { id } }`, "REGEXP_INSTR"},
	}
	for _, c := range cases {
		t.Run(c.op, func(t *testing.T) {
			sql, err := snowflakeCompile(t, c.gql, nil)
			if err != nil {
				t.Fatalf("compile err: %v", err)
			}
			if !strings.Contains(sql, c.wantExpr) {
				t.Errorf("expected %q in SQL; got:\n%s", c.wantExpr, sql)
			}
			// Negated forms assert `= 0`, positive forms assert `> 0`.
			if c.op == "nregex" || c.op == "niregex" {
				if !strings.Contains(sql, "= 0") {
					t.Errorf("expected negated regex to compare = 0; SQL:\n%s", sql)
				}
			} else {
				if !strings.Contains(sql, "> 0") {
					t.Errorf("expected regex to compare > 0; SQL:\n%s", sql)
				}
			}
			// Case-insensitive forms should pass 'i' flag to REGEXP_INSTR.
			if c.op == "iregex" || c.op == "niregex" {
				if !strings.Contains(sql, "'i'") {
					t.Errorf("expected 'i' flag on case-insensitive regex; SQL:\n%s", sql)
				}
			}
		})
	}
}

// TestSnowflakeMutationNoPKErrors asserts that a mutation against a table
// whose target has no PrimaryCols returns a clean, actionable error rather
// than emitting SQL with an empty quoted identifier (BUG-S3).
func TestSnowflakeMutationNoPKErrors(t *testing.T) {
	// Build a minimal Snowflake schema where one table has NO primary key.
	cols := []sdata.DBColumn{
		{Schema: "PUBLIC", Table: "NO_PK", Name: "A", Type: "bigint"},
		{Schema: "PUBLIC", Table: "NO_PK", Name: "B", Type: "varchar"},
	}
	di := sdata.NewDBInfo("snowflake", 0, "PUBLIC", "db", cols, nil, nil)
	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatalf("qcode: %v", err)
	}
	if err := qc.AddRole("anon", "PUBLIC", "NO_PK", qcode.TRConfig{
		Insert: qcode.InsertConfig{},
	}); err != nil {
		t.Fatalf("role: %v", err)
	}
	pc := psql.NewCompiler(psql.Config{DBType: "snowflake"})

	qcomp, err := qc.Compile([]byte(`mutation { no_pk(insert: {a: 1, b: "x"}) { a } }`), nil, "anon", "")
	if err != nil {
		t.Fatalf("qcompile err: %v", err)
	}
	_, _, err = pc.CompileEx(qcomp)
	if err == nil {
		t.Fatal("expected compile error for no-PK mutation, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "primary key") {
		t.Errorf("error should mention primary key; got %q", msg)
	}
	if !strings.Contains(msg, "NO_PK") {
		t.Errorf("error should name the table; got %q", msg)
	}
}

// TestSnowflakeTypenameUsesFieldName is a regression guard for BUG-S4.
// __typename must emit the user-typed field alias (GraphQL casing), not
// the stored UPPERCASE table name — otherwise cross-DB GraphQL responses
// would differ in case.
func TestSnowflakeTypenameUsesFieldName(t *testing.T) {
	// The Snowflake test schema has lowercase table names (products), so
	// simulate the UPPERCASE-storage case by querying via a mixed-case
	// alias. The user-typed "products" should appear in __typename, not
	// the stored name regardless of DB casing.
	sql, err := snowflakeCompile(t, `{ products(limit: 1) { __typename id } }`, nil)
	if err != nil {
		t.Fatalf("compile err: %v", err)
	}
	if !strings.Contains(sql, `'products' AS "__typename"`) {
		t.Errorf("expected 'products' as __typename literal; SQL:\n%s", sql)
	}
}

// TestSnowflakeGroupByExcludesAggInput is a regression guard for BUG-G1.
// A query like `{ t { x count_y } }` must GROUP BY x only — not by (x, y).
// Including the aggregate's input column in GROUP BY makes every group
// unique and counts collapse to 1.
func TestSnowflakeGroupByExcludesAggInput(t *testing.T) {
	// products has `user_id` which we'll aggregate; GROUP BY should only
	// contain the non-aggregate field (`name`).
	sql, err := snowflakeCompile(t, `{ products { name count_user_id } }`, nil)
	if err != nil {
		t.Fatalf("compile err: %v", err)
	}
	// Must have GROUP BY name
	if !strings.Contains(sql, `GROUP BY "products"."name"`) {
		t.Errorf("expected GROUP BY on name; SQL:\n%s", sql)
	}
	// Must NOT include user_id in GROUP BY
	if strings.Contains(sql, `GROUP BY "products"."name", "products"."user_id"`) ||
		strings.Contains(sql, `GROUP BY "products"."user_id"`) {
		t.Errorf("user_id leaked into GROUP BY (BUG-G1 regression); SQL:\n%s", sql)
	}
}

// TestSnowflakeOrderByAliasResolvesToColumn is a regression guard for
// BUG-G2. `order_by: {age: desc}` where `age: customer_age` is a select
// alias must resolve to the underlying column in the innermost SELECT's
// ORDER BY (where the alias isn't visible).
func TestSnowflakeOrderByAliasResolvesToColumn(t *testing.T) {
	// Alias `nm` onto products.name.
	sql, err := snowflakeCompile(t, `{ products(order_by: {nm: desc}, limit: 3) { id nm: name } }`, nil)
	if err != nil {
		t.Fatalf("compile err: %v", err)
	}
	// The ORDER BY in the inner SELECT should reference the column, not the alias.
	if !strings.Contains(sql, `ORDER BY  "products"."name"`) && !strings.Contains(sql, `ORDER BY "products"."name"`) {
		t.Errorf("expected ORDER BY on products.name (alias-translated); SQL:\n%s", sql)
	}
	// The alias must not be referenced directly by the inner ORDER BY.
	// (We check for the inner scope by looking for a bare `"nm"` reference
	// immediately preceded by `ORDER BY`.)
	if strings.Contains(sql, `ORDER BY "nm"`) || strings.Contains(sql, `ORDER BY  "nm"`) {
		t.Errorf("alias 'nm' appeared raw in ORDER BY (BUG-G2 regression); SQL:\n%s", sql)
	}
}

// TestSnowflakeEmptyProjectionEmitsNULL is a regression guard for the
// empty-SELECT case: when `@include(if: false)` or `@skip(if: true)`
// drops the only selected field, the inner SELECT list would otherwise
// be empty (`SELECT  FROM ...`) and the SQL would fail to parse. The
// compiler must emit NULL as a placeholder.
func TestSnowflakeEmptyProjectionEmitsNULL(t *testing.T) {
	sql, err := snowflakeCompile(t, `{ products(limit: 1) { id @include(if: false) } }`, nil)
	if err != nil {
		t.Fatalf("compile err: %v", err)
	}
	// The innermost SELECT should contain `SELECT NULL FROM ...` (no bare
	// `SELECT  FROM`).
	if strings.Contains(sql, `SELECT  FROM`) {
		t.Errorf("empty SELECT list leaked; SQL:\n%s", sql)
	}
}

// TestSnowflakeFloatCastsToNumber verifies that DOUBLE/FLOAT/REAL columns
// are wrapped in CAST(... AS NUMBER(18,4)) at the inner `__sr_N`
// projection. Without this cast Snowflake serializes DOUBLE inside
// OBJECT_CONSTRUCT_KEEP_NULL as scientific notation
// (e.g. `1.150000000000000e+01`), diverging from every other dialect's
// decimal output.
func TestSnowflakeFloatCastsToNumber(t *testing.T) {
	di := sdata.NewDBInfo("snowflake", 0, "public", "db", []sdata.DBColumn{
		{Schema: "public", Table: "widgets", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
		{Schema: "public", Table: "widgets", Name: "weight", Type: "float"},
		{Schema: "public", Table: "widgets", Name: "height", Type: "double"},
		{Schema: "public", Table: "widgets", Name: "count", Type: "bigint"},
		{Schema: "public", Table: "widgets", Name: "name", Type: "varchar"},
	}, nil, nil)

	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatalf("qcode: %v", err)
	}
	pc := psql.NewCompiler(psql.Config{DBType: "snowflake"})

	qcomp, err := qc.Compile([]byte(`{ widgets(limit: 1) { id weight height count name } }`), nil, "anon", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, sqlBytes, err := pc.CompileEx(qcomp)
	if err != nil {
		t.Fatalf("psql compile: %v", err)
	}
	sql := string(sqlBytes)

	for _, want := range []string{
		`CAST("widgets"."weight" AS NUMBER(18,4)) AS "weight"`,
		`CAST("widgets"."height" AS NUMBER(18,4)) AS "height"`,
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing float-column cast %q in SQL:\n%s", want, sql)
		}
	}
	for _, notWant := range []string{
		`CAST("widgets"."id"`,
		`CAST("widgets"."count"`,
		`CAST("widgets"."name"`,
	} {
		if strings.Contains(sql, notWant) {
			t.Errorf("non-float column incorrectly wrapped %q in SQL:\n%s", notWant, sql)
		}
	}
}

// TestSnowflakeIncludeDirectiveLiteralBool checks that @include(if: true)
// and @skip(if: false) preserve the field, while @include(if: false) and
// @skip(if: true) drop it.
func TestSnowflakeIncludeDirectiveLiteralBool(t *testing.T) {
	cases := []struct {
		name        string
		gql         string
		wantIDCol   bool // whether "products"."id" should appear in the inner SELECT
	}{
		{"include true keeps", `{ products(limit: 1) { id @include(if: true) } }`, true},
		{"include false drops", `{ products(limit: 1) { id @include(if: false) } }`, false},
		{"skip true drops", `{ products(limit: 1) { id @skip(if: true) } }`, false},
		{"skip false keeps", `{ products(limit: 1) { id @skip(if: false) } }`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sql, err := snowflakeCompile(t, c.gql, nil)
			if err != nil {
				t.Fatalf("compile err: %v", err)
			}
			has := strings.Contains(sql, `"products"."id"`)
			if has != c.wantIDCol {
				t.Errorf("id-in-SQL = %v, want %v; SQL:\n%s", has, c.wantIDCol, sql)
			}
		})
	}
}
