package bigquerylive

import (
	"strings"
	"testing"
)

func TestNormalizeSeedStatementsStripsSelfReferencingFK(t *testing.T) {
	stmts := normalizeSeedStatements(`
CREATE TABLE comments (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  reply_to_id INT64 REFERENCES comments(id) NOT ENFORCED
)`)
	if len(stmts) != 1 {
		t.Fatalf("expected one statement, got %d statements: %#v", len(stmts), stmts)
	}
	if strings.Contains(stmts[0], "REFERENCES comments(id)") {
		t.Fatalf("create statement still has self reference:\n%s", stmts[0])
	}
}

func TestNormalizeSeedStatementsPreservesNonSelfReferences(t *testing.T) {
	stmts := normalizeSeedStatements(`
CREATE TABLE products (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  owner_id INT64 REFERENCES users(id) NOT ENFORCED
)`)
	if len(stmts) != 1 {
		t.Fatalf("expected one statement, got %#v", stmts)
	}
	if !strings.Contains(stmts[0], "REFERENCES users(id)") {
		t.Fatalf("non-self reference was removed:\n%s", stmts[0])
	}
}

func TestSQLRowCountsInfersFixtureCounts(t *testing.T) {
	counts := SQLRowCounts(`
INSERT INTO users (id) SELECT i FROM UNNEST(GENERATE_ARRAY(1, 100)) AS i;
INSERT INTO products (id, name) VALUES (1, 'a'), (2, 'b'), (3, 'c');
`)
	if counts["users"] != 100 {
		t.Fatalf("users count = %d, want 100", counts["users"])
	}
	if counts["products"] != 3 {
		t.Fatalf("products count = %d, want 3", counts["products"])
	}
}

func TestTableStorageResponseUsesFixtureRows(t *testing.T) {
	st := &state{
		datasetID: "dataset_a",
		tableRows: seedTableRows("dataset_a", map[string]uint64{
			"users":              100,
			"dataset_a.products": 3,
		}),
	}

	res, err := st.tableStorageRows(nil, "dataset_a")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(res.Rows); got != 2 {
		t.Fatalf("rows = %d, want 2", got)
	}

	one, err := st.tableStorageRow(nil, "dataset_a", "users")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(one.Rows); got != 1 {
		t.Fatalf("single table rows = %d, want 1", got)
	}

	rollup, err := st.tableStorageNamespaceRows("dataset_a")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(rollup.Rows); got != 1 {
		t.Fatalf("namespace rows = %d, want 1", got)
	}

	missing, err := st.tableStorageRow(nil, "dataset_a", "missing")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(missing.Rows); got != 0 {
		t.Fatalf("missing table rows = %d, want 0", got)
	}
}
