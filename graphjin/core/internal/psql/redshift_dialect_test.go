package psql_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/dialect"
	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestRedshiftQueryCompileSmoke(t *testing.T) {
	sql := compileRedshiftSQL(t, `query { users(limit: 1, order_by: { id: asc }) { id email } }`)
	for _, want := range []string{`OBJECT_CONSTRUCT_KEEP_NULL`, `ARRAY_AGG`, `"users"."id"`, `ORDER BY "users"."id" ASC LIMIT 1`} {
		if !strings.Contains(sql, want) {
			t.Fatalf("compiled SQL missing %q:\n%s", want, sql)
		}
	}
	assertRedshiftHostedEmulatorSQL(t, sql)
}

func TestRedshiftSearchCompileLimited(t *testing.T) {
	sql := compileRedshiftSQL(t, `query { users(search: $query) { id email } }`)
	for _, want := range []string{`"users"."email" ILIKE ('%' || ? || '%')`} {
		if !strings.Contains(sql, want) {
			t.Fatalf("compiled SQL missing %q:\n%s", want, sql)
		}
	}
	assertRedshiftHostedEmulatorSQL(t, sql)
}

func TestRedshiftGISCompileLimited(t *testing.T) {
	sql := compileRedshiftSQL(t, `query {
		users(where: { shape: { st_dwithin: { point: [-122.4194, 37.7749], distance: 1000 } } }) {
			id
			email
		}
	}`)
	for _, want := range []string{`ST_DWithin("users"."shape"`, `ST_GeomFromText('POINT(-122.419400 37.774900)', 4326)`, `1000.000000`} {
		if !strings.Contains(sql, want) {
			t.Fatalf("compiled SQL missing %q:\n%s", want, sql)
		}
	}
	assertRedshiftHostedEmulatorSQL(t, sql)
}

func TestRedshiftInsertCompileRequiresSuppliedPK(t *testing.T) {
	sql := compileRedshiftSQL(t, `mutation { users(insert: {id: 99, email: "new@example.com"}) { id email } }`)
	for _, want := range []string{`CREATE TEMP TABLE _gj_ids`, `INSERT INTO "public"."users"`, `INSERT INTO _gj_ids`, `SELECT id FROM _gj_ids WHERE k = 'users_0'`} {
		if !strings.Contains(sql, want) {
			t.Fatalf("compiled SQL missing %q:\n%s", want, sql)
		}
	}
	for _, disallowed := range []string{` RETURNING `, `ON CONFLICT`} {
		if strings.Contains(sql, disallowed) {
			t.Fatalf("redshift insert SQL should not contain %q:\n%s", disallowed, sql)
		}
	}
}

