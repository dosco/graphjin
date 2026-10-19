package tests_test

import (
	"context"
	"fmt"

	"github.com/dosco/graphjin/core/v3"
)

func Example_queryMultiOperatorRange() {
	gj, err := core.NewGraphJin(newConfig(&core.Config{DBType: dbType, DisableAllowList: true}), db)
	if err != nil {
		panic(err)
	}
	defer gj.Close()
	res, err := gj.GraphQL(context.Background(), `query {
		products(where: { id: { gte: 2, lte: 4 } }, order_by: { id: asc }) { id }
	}`, nil, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	printJSON(res.Data)
	// Output: {"products":[{"id":2},{"id":3},{"id":4}]}
}
