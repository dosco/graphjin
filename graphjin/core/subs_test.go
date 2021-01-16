package core_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core"
)

func Example_subscription() {
	gql := `subscription test {
		user(id: $id) {
			id
			email
		}
	}`

	vars := json.RawMessage(`{ "id": 3 }`)

	conf := &core.Config{DBType: dbType, DisableAllowList: true, PollDuration: 1}
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	m, err := gj.Subscribe(context.Background(), gql, vars)
	if err != nil {
		fmt.Println(err)
		return
	}
	msg := <-m.Result
	fmt.Println(string(msg.Data))

	// Output: {"user": {"id": 3, "email": "user3@test.com"}}
}

/*
func TestSubscription(t *testing.T) {
	gql := `subscription test {
		user(where: { or: { id: { eq: $id }, id: { eq: $id2 } } }) {
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

	for i := 0; i < 100000; i++ {
		go func(n int) {
			w.Add(1)
			id := (rand.Intn(100-1) + 1)
			vars := json.RawMessage(fmt.Sprintf(`{ "id": %d, "id2": %d }`, n, id))
			m, err := gj.Subscribe(context.Background(), gql, vars)
			if err != nil {
				fmt.Println(err)
				return
			}
			msg := <-m.Result
			exp := fmt.Sprintf(`{"user": {"id": %d, "email": "user%d@test.com"}}`, id, id)
			val := string(msg.Data)

			if val != exp {
				t.Errorf("expected '%s' got '%s'", exp, val)
			}
			w.Done()
		}(i)
	}

	w.Wait()
}
*/
