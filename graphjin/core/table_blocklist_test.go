package core

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestAddTablesAppliesColumnBlocklistToAlias(t *testing.T) {
	dbInfo := sdata.NewDBInfo("mysql", 80000, "app", "store", []sdata.DBColumn{
		{Schema: "app", Table: "legacy_orders", Name: "id", Type: "bigint"},
		{Schema: "app", Table: "legacy_orders", Name: "private_note", Type: "varchar"},
		{Schema: "app", Table: "legacy_orders", Name: "total", Type: "decimal"},
	}, nil, nil)
	conf := &Config{Tables: []Table{{
		Name:      "orders",
		Table:     "legacy_orders",
		Schema:    "app",
		Database:  "analytics",
		Blocklist: []string{" PRIVATE_NOTE ", "internal_code"},
	}}}

	if err := addTables(conf, dbInfo, "analytics"); err != nil {
		t.Fatalf("add tables: %v", err)
	}

	blocked, err := dbInfo.GetColumn("app", "legacy_orders", "private_note")
	if err != nil {
		t.Fatal(err)
	}
	if !blocked.Blocked {
		t.Fatal("expected aliased table blocklist to hide private_note")
	}
	visible, err := dbInfo.GetColumn("app", "legacy_orders", "total")
	if err != nil {
		t.Fatal(err)
	}
	if visible.Blocked {
		t.Fatal("table blocklist must not hide unrelated columns")
	}
}

func TestAddTablesAppliesColumnBlocklistToDirectTable(t *testing.T) {
	dbInfo := sdata.NewDBInfo("mysql", 80000, "app", "store", []sdata.DBColumn{
		{Schema: "app", Table: "orders", Name: "id", Type: "bigint"},
		{Schema: "app", Table: "orders", Name: "private_note", Type: "varchar"},
	}, nil, nil)
	conf := &Config{Tables: []Table{{
		Name:      "orders",
		Schema:    "app",
		Database:  "analytics",
		Blocklist: []string{"private_note"},
	}}}

	if err := addTables(conf, dbInfo, "analytics"); err != nil {
		t.Fatalf("add tables: %v", err)
	}
	column, err := dbInfo.GetColumn("app", "orders", "private_note")
	if err != nil {
		t.Fatal(err)
	}
	if !column.Blocked {
		t.Fatal("expected direct table blocklist to hide private_note")
	}
}
