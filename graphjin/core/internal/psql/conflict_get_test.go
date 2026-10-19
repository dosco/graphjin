package psql_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func compileConflictGetSQL(t *testing.T, dbType string, dbVersion int) (*qcode.QCode, string, error) {
	t.Helper()
	cols := []sdata.DBColumn{
		{Schema: "public", Table: "users", Name: "id", Type: "bigint", PrimaryKey: true, UniqueKey: true, NotNull: true},
		{Schema: "public", Table: "users", Name: "email", Type: "text", UniqueKey: true, NotNull: true},
		{Schema: "public", Table: "users", Name: "name", Type: "text"},
	}
	schema, err := sdata.NewDBSchema(sdata.NewDBInfo(dbType, dbVersion, "public", "db", cols, nil, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	qcCompiler, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	qc, err := qcCompiler.Compile([]byte(`mutation { users(insert: { email: "ada@example.com", name: "Submitted" }, on_conflict: get) { id email name } }`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	_, sqlBytes, err := psql.NewCompiler(psql.Config{DBType: dbType, DBVersion: dbVersion}).CompileEx(qc)
	return qc, string(sqlBytes), err
}

func TestPostgres19InsertConflictGetSQL(t *testing.T) {
	qc, sql, err := compileConflictGetSQL(t, "postgres", 190000)
	if err != nil {
		t.Fatal(err)
	}
	if qc.InsertConflictFallback {
		t.Fatal("PG19 must use the native path")
	}
	for _, want := range []string{`ON CONFLICT ("email") DO SELECT`, `RETURNING "public"."users".*`} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected %q in SQL:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "DO UPDATE") {
		t.Fatalf("conflict get must not update rows:\n%s", sql)
	}
}

func TestPostgresPre19InsertConflictGetSQL(t *testing.T) {
	qc, sql, err := compileConflictGetSQL(t, "postgres", 160000)
	if err != nil {
		t.Fatal(err)
	}
	if !qc.InsertConflictFallback {
		t.Fatal("pre-19 PostgreSQL must mark the retryable fallback")
	}
	for _, want := range []string{`"_gj_inserted_0" AS (INSERT`, `ON CONFLICT ("email") DO NOTHING`, `UNION ALL SELECT * FROM "public"."users" AS "_gj_existing"`, `"_gj_existing"."email" IS NOT DISTINCT FROM`} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected %q in SQL:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "DO UPDATE") {
		t.Fatalf("conflict get must not update rows:\n%s", sql)
	}
}

func TestSQLiteInsertConflictGetSQL(t *testing.T) {
	qc, sql, err := compileConflictGetSQL(t, "sqlite", 3045000)
	if err != nil {
		t.Fatal(err)
	}
	if !qc.InsertConflictFallback {
		t.Fatal("SQLite must mark the retryable fallback")
	}
	for _, want := range []string{`CREATE TEMP TABLE IF NOT EXISTS _gj_conflicts`, `ON CONFLICT ("email") DO NOTHING`, `INSERT OR REPLACE INTO _gj_conflicts`, `json_extract((SELECT v FROM _gj_conflicts`, `DROP TABLE IF EXISTS _gj_conflicts`} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected %q in SQL:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "INSERT OR IGNORE INTO \"public\".\"users\"") || strings.Contains(sql, "DO UPDATE") {
		t.Fatalf("SQLite conflict get must be targeted and update-free:\n%s", sql)
	}
}

func TestInsertConflictGetUnsupportedDialect(t *testing.T) {
	_, _, err := compileConflictGetSQL(t, "mysql", 80000)
	if err == nil || !strings.Contains(err.Error(), "not supported by the mysql dialect") {
		t.Fatalf("unexpected unsupported-dialect error: %v", err)
	}
}

func TestInsertConflictGetObjectVariableSQL(t *testing.T) {
	cols := []sdata.DBColumn{
		{Schema: "public", Table: "users", Name: "id", Type: "bigint", PrimaryKey: true, UniqueKey: true, NotNull: true},
		{Schema: "public", Table: "users", Name: "email", Type: "text", UniqueKey: true, NotNull: true},
		{Schema: "public", Table: "users", Name: "name", Type: "text"},
	}
	schema, err := sdata.NewDBSchema(sdata.NewDBInfo("postgres", 160000, "public", "db", cols, nil, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	qcCompiler, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	qc, err := qcCompiler.Compile(
		[]byte(`mutation { users(insert: $data, on_conflict: get) { id email name } }`),
		map[string]json.RawMessage{"data": json.RawMessage(`{"email":"ada@example.com","name":"Ada"}`)},
		"user", "")
	if err != nil {
		t.Fatal(err)
	}

	for _, dbType := range []string{"postgres", "sqlite"} {
		t.Run(dbType, func(t *testing.T) {
			_, sqlBytes, err := psql.NewCompiler(psql.Config{DBType: dbType, DBVersion: 160000}).CompileEx(qc)
			if err != nil {
				t.Fatal(err)
			}
			sql := string(sqlBytes)
			if !strings.Contains(sql, `ON CONFLICT ("email")`) {
				t.Fatalf("missing targeted conflict clause:\n%s", sql)
			}
			if dbType == "postgres" && (!strings.Contains(sql, `json_to_record`) || !strings.Contains(sql, `IS NOT DISTINCT FROM`)) {
				t.Fatalf("missing PostgreSQL object-variable lookup:\n%s", sql)
			}
			if dbType == "sqlite" && (!strings.Contains(sql, `_gj_conflicts`) || !strings.Contains(sql, `json_extract`) || !strings.Contains(sql, `WHERE true ON CONFLICT`)) {
				t.Fatalf("missing SQLite object-variable lookup:\n%s", sql)
			}
		})
	}
}
