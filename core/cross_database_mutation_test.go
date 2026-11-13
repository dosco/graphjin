package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// crossDatabaseEngine is two SQLite databases behind one engine: tickets in
// "app" and notes in "ledger". There is no transaction across them, which is
// the whole reason the tests below exist.
type crossDatabaseEngine struct {
	gj     *GraphJin
	app    *sql.DB
	ledger *sql.DB
}

func newCrossDatabaseEngine(t *testing.T, configure func(*Config), options ...Option) crossDatabaseEngine {
	t.Helper()
	open := func(name, ddl string) *sql.DB {
		db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), name+".db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
		return db
	}
	app := open("app", `CREATE TABLE tickets (id INTEGER PRIMARY KEY, status TEXT NOT NULL);
INSERT INTO tickets VALUES (1, 'open'), (2, 'open');`)
	ledger := open("ledger", `CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL);
INSERT INTO notes VALUES (1, 'first');`)

	conf := &Config{
		DisableAllowList: true,
		Databases: map[string]DatabaseConfig{
			"app":    {Type: "sqlite"},
			"ledger": {Type: "sqlite"},
		},
	}
	if configure != nil {
		configure(conf)
	}
	options = append([]Option{OptionSetDatabases(map[string]*sql.DB{"app": app, "ledger": ledger})}, options...)
	gj, err := NewGraphJin(conf, app, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gj.Close)
	return crossDatabaseEngine{gj: gj, app: app, ledger: ledger}
}

