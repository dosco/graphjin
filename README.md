<img src="docs/website/static/img/graphjin-logo.svg" width="80" />

# GraphJin - Build APIs in 5 minutes

[![GoDoc](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=for-the-badge&logo=appveyor&logo=appveyor)](https://pkg.go.dev/github.com/dosco/graphjin/core?tab=doc)
[![GoReport](https://goreportcard.com/badge/github.com/gojp/goreportcard?style=for-the-badge)](https://goreportcard.com/report/github.com/dosco/graphjin)
[![Apache 2.0](https://img.shields.io/github/license/dosco/graphjin.svg?style=for-the-badge)](https://github.com/dosco/graphjin/blob/master/LICENSE)
[![Docker build](https://img.shields.io/docker/cloud/build/dosco/graphjin.svg?style=for-the-badge)](https://hub.docker.com/r/dosco/graphjin/builds)
[![Discord Chat](https://img.shields.io/discord/628796009539043348.svg?style=for-the-badge&logo=appveyor)](https://discord.gg/6pSWCTZ)

GraphJin gives you a high performance GraphQL API without you having to write any code. GraphQL is automagically compiled into an efficient SQL query. Use it either as a library or a standalone service.

## 1. Quick Install

Mac (Homebrew)
```
brew install dosco/graphjin/graphjin
```

Ubuntu (Snap)
```
sudo snap install --classic graphjin
```

Go Install
```
go get github.com/dosco/graphjin
```

## 2. Create New API

```bash
graphjin new <app_name>

cd <app_name>
docker-compose run api db:setup
docker-compose up
```

## Using it in your own code

```console
go get github.com/dosco/graphjin/core
```

```golang
package main

import (
  "context"
  "database/sql"
  "fmt"
  "log"

  "github.com/dosco/graphjin/core"
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

  res, err := sg.GraphQL(ctx, query, nil)
  if err != nil {
    log.Fatal(err)
  }

  fmt.Println(string(res.Data))
}
```

## About GraphJin

After working on several products through my career I found that we spend way too much time on building API backends. Most APIs also need constant updating, and this costs time and money.

It's always the same thing, figure out what the UI needs then build an endpoint for it. Most API code involves struggling with an ORM to query a database and mangle the data into a shape that the UI expects to see.

I didn't want to write this code anymore, I wanted the computer to do it. Enter GraphQL, to me it sounded great, but it still required me to write all the same database query code.

Having worked with compilers before I saw this as a compiler problem. Why not build a compiler that converts GraphQL to highly efficient SQL.

This compiler is what sits at the heart of GraphJin, with layers of useful functionality around it like authentication, remote joins, rails integration, database migrations, and everything else needed for you to build production-ready apps with it.

## Features

- Works with Postgres, MySQL8 and Yugabyte DB
- Complex nested queries and mutations
- Realtime updates with subscriptions
- Build infinite scroll, feeds, nested comments, etc
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
- OpenCensus Support: Zipkin, Prometheus, X-Ray, Stackdriver
- API Rate Limiting
- Highly scalable and fast

## Documentation

[Quick Start](https://github.com/dosco/graphjin/wiki/Quick-Start)

[Documentation](https://github.com/dosco/graphjin/wiki)

[Build APIs in 5 minutes with GraphJin](https://dev.to/dosco/build-high-performance-graphql-apis-in-5-minutes-with-graphjin-261o)

[GraphQL vs REST](https://dev.to/dosco/rest-vs-graphql-building-startups-in-2021-3k73)

[GraphQL Examples](https://pkg.go.dev/github.com/dosco/graphjin/core#pkg-examples)

## Reach out

We're happy to help you leverage GraphJin reach out if you have questions

[twitter/dosco](https://twitter.com/dosco)

[discord/graphjin](https://discord.gg/6pSWCTZ) (Chat)

## Production use

The popular [42papers.com](https://42papers.com) site for discovering trending papers in AI and Computer Science uses GraphJin for it's entire backend.

## License

[Apache Public License 2.0](https://opensource.org/licenses/Apache-2.0)

Copyright (c) 2019-present Vikram Rangnekar
