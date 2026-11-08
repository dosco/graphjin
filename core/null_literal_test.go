package core

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// A GraphQL null literal parses as the label null, and the linear mutation
// path (SQLite, MySQL, MSSQL, Oracle) quoted its text: update { note: null }
// stored the string 'null'. A null variable was always fine.
func TestNullLiteralWritesSQLNullSQLite(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT, score INTEGER);
INSERT INTO notes VALUES (1, 'kept', 3);`); err != nil {
		t.Fatal(err)
	}
	gj, err := NewGraphJin(&Config{DBType: "sqlite", DisableAllowList: true}, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gj.Close)
	stored := func(id int) (body, score string) {
		t.Helper()
		if err := db.QueryRow(`SELECT quote(body), quote(score) FROM notes WHERE id = ?`, id).Scan(&body, &score); err != nil {
			t.Fatal(err)
		}
		return body, score
	}
	for _, q := range []string{
		`mutation { notes(where: { id: { eq: 1 } }, update: { body: null, score: null }) { id } }`,
		`mutation { notes(insert: { id: 2, body: null, score: null }) { id } }`,
	} {
		if _, err := gj.GraphQL(context.Background(), q, nil, nil); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, id := range []int{1, 2} {
		if body, score := stored(id); body != "NULL" || score != "NULL" {
			t.Fatalf("row %d stored body=%s score=%s, want SQL NULL", id, body, score)
		}
	}
	// A string that spells null is still a string.
	if _, err := gj.GraphQL(context.Background(), `mutation { notes(where: { id: { eq: 1 } }, update: { body: "null" }) { id } }`, nil, nil); err != nil {
		t.Fatal(err)
	}
	if body, _ := stored(1); body != "'null'" {
		t.Fatalf("the string \"null\" stored %s", body)
	}
}
