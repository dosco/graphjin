// #nosec G404
package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"golang.org/x/sync/errgroup"
)

var cursorRegex *regexp.Regexp

func init() {
	cursorRegex = regexp.MustCompile(`cursor\"\:\s?\"([^\s\"]+)`)
}

func Example_subscription() {
	gql := `subscription test {
		users(id: $id) {
			id
			email
			phone
		}
	}`

	vars := json.RawMessage(`{ "id": 3 }`)

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, SubsPollDuration: 1})
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
		printJSON(msg.Data)

		// update user phone in database to trigger subscription
		q := fmt.Sprintf(`UPDATE users SET phone = '650-447-000%d' WHERE id = 3`, i)
		if _, err := db.Exec(q); err != nil {
			panic(err)
		}
	}
	// Output:
	// {"users":{"email":"user3@test.com","id":3,"phone":null}}
	// {"users":{"email":"user3@test.com","id":3,"phone":"650-447-0000"}}
	// {"users":{"email":"user3@test.com","id":3,"phone":"650-447-0001"}}
	// {"users":{"email":"user3@test.com","id":3,"phone":"650-447-0002"}}
	// {"users":{"email":"user3@test.com","id":3,"phone":"650-447-0003"}}
	// {"users":{"email":"user3@test.com","id":3,"phone":"650-447-0004"}}
	// {"users":{"email":"user3@test.com","id":3,"phone":"650-447-0005"}}
	// {"users":{"email":"user3@test.com","id":3,"phone":"650-447-0006"}}
	// {"users":{"email":"user3@test.com","id":3,"phone":"650-447-0007"}}
	// {"users":{"email":"user3@test.com","id":3,"phone":"650-447-0008"}}
}

func Example_subscriptionWithCursor() {
	// func TestSubCursor(t *testing.T) {
	// query to fetch existing chat messages
	// gql1 := `query {
	// 	chats(first: 3, after: $cursor) {
	// 		id
	// 		body
	// 	}
	// 	chats_cursor
	// }`

	// query to subscribe to new chat messages
	gql2 := `subscription {
		chats(first: 1, after: $cursor) {
			id
			body
		}
	}`

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		SubsPollDuration: 1,
		SecretKey:        "not_a_real_secret",
	})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	// struct to hold the cursor value from fetching the existing
	// chat messages
	// res := struct {
	// 	Cursor string `json:"chats_cursor"`
	// }{}

	// execute query for existing chat messages
	// m1, err := gj.GraphQL(context.Background(), gql1, nil, nil)
	// if err != nil {
	// 	fmt.Println(err)
	// 	return
	// }

	// extract out the cursor `chats_cursor` to use in the subscription
	// if err := json.Unmarshal(m1.Data, &res); err != nil {
	// 	fmt.Println(err)
	// 	return
	// }

	// replace cursor value to make test work since it's encrypted
	// v1 := cursorRegex.ReplaceAllString(string(m1.Data), "cursor_was_here")
	// fmt.Println(v1)

	// create variables with the previously extracted cursor value to
	// pass to the new chat messages subscription
	// vars := json.RawMessage(`{ "cursor": "` + res.Cursor + `" }`)
	vars := json.RawMessage(`{ "cursor": null }`)

	// subscribe to new chat messages using the cursor
	m2, err := gj.Subscribe(context.Background(), gql2, vars, nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		for i := 6; i < 20; i++ {
			// insert a new chat message
			q := fmt.Sprintf(`INSERT INTO chats (id, body) VALUES (%d, 'New chat message %d')`, i, i)
			if _, err := db.Exec(q); err != nil {
				panic(err)
			}
			time.Sleep(3 * time.Second)
		}
	}()

	for i := 0; i < 19; i++ {
		msg := <-m2.Result
		// replace cursor value to make test work since it's encrypted
		v2 := cursorRegex.ReplaceAllString(string(msg.Data), `cursor":"cursor_was_here`)
		printJSON([]byte(v2))
	}
	// Output:
	// {"chats":[{"body":"This is chat message number 1","id":1}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"This is chat message number 2","id":2}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"This is chat message number 3","id":3}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"This is chat message number 4","id":4}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"This is chat message number 5","id":5}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 6","id":6}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 7","id":7}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 8","id":8}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 9","id":9}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 10","id":10}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 11","id":11}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 12","id":12}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 13","id":13}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 14","id":14}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 15","id":15}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 16","id":16}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 17","id":17}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 18","id":18}],"chats_cursor":"cursor_was_here"}
	// {"chats":[{"body":"New chat message 19","id":19}],"chats_cursor":"cursor_was_here"}
}

func TestSubscription(t *testing.T) {
	gql := `subscription test {
		users(where: { or: { id: { eq: $id }, id: { eq: $id2 } } }) @object {
			id
			email
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, SubsPollDuration: 1})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	g, ctx := errgroup.WithContext(context.Background())

	for i := 101; i < 3000; i++ {
		n := i
		time.Sleep(20 * time.Millisecond)

		g.Go(func() error {
			id := (rand.Intn(100-1) + 1)
			vars := json.RawMessage(fmt.Sprintf(`{ "id": %d, "id2": %d }`, n, id))
			m, err := gj.Subscribe(ctx, gql, vars, nil)
			if err != nil {
				return fmt.Errorf("subscribe: %w", err)
			}

			msg := <-m.Result
			exp := fmt.Sprintf(`{"users": {"id": %d, "email": "user%d@test.com"}}`, id, id)
			val := string(msg.Data)

			if val != exp {
				t.Errorf("expected '%s' got '%s'", exp, val)
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		panic(err)
	}
}
