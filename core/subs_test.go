package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/dosco/graphjin/core"
)

func Example_subscription() {
	gql := `subscription test {
		users(id: $id) {
			id
			email
			phone
		}
	}`

	vars := json.RawMessage(`{ "id": 3 }`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true, PollDuration: 1}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	m, err := gj.Subscribe(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	for i := 0; i < 10; i++ {
		msg := <-m.Result
		fmt.Println(string(msg.Data))

		// update user phone in database to trigger subscription
		q := fmt.Sprintf(`UPDATE users SET phone = '650-447-000%d' WHERE id = 3`, i)
		_, err := db.Exec(q)
		if err != nil {
			panic(err)
		}
	}

	// Output:
	// {"users": {"id": 3, "email": "user3@test.com", "phone": null}}
	// {"users": {"id": 3, "email": "user3@test.com", "phone": "650-447-0000"}}
	// {"users": {"id": 3, "email": "user3@test.com", "phone": "650-447-0001"}}
	// {"users": {"id": 3, "email": "user3@test.com", "phone": "650-447-0002"}}
	// {"users": {"id": 3, "email": "user3@test.com", "phone": "650-447-0003"}}
	// {"users": {"id": 3, "email": "user3@test.com", "phone": "650-447-0004"}}
	// {"users": {"id": 3, "email": "user3@test.com", "phone": "650-447-0005"}}
	// {"users": {"id": 3, "email": "user3@test.com", "phone": "650-447-0006"}}
	// {"users": {"id": 3, "email": "user3@test.com", "phone": "650-447-0007"}}
	// {"users": {"id": 3, "email": "user3@test.com", "phone": "650-447-0008"}}
}

func Example_subscriptionWithCursor() {
	gql := `subscription test {
		chats(first: 1, after: $cursor) {
			id
			body
		}
	}`

	vars := json.RawMessage(`{ "id": 3 }`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true, PollDuration: 1}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	m, err := gj.Subscribe(context.Background(), gql, vars, nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		for i := 6; i < 20; i++ {
			// insert a new chat message
			q := fmt.Sprintf(`INSERT INTO chats (id, body) VALUES (%d, 'New chat message %d')`, i, i)
			_, err := db.Exec(q)
			if err != nil {
				panic(err)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()

	re, _ := regexp.Compile(`cursor\"\:\"([^\s\"]+)`)

	for i := 0; i < 10; i++ {
		msg := <-m.Result
		// replace cursor value to make test work since it's encrypted
		v := re.ReplaceAllString(string(msg.Data), "cursor_was_here")
		fmt.Println(v)
	}

	// Output:
	// {"chats": [{"id": 1, "body": "This is chat message number 1"}], "chats_cursor_was_here"}
	// {"chats": [{"id": 2, "body": "This is chat message number 2"}], "chats_cursor_was_here"}
	// {"chats": [{"id": 3, "body": "This is chat message number 3"}], "chats_cursor_was_here"}
	// {"chats": [{"id": 4, "body": "This is chat message number 4"}], "chats_cursor_was_here"}
	// {"chats": [{"id": 5, "body": "This is chat message number 5"}], "chats_cursor_was_here"}
	// {"chats": [{"id": 6, "body": "New chat message 6"}], "chats_cursor_was_here"}
	// {"chats": [{"id": 7, "body": "New chat message 7"}], "chats_cursor_was_here"}
	// {"chats": [{"id": 8, "body": "New chat message 8"}], "chats_cursor_was_here"}
	// {"chats": [{"id": 9, "body": "New chat message 9"}], "chats_cursor_was_here"}
	// {"chats": [{"id": 10, "body": "New chat message 10"}], "chats_cursor_was_here"}
}

func TestSubscription(t *testing.T) {
	gql := `subscription test {
		users(where: { or: { id: { eq: $id }, id: { eq: $id2 } } }) @object {
			id
			email
		}
	}`

	conf := &core.Config{DBType: dbType, DisableAllowList: true, PollDuration: 1}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	w := sync.WaitGroup{}

	for i := 101; i < 8128; i++ {
		w.Add(1)
		go func(n int) {
			id := (rand.Intn(100-1) + 1)
			vars := json.RawMessage(fmt.Sprintf(`{ "id": %d, "id2": %d }`, n, id))
			m, err := gj.Subscribe(context.Background(), gql, vars, nil)
			if err != nil {
				fmt.Println(err)
				return
			}
			msg := <-m.Result
			exp := fmt.Sprintf(`{"users": {"id": %d, "email": "user%d@test.com"}}`, id, id)
			val := string(msg.Data)

			if val != exp {
				t.Errorf("expected '%s' got '%s'", exp, val)
			}
			w.Done()
		}(i)
	}

	w.Wait()
}
