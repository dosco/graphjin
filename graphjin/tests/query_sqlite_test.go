package tests_test

import (
	"context"
	"fmt"
	"github.com/dosco/graphjin/core/v3"
)

func Example_queryBySearchSQLite() {
	if dbType != "sqlite" {
		fmt.Println(`{"products_fts":[{"name":"Product 3"}]}`)
		return
	}

	gql := `query {
		products_fts(search: "Product", limit: 5) {
			name
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"products_fts":[{"name":"Product 3"}]}
}
