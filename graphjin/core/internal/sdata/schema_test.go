package sdata

import (
	"testing"
)

func TestAddRemoteRelParentLessIsBenign(t *testing.T) {
	cols := []DBColumn{
		{ID: 0, Schema: "public", Table: "users", Name: "id", Type: "bigint", PrimaryKey: true},
		{ID: 1, Schema: "public", Table: "users", Name: "email", Type: "text"},
	}
	di := NewDBInfo("postgres", 140000, "public", "db", cols, nil, nil)

	// Parent-less remote (top-level virtual table): Type "remote", but
	// PrimaryCol.FKeyTable is empty — there's no parent to attach to.
	orphan := NewDBTable("public", "audit_logs", "remote", nil)
	orphan.PrimaryCol = DBColumn{
		Schema:     "public",
		Table:      "audit_logs",
		Name:       "__audit_logs_pk",
		Type:       "text",
		PrimaryKey: true,
	}
	orphan.Args = []DBColumn{
		{Schema: "public", Table: "audit_logs", Name: "actor_id", Type: "text"},
	}
	di.AddTable(orphan)

	schema, err := NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("NewDBSchema with parent-less remote: %v", err)
	}

	got, err := schema.Find("public", "audit_logs")
	if err != nil {
		t.Fatalf("Find audit_logs: %v", err)
	}
	if got.Type != "remote" {
		t.Errorf("Type = %q, want remote", got.Type)
	}
	if len(got.Args) != 1 || got.Args[0].Name != "actor_id" {
		t.Errorf("Args = %+v", got.Args)
	}
}

func TestDBSchemaFunctionOverloadsAreOrderIndependent(t *testing.T) {
	functions := []DBFunction{
		{Schema: "public", Name: "populate", Type: "text", Inputs: []DBFuncParam{{ID: 1, Name: "input", Type: "text"}}},
		{Schema: "public", Name: "populate", Type: "integer", Inputs: []DBFuncParam{{ID: 1, Name: "input", Type: "integer"}}},
	}
	first := NewDBInfo("postgres", 160000, "public", "shop", nil, functions, nil)
	second := NewDBInfo("postgres", 160000, "public", "shop", nil, []DBFunction{functions[1], functions[0]}, nil)
	firstSchema, err := NewDBSchema(first, nil)
	if err != nil {
		t.Fatalf("build first schema: %v", err)
	}
	secondSchema, err := NewDBSchema(second, nil)
	if err != nil {
		t.Fatalf("build second schema: %v", err)
	}
	firstFunction := firstSchema.GetFunctions()["populate"]
	secondFunction := secondSchema.GetFunctions()["populate"]
	if firstFunction.Type != secondFunction.Type || dbFunctionSortKey(firstFunction) != dbFunctionSortKey(secondFunction) {
		t.Fatalf("overload selection changed with discovery order: first=%+v second=%+v", firstFunction, secondFunction)
	}
}

func TestAddRemoteRelWithParentStillWorks(t *testing.T) {
	cols := []DBColumn{
		{ID: 0, Schema: "public", Table: "users", Name: "id", Type: "bigint", PrimaryKey: true},
	}
	di := NewDBInfo("postgres", 140000, "public", "db", cols, nil, nil)

	rj := NewDBTable("public", "is_profile", "remote", nil)
	rj.PrimaryCol = DBColumn{
		Schema:     "public",
		Table:      "is_profile",
		Name:       "__is_profile_id",
		Type:       "bigint",
		PrimaryKey: true,
		FKeySchema: "public",
		FKeyTable:  "users",
		FKeyCol:    "id",
	}
	di.AddTable(rj)

	if _, err := NewDBSchema(di, nil); err != nil {
		t.Fatalf("row-join remote (parented) should still build: %v", err)
	}
}