func (e crossDatabaseEngine) ticketStatus(t *testing.T, id int) string {
	t.Helper()
	var status string
	if err := e.app.QueryRow(`SELECT status FROM tickets WHERE id = ?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func (e crossDatabaseEngine) noteCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.ledger.QueryRow(`SELECT count(*) FROM notes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

const crossDatabaseWrite = `mutation {
	notes(insert: { id: 2, body: "sorted out" }) { id }
	tickets(where: { id: { eq: 1 } }, update: { status: "resolved" }) { id status }
}`

// Both halves run, each as its database's own script, and the answer carries
// both roots.
func TestCrossDatabaseMutationWritesBothDatabases(t *testing.T) {
	e := newCrossDatabaseEngine(t, nil)
	res, err := e.gj.GraphQL(context.Background(), crossDatabaseWrite, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := string(res.Data)
	if !strings.Contains(got, `"tickets"`) || !strings.Contains(got, `"resolved"`) || !strings.Contains(got, `"notes"`) {
		t.Fatalf("the answer is missing a root: %s", got)
	}
	if status := e.ticketStatus(t, 1); status != "resolved" {
		t.Fatalf("ticket 1 is %q, want resolved", status)
	}
	if n := e.noteCount(t); n != 2 {
		t.Fatalf("the ledger has %d notes, want 2", n)
	}
	// The connections the scripts ran on are clean: the same write runs again.
	if _, err := e.gj.GraphQL(context.Background(),
		`mutation { tickets(where: { id: { eq: 2 } }, update: { status: "resolved" }) { id status } }`, nil, nil); err != nil {
		t.Fatalf("a write after the cross-database mutation failed: %v", err)
	}
}

// A mutation that is going to be refused is refused before any of it runs.
//
// It used to run every database's half at once and report the errors
// afterwards, so the half that was allowed committed while the caller was told
// the mutation had been blocked. An agent given that answer reports a failure,
// or tries again, for a write that happened.
func TestCrossDatabaseMutationRefusedRunsNothing(t *testing.T) {
	cases := []struct {
		name      string
		configure func(*Config)
		query     string
		wantErr   string
	}{
		{
			name: "read-only database",
			configure: func(c *Config) {
				c.Databases["ledger"] = DatabaseConfig{Type: "sqlite", ReadOnly: true}
			},
			query:   crossDatabaseWrite,
			wantErr: "database ledger is read-only",
		},
		{
			name: "read-only table",
			configure: func(c *Config) {
				c.Tables = append(c.Tables, Table{Name: "notes", Database: "ledger", ReadOnly: true})
			},
			query:   crossDatabaseWrite,
			wantErr: "notes is read-only",
		},
		{
			name: "one half does not compile",
			query: `mutation {
	notes(insert: { id: 2, no_such_column: "sorted out" }) { id }
	tickets(where: { id: { eq: 1 } }, update: { status: "resolved" }) { id status }
}`,
			wantErr: "no_such_column",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newCrossDatabaseEngine(t, tc.configure)
			_, err := e.gj.GraphQL(context.Background(), tc.query, nil, nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want an error naming %q, got %v", tc.wantErr, err)
			}
			if status := e.ticketStatus(t, 1); status != "open" {
				t.Errorf("the mutation was refused, but ticket 1 is now %q", status)
			}
			if n := e.noteCount(t); n != 1 {
				t.Errorf("the mutation was refused, but the ledger now has %d notes", n)
			}
		})
	}
}

// Two databases share no transaction, so one half can still fail while it runs
// after the other has committed. The error then says which writes stand.
func TestCrossDatabaseMutationSaysWhatWasApplied(t *testing.T) {
	e := newCrossDatabaseEngine(t, nil)
	// Note 1 exists, so the ledger half fails on its primary key when it runs.
	_, err := e.gj.GraphQL(context.Background(), `mutation {
	notes(insert: { id: 1, body: "again" }) { id }
	tickets(where: { id: { eq: 1 } }, update: { status: "resolved" }) { id status }
}`, nil, nil)
	if err == nil {
		t.Fatal("a duplicate primary key was accepted")
	}
	if status := e.ticketStatus(t, 1); status != "resolved" {
		t.Fatalf("ticket 1 is %q: this test needs the app half to have committed", status)
	}
	if !strings.Contains(err.Error(), "database ledger") {
		t.Errorf("the error does not name the database that failed: %v", err)
	}
	if !strings.Contains(err.Error(), "app") || !strings.Contains(err.Error(), "applied") {
		t.Errorf("the error does not say the app database's write was applied: %v", err)
	}
}

// notesControlPlane stands in for the service's control plane: it owns the
// ledger's notes table, as the service owns gj_task_entry, and counts the
// writes that reach it.
type notesControlPlane struct{ writes *int }

func (notesControlPlane) ManagedMutationTables() []string { return []string{"notes"} }

func (h notesControlPlane) ExecuteManagedMutation(context.Context, ManagedMutationRequest) (json.RawMessage, error) {
	*h.writes++
	return json.RawMessage(`{"notes":[{"id":2}]}`), nil
}

// A system root and a database table cannot share a mutation, and the refusal
// says that, naming both.
//
// It used to say "mutations blocked: database ... is read-only". That is true
// of SQL and false of the table: the same write, sent alone, is accepted. A
// model that wrote a task note and a ticket update in one mutation read the
// refusal as a policy it could not get past, and told the user the database
// was read-only.
func TestCrossDatabaseMutationWithSystemRootSaysToSplit(t *testing.T) {
	writes := 0
	e := newCrossDatabaseEngine(t, func(c *Config) {
		// The control plane's database takes no SQL writes.
		c.Databases["ledger"] = DatabaseConfig{Type: "sqlite", ReadOnly: true}
	}, OptionSetManagedMutationHandler("ledger", notesControlPlane{&writes}))

	_, err := e.gj.GraphQL(context.Background(), crossDatabaseWrite, nil, nil)
	if err == nil {
		t.Fatal("a system root and a database table were written in one mutation")
	}
	for _, want := range []string{"notes", "tickets", "separate mutations"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
	// The service marks any error containing these as a final policy denial
	// ("retry only after the access policy changes"), which this is not.
	for _, misleading := range []string{"read-only", "blocked", "denied", "unauthorized"} {
		if strings.Contains(err.Error(), misleading) {
			t.Errorf("the refusal reads as an access denial (%q): %v", misleading, err)
		}
	}
	if status := e.ticketStatus(t, 1); status != "open" || writes != 0 {
		t.Fatalf("the mutation was refused, but ticket 1 is %q and %d system writes ran", status, writes)
	}

	// Sent separately, as the refusal says to, both are accepted.
	if _, err := e.gj.GraphQL(context.Background(),
		`mutation { notes(insert: { id: 2, body: "sorted out" }) { id } }`, nil, nil); err != nil || writes != 1 {
		t.Fatalf("the system root on its own: err %v, %d writes", err, writes)
	}
	if _, err := e.gj.GraphQL(context.Background(),
		`mutation { tickets(where: { id: { eq: 1 } }, update: { status: "resolved" }) { id status } }`, nil, nil); err != nil {
		t.Fatalf("the database table on its own: %v", err)
	}
	if status := e.ticketStatus(t, 1); status != "resolved" {
		t.Fatalf("ticket 1 is %q, want resolved", status)
	}
}
