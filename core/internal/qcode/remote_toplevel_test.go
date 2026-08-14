package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func newToplevelRemoteSchema(t *testing.T) *sdata.DBSchema {
	t.Helper()

	cols := []sdata.DBColumn{
		{ID: 0, Schema: "public", Table: "users", Name: "id", Type: "bigint", PrimaryKey: true},
		{ID: 1, Schema: "public", Table: "users", Name: "email", Type: "text"},
	}
	di := sdata.NewDBInfo("postgres", 140000, "public", "db", cols, nil, nil)

	rt := sdata.NewDBTable("public", "audit_logs", "remote", []sdata.DBColumn{
		{ID: 0, Schema: "public", Table: "audit_logs", Name: "id", Type: "text"},
		{ID: 1, Schema: "public", Table: "audit_logs", Name: "action", Type: "text"},
	})
	rt.PrimaryCol = sdata.DBColumn{
		Schema:     "public",
		Table:      "audit_logs",
		Name:       "id",
		Type:       "text",
		PrimaryKey: true,
	}
	rt.Args = []sdata.DBColumn{
		{Schema: "public", Table: "audit_logs", Name: "actor_id", Type: "text"},
	}
	di.AddTable(rt)

	s, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("NewDBSchema: %v", err)
	}
	return s
}

func TestTopLevelRemoteIsMarkedAsRemote(t *testing.T) {
	co, err := qcode.NewCompiler(newToplevelRemoteSchema(t), qcode.Config{DefaultLimit: 5, DisableAgg: true, DisableFuncs: true})
	if err != nil {
		t.Fatal(err)
	}

	gql := `query { audit_logs(actorId: "u-7") { id action } }`
	qc, err := co.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if qc.Remotes != 1 {
		t.Errorf("qc.Remotes = %d, want 1", qc.Remotes)
	}
	if len(qc.Selects) == 0 {
		t.Fatalf("no selects produced")
	}
	sel := qc.Selects[0]
	if sel.Rel.Type != sdata.RelRemote {
		t.Errorf("Rel.Type = %v, want RelRemote", sel.Rel.Type)
	}
	if sel.SkipRender != qcode.SkipTypeRemote {
		t.Errorf("SkipRender = %v, want SkipTypeRemote", sel.SkipRender)
	}
	if got := sel.ExtraArgs["actorId"]; got != "u-7" {
		t.Errorf("ExtraArgs[actorId] = %q, want u-7", got)
	}
}

func TestUnknownArgOnRealTableStillErrors(t *testing.T) {
	co, err := qcode.NewCompiler(newToplevelRemoteSchema(t), qcode.Config{DefaultLimit: 5, DisableAgg: true, DisableFuncs: true})
	if err != nil {
		t.Fatal(err)
	}

	gql := `query { users(actorId: "u-7") { id email } }`
	if _, err := co.Compile([]byte(gql), nil, "user", ""); err == nil {
		t.Fatal("expected unknown-arg error on real table; got none")
	}
}

// TestRemoteUnknownColumnLenientByDefault pins the pass-through contract:
// remote tables without StrictColumns accept selections their registered
// columns don't list, because resolver-backed and OpenAPI tables may serve
// fields the schema never learned about (SynthesiseColumns can return nil).
func TestRemoteUnknownColumnLenientByDefault(t *testing.T) {
	co, err := qcode.NewCompiler(newToplevelRemoteSchema(t), qcode.Config{DefaultLimit: 5, DisableAgg: true, DisableFuncs: true})
	if err != nil {
		t.Fatal(err)
	}

	gql := `query { audit_logs(actorId: "u-7") { id action made_up_field } }`
	qc, err := co.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatalf("lenient remote table rejected an unknown selection: %v", err)
	}
	if got := len(qc.Selects[0].Fields); got != 3 {
		t.Errorf("fields = %d, want 3 (unknown field passes through)", got)
	}
}

// TestRemoteUnknownColumnStrictErrors pins the closed-surface contract used
// by filesystem tables: with StrictColumns set, a selection outside the
// registered columns fails at compile time with an error that names the
// column, the table, and the valid surface — the shape a model can
// self-correct from. Keyword fields (__typename, *_cursor) stay exempt.
func TestRemoteUnknownColumnStrictErrors(t *testing.T) {
	cols := []sdata.DBColumn{
		{ID: 0, Schema: "public", Table: "users", Name: "id", Type: "bigint", PrimaryKey: true},
	}
	di := sdata.NewDBInfo("postgres", 140000, "public", "db", cols, nil, nil)

	rt := sdata.NewDBTable("public", "policies", "remote", []sdata.DBColumn{
		{ID: 0, Schema: "public", Table: "policies", Name: "key", Type: "text", PrimaryKey: true},
		{ID: 1, Schema: "public", Table: "policies", Name: "url", Type: "text"},
	})
	rt.StrictColumns = true
	di.AddTable(rt)

	s, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("NewDBSchema: %v", err)
	}
	co, err := qcode.NewCompiler(s, qcode.Config{DefaultLimit: 5, DisableAgg: true, DisableFuncs: true})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := co.Compile([]byte(`query { policies { key severity } }`), nil, "user", ""); err == nil {
		t.Fatal("strict remote table accepted an unknown selection; want error")
	} else {
		for _, want := range []string{"severity", "policies", "key, url"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should contain %q", err, want)
			}
		}
	}

	if _, err := co.Compile([]byte(`query { policies { key __typename policies_cursor } }`), nil, "user", ""); err != nil {
		t.Fatalf("keyword selections must stay exempt from strict checks: %v", err)
	}

	if _, err := co.Compile([]byte(`query { policies { file: key url } }`), nil, "user", ""); err != nil {
		t.Fatalf("aliased valid column rejected: %v", err)
	}
}

func TestRemoteScalarArgsAccepted(t *testing.T) {
	co, err := qcode.NewCompiler(newToplevelRemoteSchema(t), qcode.Config{DefaultLimit: 5, DisableAgg: true, DisableFuncs: true})
	if err != nil {
		t.Fatal(err)
	}

	gql := `query { audit_logs(actorId: "u-7", page: 3, verbose: true) { id } }`
	qc, err := co.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	sel := qc.Selects[0]
	if sel.ExtraArgs["actorId"] != "u-7" || sel.ExtraArgs["page"] != "3" || sel.ExtraArgs["verbose"] != "true" {
		t.Errorf("ExtraArgs = %+v", sel.ExtraArgs)
	}
}
