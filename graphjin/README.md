<img src="docs/website/static/img/super-graph-logo.svg" width="80" />

# Super Graph - Fetch data without code!

[![GoDoc](https://img.shields.io/badge/godoc-reference-5272B4.svg)](https://pkg.go.dev/github.com/dosco/super-graph/core?tab=doc)
![Apache 2.0](https://img.shields.io/github/license/dosco/super-graph.svg?style=flat-square)
![Docker build](https://img.shields.io/docker/cloud/build/dosco/super-graph.svg?style=flat-square)
[![Discord Chat](https://img.shields.io/discord/628796009539043348.svg)](https://discord.gg/6pSWCTZ)

Super Graph gives you a high performance GraphQL API without you having to write any code. GraphQL is automagically compiled into an efficient SQL query. Use it either as a library or a standalone service.

## Using it as a service

```console
go get github.com/dosco/super-graph
super-graph new <app_name>
```

## Using it in your own code

```console
go get github.com/dosco/super-graph/core
```

```golang
package main

import (
  "database/sql"
  "fmt"
  "time"
  "github.com/dosco/super-graph/core"
  _ "github.com/jackc/pgx/v4/stdlib"
)

func main() {
  db, err := sql.Open("pgx", "postgres://postgrs:@localhost:5432/example_db")
  if err != nil {
    log.Fatal(err)
  }

  sg, err := core.NewSuperGraph(nil, db)
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

  res, err := sg.GraphQL(context.Background(), query, nil)
  if err != nil {
    log.Fatal(err)
  }

  fmt.Println(string(res.Data))
}
```

## About Super Graph

After working on several products through my career I found that we spend way too much time on building API backends. Most APIs also need constant updating, and this costs time and money.

It's always the same thing, figure out what the UI needs then build an endpoint for it. Most API code involves struggling with an ORM to query a database and mangle the data into a shape that the UI expects to see.

I didn't want to write this code anymore, I wanted the computer to do it. Enter GraphQL, to me it sounded great, but it still required me to write all the same database query code.

Having worked with compilers before I saw this as a compiler problem. Why not build a compiler that converts GraphQL to highly efficient SQL.

This compiler is what sits at the heart of Super Graph, with layers of useful functionality around it like authentication, remote joins, rails integration, database migrations, and everything else needed for you to build production-ready apps with it.

## Features

- Complex nested queries and mutations
- Auto learns database tables and relationships
- Role and Attribute-based access control
- Opaque cursor-based efficient pagination
- Full-text search and aggregations
- JWT tokens supported (Auth0, etc)
- Join database queries with remote REST APIs
- Also works with existing Ruby-On-Rails apps
- Rails authentication supported (Redis, Memcache, Cookie)
- A simple config file
- High performance Go codebase
- Tiny docker image and low memory requirements
- Fuzz tested for security
- Database migrations tool
- Database seeding tool
- Works with Postgres and YugabyteDB

## Documentation

[supergraph.dev](https://supergraph.dev)

## Reach out

We're happy to help you leverage Super Graph reach out if you have questions

[twitter/dosco](https://twitter.com/dosco)

[chat/super-graph](https://discord.gg/6pSWCTZ)

## License

[Apache Public License 2.0](https://opensource.org/licenses/Apache-2.0)

Copyright (c) 2019-present Vikram Rangnekar
