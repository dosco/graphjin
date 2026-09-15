package tests_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/v3"
)

// Example_queryWithWhereInTextVariable checks in and nin with a JSON array
// variable on a text column. MySQL and MariaDB compare the column as a JSON
// string, and nin must exclude the listed values.
func Example_queryWithWhereInTextVariable() {
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}
	defer gj.Close()

	vars := json.RawMessage(`{ "emails": ["user1@test.com", "user3@test.com"] }`)
	for _, gql := range []string{
		`query {
			users(where: { and: [{ email: { in: $emails } }, { id: { lte: 4 } }] }, order_by: { id: asc }) { id }
		}`,
		`query {
			users(where: { and: [{ email: { nin: $emails } }, { id: { lte: 4 } }] }, order_by: { id: asc }) { id }
		}`,
	} {
		res, err := gj.GraphQL(context.Background(), gql, vars, nil)
		if err != nil {
			fmt.Println(err)
			continue
		}
		printJSON(res.Data)
	}
	// Output:
	// {"users":[{"id":1},{"id":3}]}
	// {"users":[{"id":2},{"id":4}]}
}
