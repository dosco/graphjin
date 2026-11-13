package serv

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

// A mutation that writes two SQLite databases must leave no statement open on
// either of them.
//
// One did. Each database's half is a script (temp tables, the write, a final
// SELECT, teardown), and it reached the driver as one string. The pure-Go
// driver the server ships with steps such a string in turn and, when the
// teardown's DROP fails because the script's own SELECT is still reading the
// table, drops that SELECT without finalizing it. Closing the pool cannot close
// a connection with a statement open, so its read lock on the written table
// lasted as long as the process, and with cache=shared (how the demo opens its
// database) every later connection to the file inherited it. Reads worked; the
// next write waited in the driver with no timeout, deaf to its context. A
// benchmark run stopped on exactly that on 2026-11-13.
//
// This lives in serv because the driver does: core's own tests link the cgo
// driver, which closes each statement of a multi-statement string and cannot
// show the lock.
func TestCrossDatabaseMutationLeavesNoStatementOpen(t *testing.T) {
	dir := t.TempDir()
	appDSN := "file:" + filepath.Join(dir, "app.db") + "?cache=shared&_pragma=busy_timeout(5000)"
	open := func(dsn, ddl string) *sql.DB {
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		if ddl != "" {
			if _, err := db.Exec(ddl); err != nil {
				t.Fatal(err)
			}
		}
		return db
	}
	app := open(appDSN, `CREATE TABLE tickets (id INTEGER PRIMARY KEY, status TEXT NOT NULL);
INSERT INTO tickets VALUES (1, 'open'), (2, 'open');`)
	ledger := open(filepath.Join(dir, "ledger.db"), `CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL);`)
	defer ledger.Close() //nolint:errcheck

	conf := core.Config{
		DisableAllowList: true,
		Databases: map[string]core.DatabaseConfig{
			"app":    {Type: "sqlite"},
			"ledger": {Type: "sqlite"},
		},
	}
	gj, err := core.NewGraphJin(&conf, app,
		core.OptionSetDatabases(map[string]*sql.DB{"app": app, "ledger": ledger}))
	if err != nil {
		t.Fatal(err)
	}

	res, err := gj.GraphQL(context.Background(), `mutation {
	notes(insert: { id: 1, body: "sorted out" }) { id }
	tickets(where: { id: { eq: 1 } }, update: { status: "resolved" }) { id status }
}`, nil, nil)
	if err != nil {
		t.Errorf("the cross-database mutation failed: %v", err)
	} else if got := string(res.Data); !strings.Contains(got, `"resolved"`) || !strings.Contains(got, `"notes"`) {
		t.Errorf("the answer is missing a root: %s", got)
	}

	// Close everything the engine held, as a restart or an eval reset does,
	// and open the file again in the same process.
	gj.Close()
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := open(appDSN, "")
	written := make(chan error, 1)
	go func() {
		_, err := reopened.Exec(`UPDATE tickets SET status = 'closed' WHERE id = 2`)
		written <- err
	}()
	select {
	case err := <-written:
		if err != nil {
			t.Fatalf("a write after the pool was reopened failed: %v", err)
		}
		_ = reopened.Close()
	case <-time.After(15 * time.Second):
		// The pool is left open: the write holds its connection, and nothing
		// this test can do will make the driver give it back.
		t.Fatal("a write to tickets is still waiting 15s after the pool was reopened: " +
			"the mutation left a statement open, and its table lock outlived the pool")
	}
}
