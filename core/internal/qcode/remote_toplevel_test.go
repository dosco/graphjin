package qcode_test

import (
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
