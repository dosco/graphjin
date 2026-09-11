package core

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGraphJinCloseStopsWatcherPromptly(t *testing.T) {
	g := &GraphJin{done: make(chan bool)}

	stopped := make(chan struct{})
	go func() {
		g.startDBWatcher(10 * time.Second)
		close(stopped)
	}()

	g.Close()
	g.Close()

	select {
	case <-stopped:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("expected watcher to stop promptly after Close")
	}
}

func TestWatcherRecompilesChangedColumns(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "watch.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT); INSERT INTO customers VALUES (1, 'Ada')`); err != nil {
		t.Fatal(err)
	}
	g, err := NewGraphJin(&Config{DBType: "sqlite", DisableAllowList: true}, db, OptionSetFS(NewOsFS(t.TempDir())), OptionSetDBSchemaWatcherDisabled(true))
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	changes := make(chan struct{}, 8)
	g.OnSchemaChange(func(string, string) { changes <- struct{}{} })
	go g.startDBWatcher(20 * time.Millisecond)
	for _, step := range []struct{ ddl, query, want string }{
		{`ALTER TABLE customers ADD COLUMN email TEXT DEFAULT 'ada@example.test'`, `query { customers { email } }`, `ada@example.test`},
		{`ALTER TABLE customers RENAME COLUMN name TO full_name`, `query { customers { full_name } }`, `Ada`},
	} {
		if _, err := db.Exec(step.ddl); err != nil {
			t.Fatal(err)
		}
		select {
		case <-changes:
		case <-time.After(5 * time.Second):
			t.Fatal("watcher did not reload changed schema")
		}
		result, err := g.GraphQL(context.Background(), step.query, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(result.Data), step.want) {
			t.Fatalf("query used stale schema: %s", result.Data)
		}
	}
	select {
	case <-changes:
		t.Fatal("unchanged schema caused another reload")
	case <-time.After(150 * time.Millisecond):
	}
}
