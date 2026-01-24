package core_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	defer db.Close()

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

	for i := 0; i < 4; i++ {
		select {
		case <-m.Result:
			// Message received successfully
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for message %d", i+1)
		}
	}
}
