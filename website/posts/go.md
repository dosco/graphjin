---
chapter: 10
title: GO
description: Using the GO API
---

# GO

#### TOC

```golang
package main

import (
  "context"
  "database/sql"
  "fmt"
  "log"

  "github.com/dosco/graphjin/core/v2"
  _ "github.com/jackc/pgx/v4/stdlib"
)

func main() {
  db, err := sql.Open("pgx", "postgres://postgres:@localhost:5432/example_db")
  if err != nil {
    log.Fatal(err)
  }

  sg, err := core.NewGraphJin(nil, db)
  if err != nil {
    log.Fatal(err)
  }

  query := `
    query {
      posts {
      id
      title
    }
  }`

  ctx := context.Background()
  ctx = context.WithValue(ctx, core.UserIDKey, 1)

  res, err := sg.GraphQL(ctx, query, nil, nil)
  if err != nil {
    log.Fatal(err)
  }

  fmt.Println(string(res.Data))
}
```

### Add GraphJin

Add Graphjin to your GO application.

```shell
go get github.com/dosco/graphjin/core/v3
```

<mark>
👋 In production it is <b>very</b> important that you run GraphJin in production mode to do this you can use the `prod.yml` config which already has `production: true` or if you're using a config object then set it manually
</mark>

```yaml title="Config File prod.yml"
# When enabled GraphJin runs with production level security defaults.
# For example only queries from saved in the queries folder can be used.
production: true
```

```go title="Go config struct"
config := core.Config{ Production: true, DefaultLimit: 50 }
```

### Using GraphJin

```go
import "github.com/dosco/graphjin/core/v3"

// config can be read in from a file
config, err := NewConfig("./config", "dev.yml")

// or config can be a go struct
// config := core.Config{ Production: true, DefaultLimit: 50 }

gj, err := core.NewGraphJin(config, db)
```

### Whats `db` ?

Its the database client, currently we only support any database driver library for MySQL and Postgres that works with the Go `sql.DB` interface.

```go
import "database/sql"
import _ "github.com/jackc/pgx/v4/stdlib"

db, err := sql.Open("pgx", "postgres://postgres:@localhost:5432/example_db")
```

### Your first query

```go
// graphql query
query := `
query getPost {
  posts(id: $id) {
    id
    title
    author {
      id
      full_name
    }
  }
}`

// context with user id set to 1
ctx = context.WithValue(context.Background(), core.UserIDKey, 1)

// variables id set to 3
vars := json.RawMessage(`{ "id": 3 }`)

// execute the query
res, err := sg.GraphQL(ctx, query, vars, nil)
```

If you would rather use a `.gql` or `.graphql` file for the query then place it under `./config/queries` and use the `queryByName` API instead. <mark>Filename must be the query name with a graphql extension</mark>

```graphql title="./config/queries/getPost.gql"
query getPost {
  posts(id: $id) {
    id
    title
    author {
      id
      full_name
    }
  }
}
```

```go
res, err := gj.GraphQLByName(ctx, "getPosts", vars, nil)
```

Get the result

```go
fmt.Println(string(res.Data));
```

```json title="Result"
{
  "post": {
    "id": 3,
    "title": "My Third Blog Post",
    "author": {
      "id": 5,
      "full_name": "Andy Anderson"
    }
  }
}
```

### Using subscriptions

Did you ever need to have database changes streamed back to you in realtime. For example new sales that happened, comments added to a blog post, new likes that you want to stream back over websockets, whatever. This is not easy to implement efficiently. But with GraphJin its just as easy as making the above query and is designed to be very efficient.

A subscription query is just a normal query with the prefix `subscription`. Use the `subscribe` API that works similar to `query` in production mode
only allows you to use queries from the queries folder.

```go
// graphql query
query := `
query getPost {
  posts(id: $id) {
    id
    title
    author {
      id
      full_name
    }
  }
}`

// context with user id set to 1
ctx = context.WithValue(context.Background(), core.UserIDKey, 1)

// variables id set to 3
vars := json.RawMessage(`{ "id": 3 }`)

m, err := gj.Subscribe(ctx, query, vars, nil);
```

Alterntively you can use the `subscribeByName` API which is similar to the `queryByName` API.

```go
// context with user id set to 1
ctx = context.WithValue(context.Background(), core.UserIDKey, 1)

// variables id set to 3
vars := json.RawMessage(`{ "id": 3 }`)

m, err := gj.SubscribeByName(ctx, "getPost", vars, nil);
```

Getting the updates back from a subscription is a little different you have to use a callback since the results keep coming.

```go
for {
    msg := <-m.Result
    fmt.Println(string(res.Data))
}
```

```json title="Result"
{
  "post": {
    "id": 3,
    "title": "My Third Blog Post",
    "author": {
      "id": 5,
      "full_name": "Andy Anderson"
    }
  }
}
{
  "post": {
    "id": 3,
    "title": "I just changed the title",
    "author": {
      "id": 5,
      "full_name": "Andy Anderson"
    }
  }
}
{
  "post": {
    "id": 3,
    "title": "Changed it again",
    "author": {
      "id": 5,
      "full_name": "Andy A."
    }
  }
}
```

### Using the service

GraphJin has two packages `core` whih contains the core compiler and `serv` which contains the standalone service. One way to not have to build your own service and get the flexibility of using your own app is to use the `serv` package with your own code. This also means that you get cache headers (etags), compression, rate limiting all of this good stuff for free. The following http and websocket handlers are exposed for you to use:

```go title="GraphQL HTTP/Websocket Handler"
gjs.GraphQL(nil)
```

```go title="REST HTTP/Websocket Handler"
 gjs.REST(nil)
```

```go title="Embed the Web UI"
gjs.WebUI("/webui/", "/graphql")
```

Below is an example app to see how all this comes together.

```go
import (
	"log"
	"net/http"
	"path/filepath"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/go-chi/chi/v5"
)

func main() {
	// create the router
	r := chi.NewRouter()

	// readin graphjin config
	conf, err := serv.ReadInConfig(filepath.Join("./config", "dev.yml"))
	if err != nil {
		panic(err)
	}

	// create the graphjin service
	gjs, err := serv.NewGraphJinService(conf)
	if err != nil {
		log.Fatal(err)
	}

	// attach the graphql http handler
	r.Handle("/graphql", gjs.GraphQL(nil))

	// attach the rest http handler
	r.Handle("/rest/*", gjs.REST(nil))

	// attach the webui http handler
	r.Handle("/webui/*", gjs.WebUI("/webui/", "/graphql"))

	// add your own http handlers
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Welcome to the webshop!"))
	})

	http.ListenAndServe(":8080", r)
}
```
