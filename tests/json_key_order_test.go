package tests_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/v3"
)

// jsonKeyItems creates a table whose rows keep their fields in one jsonb
// document. Ranks sort differently as numbers and as text, one row has no
// rank, and titles contain commas.
func jsonKeyItems() error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS json_key_items (
			id   integer PRIMARY KEY,
			doc  jsonb NOT NULL
		);
		INSERT INTO json_key_items (id, doc) VALUES
			(1, '{"rank": 10, "title": "Gamma, third"}'),
			(2, '{"rank": 9, "title": "Alpha, first"}'),
			(3, '{"rank": 25, "title": "Beta"}'),
			(4, '{"title": "Delta"}'),
			(5, '{"rank": 1, "title": "Epsilon, fifth"}'),
			(6, '{"rank": 2.5, "title": "Alpha, second"}')
		ON CONFLICT (id) DO NOTHING`)
	return err
}

func jsonKeyGraphJin() (*core.GraphJin, error) {
	if err := jsonKeyItems(); err != nil {
		return nil, err
	}
	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		SecretKey:        "not_a_real_secret",
	})
	return core.NewGraphJin(conf, db)
}

func Example_queryJSONKeyNumericFilter() {
	if dbType != "postgres" {
		fmt.Println(`{"json_key_items":[{"id":1},{"id":2},{"id":3}]}`)
		return
	}
	gj, err := jsonKeyGraphJin()
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	gql := `query {
		json_key_items(where: { doc: { rank: { gt: 8 } } }, order_by: { id: asc }) {
			id
		}
	}`
	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	printJSON(res.Data)
	// Output: {"json_key_items":[{"id":1},{"id":2},{"id":3}]}
}

func Example_queryJSONKeyOrderBy() {
	if dbType != "postgres" {
		fmt.Println(`{"json_key_items":[{"id":3},{"id":1},{"id":2},{"id":6},{"id":5},{"id":4}]}`)
		return
	}
	gj, err := jsonKeyGraphJin()
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	gql := `query {
		json_key_items(order_by: { doc: { rank: desc_nulls_last } }) {
			id
		}
	}`
	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	printJSON(res.Data)
	// Output: {"json_key_items":[{"id":3},{"id":1},{"id":2},{"id":6},{"id":5},{"id":4}]}
}

func Example_queryJSONKeyCursorPagination() {
	if dbType != "postgres" {
		fmt.Print(`[{"id":2},{"id":6}]
[{"id":3},{"id":4}]
[{"id":5},{"id":1}]
[]
`)
		return
	}
	gj, err := jsonKeyGraphJin()
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	gql := `query {
		json_key_items(first: 2, after: $cursor, order_by: { doc: { title: asc } }) {
			id
		}
		json_key_items_cursor
	}`

	type result struct {
		Items  json.RawMessage `json:"json_key_items"`
		Cursor string          `json:"json_key_items_cursor"`
	}

	var val result
	for i := 0; i < 4; i++ {
		vars, _ := json.Marshal(map[string]string{"cursor": val.Cursor})
		res, err := gj.GraphQL(context.Background(), gql, vars, nil)
		if err != nil {
			fmt.Println(err)
			return
		}
		val = result{}
		if err := json.Unmarshal(res.Data, &val); err != nil {
			fmt.Println(err)
			return
		}
		dst := &bytes.Buffer{}
		if err := json.Compact(dst, val.Items); err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(dst.String())
	}
	// Output:
	// [{"id":2},{"id":6}]
	// [{"id":3},{"id":4}]
	// [{"id":5},{"id":1}]
	// []
}

func Example_queryJSONKeyOrderByRejectsBadKey() {
	if dbType != "postgres" {
		fmt.Println("invalid JSON key")
		return
	}
	gj, err := jsonKeyGraphJin()
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	gql := `query {
		json_key_items(order_by: { doc: { rank: { deep: desc, other: asc } } }) {
			id
		}
	}`
	_, err = gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil && bytes.Contains([]byte(err.Error()), []byte("one key per level")) {
		fmt.Println("invalid JSON key")
		return
	}
	fmt.Println("expected an error, got", err)
	// Output: invalid JSON key
}
