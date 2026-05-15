package nanodb

import "testing"

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
