package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dosco/graphjin/core"
	_ "github.com/go-sql-driver/mysql"
	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/mysql"
)

/*
Steps to run this script:

1. Execute this command in a seperate window
docker run --rm -p 23042:23042 -v /var/run/docker.sock:/var/run/docker.sock -v /Users/vikram/src/graphjin/libtest:/Users/vikram/src/graphjin/libtest orlangure/gnomock

2.go run *.go`
*/

func main() {
	m := mysql.Preset(
		mysql.WithUser("user", "user"),
		mysql.WithDatabase("db"),
		mysql.WithQueriesFile("./schema.sql"),
	)

	container, err := gnomock.Start(m)
	if err != nil {
		panic(err)
	}

	defer func() { _ = gnomock.Stop(container) }()

	// connStr := fmt.Sprintf(
	// 	"host=%s port=%d user=%s password=%s  dbname=%s sslmode=disable",
	// 	container.Host, container.DefaultPort(),
	// 	"user", "user", "db",
	// )

	connStr := fmt.Sprintf("%s:%s@tcp(%s)/%s", "user", "user",
		container.DefaultAddress(), "db")

	fmt.Println(">", connStr)

	db, err := sql.Open("mysql", connStr)
	if err != nil {
		panic(err)
	}

	// db, err := sql.Open("pgx", "postgres://postgres:postgres@localhost:5432/webshop_development")
	// if err != nil {
	// 	panic(err)
	// }

	conf := &core.Config{
		DBType: "mysql",
		Debug:  true,
	}

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		fmt.Println("ERR", err)

		for {
			time.Sleep(2 * time.Second)
		}
	}

	// query := `
	//   query {
	//     products(first: 5, after: $cursor) {
	//     id
	// 		name
	// 		user {
	// 			email
	// 		}
	//   }
	// }`
	// vars := `{
	// 	"cursor":null
	// }`

	// query := `
	// mutation {
	// 	user(insert: $data) {
	// 		id
	// 		email
	// 	}
	// }
	// `
	// vars := `{
	// 	"data": {
	// 		"email": "user100@test.com"
	// 	}
	// }`

	query := `
	  subscription {
	    product(id: $id) {
	    id
			name
			user {
				email
			}
	  }
	}`
	vars := `{
		"id": 1
	}`

	ctx := context.Background()
	ctx = context.WithValue(ctx, core.UserIDKey, 1)

	// res, err := gj.GraphQL(ctx, query, json.RawMessage(vars))
	// if err != nil {
	// 	panic(err)
	// }

	res, err := gj.Subscribe(ctx, query, json.RawMessage(vars))
	if err != nil {
		panic(err)
	}
	for {
		msg := <-res.Result
		fmt.Println(">", string(msg.Data))
	}

	//fmt.Println(string(res.Data))
}