func TestRedshiftInsertCompileRejectsIdentityOnlyResult(t *testing.T) {
	_, err := compileRedshiftSQLResult(t, `mutation { users(insert: {email: "identity@example.com"}) { id email } }`)
	if err == nil {
		t.Fatal("expected supplied primary-key error")
	}
	if !strings.Contains(err.Error(), "client-supplied primary key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRedshiftUpdateCompilePKBased(t *testing.T) {
	sql := compileRedshiftSQL(t, `mutation { users(id: 1, update: {phone: "555-0100"}) { id phone } }`)
	for _, want := range []string{`INSERT INTO _gj_ids`, `UPDATE "public"."users" SET "phone" = CAST('555-0100' AS VARCHAR)`, `WHERE (("users"."id") = 1)`} {
		if !strings.Contains(sql, want) {
			t.Fatalf("compiled SQL missing %q:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, ` RETURNING `) {
		t.Fatalf("redshift update SQL should not use RETURNING:\n%s", sql)
	}
}

func TestRedshiftDeleteCompileUsesPreDeleteSnapshot(t *testing.T) {
	sql := compileRedshiftSQL(t, `mutation { users(id: 1, delete: true) { id email } }`)
	for _, want := range []string{`CREATE TEMP TABLE "_gj_redshift_deleted_users_0" AS SELECT * FROM "public"."users" AS "users" WHERE (("users"."id") = 1)`, `DELETE FROM "public"."users" WHERE "id" IN (SELECT "id" FROM "_gj_redshift_deleted_users_0")`, `FROM "_gj_redshift_deleted_users_0" AS "_gj_redshift_deleted_users_0"`, `DROP TABLE IF EXISTS "_gj_redshift_deleted_users_0"`} {
		if !strings.Contains(sql, want) {
			t.Fatalf("compiled SQL missing %q:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, ` RETURNING `) {
		t.Fatalf("redshift delete SQL should not use RETURNING:\n%s", sql)
	}
}

func TestRedshiftSubscriptionCompileBatched(t *testing.T) {
	md, sql, err := compileRedshiftSQLWithMetadata(t, `subscription { users(id: $id) { id email phone } }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"_gj_sub"."id"`, `OBJECT_CONSTRUCT_KEEP_NULL`, `"users"."phone"`} {
		if !strings.Contains(sql, want) {
			t.Fatalf("compiled subscription SQL missing %q:\n%s", want, sql)
		}
	}
	for _, disallowed := range []string{`json_array_elements`, `$1::json`, `action='`} {
		if strings.Contains(sql, disallowed) {
			t.Fatalf("redshift subscription SQL should not contain %q:\n%s", disallowed, sql)
		}
	}

	wrapped := renderRedshiftSubscriptionWrap(md, sql)
	for _, want := range []string{`WITH _gj_sub AS`, `JSON_PARSE(?)`, `UNNEST(_gj_sub_input._gj_params) WITH OFFSET`, `CAST(_gj_sub_unboxed.value[0] AS BIGINT) AS "id"`, `ORDER BY "__gj_sub_order"`} {
		if !strings.Contains(wrapped, want) {
			t.Fatalf("redshift subscription wrapper missing %q:\n%s", want, wrapped)
		}
	}
	for _, disallowed := range []string{`json_array_elements`, `$1::json`, `LEFT OUTER JOIN LATERAL`} {
		if strings.Contains(wrapped, disallowed) {
			t.Fatalf("redshift subscription wrapper should not contain %q:\n%s", disallowed, wrapped)
		}
	}
}

func compileRedshiftSQL(t *testing.T, gql string) string {
	t.Helper()
	sql, err := compileRedshiftSQLResult(t, gql)
	if err != nil {
		t.Fatal(err)
	}
	return sql
}

func compileRedshiftSQLResult(t *testing.T, gql string) (string, error) {
	t.Helper()
	_, sql, err := compileRedshiftSQLWithMetadata(t, gql)
	return sql, err
}

func compileRedshiftSQLWithMetadata(t *testing.T, gql string) (psql.Metadata, string, error) {
	t.Helper()
	cols := []sdata.DBColumn{
		{Schema: "public", Table: "users", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
		{Schema: "public", Table: "users", Name: "email", Type: "character varying", FullText: true},
		{Schema: "public", Table: "users", Name: "phone", Type: "character varying"},
		{Schema: "public", Table: "users", Name: "shape", Type: "geometry"},
		{Schema: "public", Table: "users", Name: "created_at", Type: "timestamp"},
	}
	di := sdata.NewDBInfo("redshift", 0, "public", "dev", cols, nil, nil)
	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatal(err)
	}
	qc, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	pc := psql.NewCompiler(psql.Config{DBType: "redshift"})

	reqQC, err := qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		return psql.Metadata{}, "", err
	}
	md, sqlBytes, err := pc.CompileEx(reqQC)
	if err != nil {
		return psql.Metadata{}, "", err
	}
	return md, string(sqlBytes), nil
}

func assertRedshiftHostedEmulatorSQL(t *testing.T, sql string) {
	t.Helper()
	if strings.Contains(sql, "action='") {
		t.Fatalf("redshift hosted-emulator SQL should not have action comment:\n%s", sql)
	}
}

func renderRedshiftSubscriptionWrap(md psql.Metadata, innerSQL string) string {
	params := make([]dialect.Param, len(md.Params()))
	for i, p := range md.Params() {
		params[i] = dialect.Param{Name: p.Name, Type: p.Type}
	}
	ctx := &redshiftSubscriptionWrapContext{}
	(&dialect.RedshiftDialect{}).RenderSubscriptionUnbox(ctx, params, innerSQL)
	return ctx.String()
}

type redshiftSubscriptionWrapContext struct {
	bytes.Buffer
}

func (c *redshiftSubscriptionWrapContext) Write(s string) (int, error) {
	return c.Buffer.WriteString(s)
}

func (c *redshiftSubscriptionWrapContext) WriteString(s string) (int, error) {
	return c.Buffer.WriteString(s)
}

func (c *redshiftSubscriptionWrapContext) AddParam(dialect.Param) string {
	return ""
}

func (c *redshiftSubscriptionWrapContext) Quote(s string) {
	c.WriteString(`"`)
	c.WriteString(strings.ReplaceAll(s, `"`, `""`))
	c.WriteString(`"`)
}

func (c *redshiftSubscriptionWrapContext) ColWithTable(table, col string) {
	if table != "" {
		c.Quote(table)
		c.WriteString(".")
	}
	c.Quote(col)
}

func (c *redshiftSubscriptionWrapContext) RenderJSONFields(*qcode.Select) {}

func (c *redshiftSubscriptionWrapContext) IsTableMutated(string) bool { return false }

func (c *redshiftSubscriptionWrapContext) RenderExp(sdata.DBTable, *qcode.Exp) {}

func (c *redshiftSubscriptionWrapContext) GetStaticVar(string) (string, bool) { return "", false }

func (c *redshiftSubscriptionWrapContext) GetSecPrefix() string { return "" }

func (c *redshiftSubscriptionWrapContext) SetError(error) {}
