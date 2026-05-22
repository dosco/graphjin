package catalog

import "testing"

func TestApplyDDLUpdatesSchema(t *testing.T) {
	s := &Schema{DBName: "DB", Schema: DefaultSchema, byName: make(map[string]*Table)}

	s.ApplyDDL(`CREATE TABLE "test_items" (
  "id" BIGINT NOT NULL PRIMARY KEY,
  "name" VARCHAR NOT NULL
);`)
	tbl := s.Table("test_items")
	if tbl == nil {
		t.Fatal("table not added")
	}
	if tbl.Column("id") == nil || !tbl.Column("id").PrimaryKey {
		t.Fatalf("primary key column not parsed: %#v", tbl.Column("id"))
	}

	s.ApplyDDL(`ALTER TABLE "test_items" ADD COLUMN "email" VARCHAR`)
	if tbl.Column("email") == nil {
		t.Fatal("column not added")
	}

	s.ApplyDDL(`ALTER TABLE "test_items" DROP COLUMN "email"`)
	if tbl.Column("email") != nil {
		t.Fatal("column not dropped")
	}

	s.ApplyDDL(`DROP TABLE IF EXISTS "test_items"`)
	if s.Table("test_items") != nil {
		t.Fatal("table not dropped")
	}
}
