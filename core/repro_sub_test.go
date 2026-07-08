package core_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	_ "github.com/mattn/go-sqlite3"
)

func TestReproSubHang(t *testing.T) {
	connStr := "file:memdb1?mode=memory&cache=shared"
	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck

	_, err = db.Exec(`
		CREATE TABLE chats (
			id INTEGER PRIMARY KEY,
			body TEXT
		);
		INSERT INTO chats (id, body) VALUES (1, 'msg 1'), (2, 'msg 2'), (3, 'msg 3'), (4, 'msg 4'), (5, 'msg 5');
	`)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		for i := 6; i < 10; i++ {
			time.Sleep(1 * time.Second)
			_, _ = db.Exec(fmt.Sprintf(`INSERT INTO chats (id, body) VALUES (%d, 'msg %d')`, i, i))
		}
	}()

	gql := `subscription {
		chats(first: 1, after: $cursor) {
			id
			body
		}
	}`

	conf := &core.Config{
		DBType:           "sqlite",
		DisableAllowList: true,
		SubsPollDuration: 1 * time.Second,
		SecretKey:        "not_a_real_secret",
	}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	m, err := gj.Subscribe(ctx, gql, json.RawMessage(`{"cursor": null}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Unsubscribe()
	cursorVars := m.CursorVariableNames()
	if len(cursorVars) != 1 || cursorVars[0] != "cursor" {
		t.Fatalf("cursor variable names = %v, want [cursor]", cursorVars)
	}

	for i := 0; i < 4; i++ {
		select {
		case res := <-m.Result:
			cursors := res.SubscriptionCursors()
			if cursors["cursor"] == "" {
				t.Fatalf("result subscription cursors = %v, want cursor checkpoint", cursors)
			}
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for message %d", i+1)
		}
	}
}

func TestSubscriptionRoleQueryWithNilRequestConfig(t *testing.T) {
	db, err := sql.Open("sqlite3", t.TempDir()+"/roles.sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck

	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT,
			disabled INTEGER
		);
		INSERT INTO users (id, email, disabled) VALUES (1, 'disabled@test.com', 1);
	`)
	if err != nil {
		t.Fatal(err)
	}

	conf := &core.Config{
		DBType:           "sqlite",
		DisableAllowList: true,
		RolesQuery:       "SELECT * FROM users WHERE id = $user_id",
		Roles: []core.Role{
			{Name: "disabled_user", Match: "disabled = 1"},
		},
	}
	if err := conf.AddRoleTable("disabled_user", "users", core.Query{}); err != nil {
		t.Fatal(err)
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	gql := `subscription {
		users(id: 1) {
			id
			email
		}
	}`

	ctx := context.WithValue(context.Background(), core.UserIDKey, 1)
	m, err := gj.Subscribe(ctx, gql, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Unsubscribe()
}

func TestSubscriptionWhereVariableRejected(t *testing.T) {
	db, err := sql.Open("sqlite3", t.TempDir()+"/where-var.sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck

	_, err = db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT
		);
		INSERT INTO users (id, email) VALUES (1, 'user@test.com');
	`)
	if err != nil {
		t.Fatal(err)
	}

	conf := &core.Config{
		DBType:           "sqlite",
		DisableAllowList: true,
	}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	gql := `subscription($where: UsersWhereInput) {
		users(where: $where) {
			id
			email
		}
	}`

	_, err = gj.Subscribe(context.Background(), gql, json.RawMessage(`{"where":{"id":{"eq":1}}}`), nil)
	if err == nil {
		t.Fatal("expected where variable to be rejected")
	}
	if !strings.Contains(err.Error(), "where must be an inline object; use variables only inside filter values") {
		t.Fatalf("unexpected error: %v", err)
	}
}
