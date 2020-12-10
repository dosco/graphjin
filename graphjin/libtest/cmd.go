package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/dosco/super-graph/core"
	_ "github.com/jackc/pgx/v4/stdlib"
)

// run `docker-compose up` in the repository root before
// running this test script with `go run *.go`
func main() {
	db, err := sql.Open("pgx", "postgres://postgres:postgres@localhost:5432/webshop_development")
	if err != nil {
		panic(err)
	}

	sg, err := core.NewSuperGraph(nil, db)
	if err != nil {
		panic(err)
	}

	query := `
	  query {
	    products {
	    id
	    name
	  }
	}`

	ctx := context.Background()
	ctx = context.WithValue(ctx, core.UserIDKey, 1)

	res, err := sg.GraphQL(ctx, query, nil)
	if err != nil {
		panic(err)
	}

	fmt.Println(string(res.Data))
}
