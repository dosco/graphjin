package nanodb

import (
	"errors"
	"testing"
)

func TestSnapshotIndexesSearchAndRefresh(t *testing.T) {
	db, err := New(Snapshot{Tables: []Table{{
		Name: "gj_catalog",
		Columns: []Column{
			{Name: "id", Type: "text", PrimaryKey: true},
			{Name: "kind", Type: "text", Index: true},
			{Name: "parent_id", Type: "text", Index: true, FKeyTable: "gj_catalog", FKeyColumn: "id"},
			{Name: "title", Type: "text", FullText: true},
			{Name: "summary", Type: "text", FullText: true},
		},
		Rows: []Row{
			{"id": "table:default.public.users", "kind": "table", "title": "users", "summary": "Application users"},
			{"id": "column:default.public.users.email", "kind": "column", "parent_id": "table:default.public.users", "title": "email", "summary": "User email address"},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	snap := db.Snapshot()
	rows, ok := snap.Rows(DefaultSchema, "gj_catalog")
	if !ok || len(rows) != 2 {
		t.Fatalf("rows = %d, ok=%v", len(rows), ok)
	}
	if rank := snap.SearchRank(DefaultSchema, "gj_catalog", rows[1], "email user"); rank <= 0 {
		t.Fatalf("expected positive search rank, got %v", rank)
	}
	info := snap.DBInfo("graphjin")
	if _, err := info.GetColumn(DefaultSchema, "gj_catalog", "parent_id"); err != nil {
		t.Fatalf("expected parent_id column in dbinfo: %v", err)
	}

	if err := db.Refresh(Snapshot{Tables: []Table{{
		Name: "gj_catalog",
		Columns: []Column{
			{Name: "id", Type: "text", PrimaryKey: true},
			{Name: "kind", Type: "text", Index: true},
			{Name: "title", Type: "text", FullText: true},
		},
		Rows: []Row{{"id": "workflow:daily", "kind": "workflow", "title": "daily revenue workflow"}},
	}}}); err != nil {
		t.Fatal(err)
	}
	rows, ok = db.Snapshot().Rows(DefaultSchema, "gj_catalog")
	if !ok || len(rows) != 1 || rows[0]["id"] != "workflow:daily" {
		t.Fatalf("refresh rows = %#v, ok=%v", rows, ok)
	}
}

func TestUpdateReplaceRowsPublishesAtomically(t *testing.T) {
	db, err := New(Snapshot{Tables: []Table{
		{
			Name: "gj_catalog",
			Columns: []Column{
				{Name: "id", Type: "text", PrimaryKey: true},
				{Name: "kind", Type: "text", Index: true},
				{Name: "owner_source", Type: "text", Index: true},
				{Name: "title", Type: "text", FullText: true},
			},
			Rows: []Row{
				{"id": "source:app", "kind": "source", "owner_source": "app", "title": "app"},
				{"id": "table:app.public.users", "kind": "table", "owner_source": "app", "title": "users"},
				{"id": "table:billing.public.invoices", "kind": "table", "owner_source": "billing", "title": "invoices"},
			},
		},
		{
			Name:    "gj_config",
			Columns: []Column{{Name: "id", Type: "text", PrimaryKey: true}, {Name: "catalog_revision", Type: "text"}},
			Rows:    []Row{{"id": "current", "catalog_revision": "old"}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	err = db.Update(func(tx *Update) error {
		if err := tx.ReplaceRows("", "gj_catalog", func(row Row) bool {
			return row["owner_source"] == "app"
		}, []Row{
			{"id": "source:app", "kind": "source", "owner_source": "app", "title": "app updated"},
			{"id": "table:app.public.accounts", "kind": "table", "owner_source": "app", "title": "accounts"},
		}); err != nil {
			return err
		}
		return tx.ReplaceTable("", "gj_config", []Row{{"id": "current", "catalog_revision": "new"}})
	})
	if err != nil {
		t.Fatal(err)
	}

	snap := db.Snapshot()
	rows, ok := snap.Rows(DefaultSchema, "gj_catalog")
	if !ok || len(rows) != 3 {
		t.Fatalf("catalog rows = %d, ok=%v", len(rows), ok)
	}
	ids := map[string]bool{}
	for _, row := range rows {
		ids[valueKey(row["id"])] = true
	}
	for _, want := range []string{"source:app", "table:app.public.accounts", "table:billing.public.invoices"} {
		if !ids[want] {
			t.Fatalf("missing row %s after update: %#v", want, rows)
		}
	}
	if ids["table:app.public.users"] {
		t.Fatalf("old app table survived update: %#v", rows)
	}
	var accounts Row
	for _, row := range rows {
		if row["id"] == "table:app.public.accounts" {
			accounts = row
			break
		}
	}
	if rank := snap.SearchRank(DefaultSchema, "gj_catalog", accounts, "accounts"); rank <= 0 {
		t.Fatalf("expected touched table search index to include accounts, got %v", rank)
	}
	configRows, ok := snap.Rows(DefaultSchema, "gj_config")
	if !ok || len(configRows) != 1 || configRows[0]["catalog_revision"] != "new" {
		t.Fatalf("config rows = %#v, ok=%v", configRows, ok)
	}
}

func TestUpdateErrorLeavesSnapshotUnchanged(t *testing.T) {
	db, err := New(Snapshot{Tables: []Table{{
		Name:    "gj_catalog",
		Columns: []Column{{Name: "id", Type: "text", PrimaryKey: true}, {Name: "title", Type: "text", FullText: true}},
		Rows:    []Row{{"id": "table:app.public.users", "title": "users"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	before := db.Snapshot()
	wantErr := errors.New("stop")
	err = db.Update(func(tx *Update) error {
		if err := tx.ReplaceTable("", "gj_catalog", []Row{{"id": "table:app.public.accounts", "title": "accounts"}}); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("update err = %v, want %v", err, wantErr)
	}
	after := db.Snapshot()
	if after != before {
		t.Fatal("failed update should not publish a new snapshot")
	}
	rows, ok := after.Rows(DefaultSchema, "gj_catalog")
	if !ok || len(rows) != 1 || rows[0]["id"] != "table:app.public.users" {
		t.Fatalf("snapshot changed after failed update: %#v, ok=%v", rows, ok)
	}
}
