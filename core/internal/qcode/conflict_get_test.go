package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func conflictGetCompiler(t *testing.T, compositePK bool, presetEmail bool) *qcode.Compiler {
	t.Helper()
	var cols []sdata.DBColumn
	if compositePK {
		cols = []sdata.DBColumn{
			{Schema: "public", Table: "memberships", Name: "account_id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Schema: "public", Table: "memberships", Name: "user_id", Type: "bigint", PrimaryKey: true, NotNull: true},
			{Schema: "public", Table: "memberships", Name: "name", Type: "text"},
		}
	} else {
		cols = []sdata.DBColumn{
			{Schema: "public", Table: "users", Name: "id", Type: "bigint", PrimaryKey: true, UniqueKey: true, NotNull: true},
			{Schema: "public", Table: "users", Name: "email", Type: "text", UniqueKey: true, NotNull: true},
			{Schema: "public", Table: "users", Name: "name", Type: "text"},
			{Schema: "public", Table: "profiles", Name: "id", Type: "bigint", PrimaryKey: true, UniqueKey: true, NotNull: true},
			{Schema: "public", Table: "profiles", Name: "user_id", Type: "bigint", FKeySchema: "public", FKeyTable: "users", FKeyCol: "id"},
		}
	}
	schema, err := sdata.NewDBSchema(sdata.NewDBInfo("postgres", 190000, "public", "db", cols, nil, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}
	if presetEmail {
		if err := compiler.AddRole("user", "public", "users", qcode.TRConfig{
			Query:  qcode.QueryConfig{Columns: []string{"id", "email", "name"}},
			Insert: qcode.InsertConfig{Columns: []string{"id", "email", "name"}, Presets: map[string]string{"email": "preset@example.com"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return compiler
}

func TestInsertConflictGetArgumentAliasesAndInference(t *testing.T) {
	for _, arg := range []string{"on_conflict", "onConflict"} {
		t.Run(arg, func(t *testing.T) {
			compiler := conflictGetCompiler(t, false, false)
			qc, err := compiler.Compile([]byte(`mutation { users(insert: { email: "ada@example.com", name: "Ada" }, `+arg+`: get) { id email name } }`), nil, "user", "")
			if err != nil {
				t.Fatal(err)
			}
			if qc.InsertConflictAction != qcode.ConflictGet || len(qc.Mutates) != 1 || len(qc.Mutates[0].ConflictCols) != 1 || qc.Mutates[0].ConflictCols[0].Col.Name != "email" {
				t.Fatalf("unexpected conflict metadata: %#v", qc.Mutates)
			}
		})
	}
}

func TestInsertConflictGetInferenceAfterPresets(t *testing.T) {
	compiler := conflictGetCompiler(t, false, true)
	qc, err := compiler.Compile([]byte(`mutation { users(insert: { name: "Ada" }, on_conflict: get) { id email } }`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := qc.Mutates[0].ConflictCols[0].Col.Name; got != "email" {
		t.Fatalf("expected preset email target, got %q", got)
	}
}

func TestInsertConflictGetRequiresExactlyOneCandidate(t *testing.T) {
	compiler := conflictGetCompiler(t, false, false)
	_, err := compiler.Compile([]byte(`mutation { users(insert: { name: "Ada" }, on_conflict: get) { id } }`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "requires a supplied primary or unique key") {
		t.Fatalf("unexpected missing-target error: %v", err)
	}

	_, err = compiler.Compile([]byte(`mutation { users(insert: { id: 1, email: "ada@example.com" }, on_conflict: get) { id } }`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "is ambiguous") {
		t.Fatalf("unexpected ambiguous-target error: %v", err)
	}
}

func TestInsertConflictGetCompositePrimaryKey(t *testing.T) {
	compiler := conflictGetCompiler(t, true, false)
	qc, err := compiler.Compile([]byte(`mutation { memberships(insert: { account_id: 1, user_id: 2, name: "Ada" }, on_conflict: get) { account_id user_id } }`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(qc.Mutates[0].ConflictCols); got != 2 {
		t.Fatalf("expected two primary-key columns, got %d", got)
	}
	_, err = compiler.Compile([]byte(`mutation { memberships(insert: { account_id: 1, name: "Ada" }, on_conflict: get) { account_id user_id } }`), nil, "user", "")
	if err == nil || !strings.Contains(err.Error(), "requires a supplied primary or unique key") {
		t.Fatalf("expected incomplete composite primary key error, got %v", err)
	}
}

func TestInsertConflictGetRejectsForbiddenForms(t *testing.T) {
	compiler := conflictGetCompiler(t, false, false)
	tests := []struct {
		name string
		gql  string
	}{
		{"bulk", `mutation { users(insert: [{ email: "a@example.com" }], on_conflict: get) { id } }`},
		{"nested", `mutation { users(insert: { id: 1, profiles: { id: 2 } }, on_conflict: get) { id } }`},
		{"update", `mutation { users(update: { name: "Ada" }, on_conflict: get) { id } }`},
		{"upsert", `mutation { users(upsert: { id: 1 }, on_conflict: get) { id } }`},
		{"delete", `mutation { users(delete: true, on_conflict: get) { id } }`},
		{"action", `mutation { users(insert: { email: "a@example.com" }, on_conflict: update) { id } }`},
		{"query", `query { users(on_conflict: get) { id } }`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := compiler.Compile([]byte(tt.gql), nil, "user", ""); err == nil {
				t.Fatal("expected compile error")
			}
		})
	}
}

func TestInsertConflictGetUsesReadPermissionsForReturningRow(t *testing.T) {
	compiler := conflictGetCompiler(t, false, false)
	if err := compiler.AddRole("reader", "public", "users", qcode.TRConfig{
		Query:  qcode.QueryConfig{Columns: []string{"id", "email"}, Filters: []string{"false"}},
		Insert: qcode.InsertConfig{Columns: []string{"email", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := compiler.Compile([]byte(`mutation { users(insert: { email: "ada@example.com", name: "Ada" }, on_conflict: get) { id email name } }`), nil, "reader", "")
	if err == nil || !strings.Contains(err.Error(), "db column blocked: name") {
		t.Fatalf("expected query-column permission error, got %v", err)
	}

	qc, err := compiler.Compile([]byte(`mutation { users(insert: { email: "ada@example.com", name: "Ada" }, on_conflict: get) { id email } }`), nil, "reader", "")
	if err != nil {
		t.Fatal(err)
	}
	root := qc.Selects[qc.Roots[0]]
	if root.Where.Exp == nil || root.Where.Exp.Op != qcode.OpFalse {
		t.Fatalf("expected the normal query filter on conflict-get return, got %#v", root.Where.Exp)
	}
	if !qc.InsertConflictReadFiltered {
		t.Fatal("expected conflict-get IR to record the role read filter")
	}
}
