# GraphJin Instant GraphQL API

[![GoDoc](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=for-the-badge&logo=appveyor&logo=appveyor)](https://pkg.go.dev/github.com/dosco/graphjin)
[![GoReport](https://goreportcard.com/badge/github.com/gojp/goreportcard?style=for-the-badge)](https://goreportcard.com/report/github.com/dosco/graphjin)
[![Apache 2.0](https://img.shields.io/github/license/dosco/graphjin.svg?style=for-the-badge)](https://github.com/dosco/graphjin/blob/master/LICENSE)
[![Docker build](https://img.shields.io/docker/cloud/build/dosco/graphjin.svg?style=for-the-badge)](https://hub.docker.com/r/dosco/graphjin/builds)
[![Discord Chat](https://img.shields.io/discord/628796009539043348.svg?style=for-the-badge&logo=appveyor)](https://discord.gg/6pSWCTZ)

<!-- [![Run on Google Cloud](./.github/deploy-cloud-run-button.svg)](https://deploy.cloud.run)
 -->

GraphJin gives you an instant secure and fast GraphQL API without code. GraphQL is automagically compiled into an efficient SQL query. Use either as a library or a standalone service. Build your backend APIs 100X faster.

## 1. Quick install

Mac (Homebrew)

```
brew install dosco/graphjin/graphjin
```

Ubuntu (Snap)

```
sudo snap install --classic graphjin
```

Debian and Redhat ([releases](https://github.com/dosco/graphjin/releases))

```
Download the .deb or .rpm from the releases page and install with dpkg -i and rpm -i respectively.
```

Go Install

```
go install github.com/dosco/graphjin@latest
```

## 2. Create a new app

```bash
graphjin new <app_name>

cd <app_name>
docker-compose run api db setup
docker-compose up
```

## Secure out of the box

When you use a query in development it's auto-saved in an allow list and only queries from this allow list can be run in production. In production these allowed queries are converted into prepared statments in the database to protect against sql injection, etc. This makes GraphJin very secure and also very fast since no compiling happens in production all queries go directly to the database. GraphJin is built in Go a language designed by Google to be secure and memory safe.

## Built for production

#### Instantly deploy new versions

```bash
# Deploy a new config
graphjin deploy --host=https://your-server.com --secret="your-secret-key"

# Rollback the last deployment
graphjin deploy rollback --host=https://your-server.com --secret="your-secret-key"
```

#### Secrets Management

```bash
# Secure save secrets like database passwords and JWT secret keys
graphjin secrets
```

#### Database Management

```bash
# Create, Migrate and Seed your database
graphjin db
```

## Use in your own code

You can use GraphJin as a library within your own code. The [serv](https://pkg.go.dev/github.com/dosco/graphjin/serv) package exposes the entirely GraphJin standlone service as a library while the [core](https://pkg.go.dev/github.com/dosco/graphjin/core) package exposes just the **GraphJin GraphQL to SQL compiler**. The [Go docs](https://pkg.go.dev/github.com/dosco/graphjin/core#pkg-examples) are filled with examples on how to use GraphJin within your own apps as a sort of alternative to using ORM packages. GraphJin allows you to use GraphQL and the full power of GraphJin to access your data instead of a limiting ORM.

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

  res, err := sg.GraphQL(ctx, query, nil, nil)
  if err != nil {
    log.Fatal(err)
  }

  fmt.Println(string(res.Data))
}
```

### Use the standalone service as a library

```golang
import (
  "github.com/dosco/graphjin/serv"
)

gj, err := serv.NewGraphJinService(conf, opt...)
if err != nil {
 return err
}

if err := gj.Start(); err != nil {
 return err
}
```

![graphjin-screenshot-final](https://user-images.githubusercontent.com/832235/108806955-1c363180-7571-11eb-8bfa-488ece2e51ae.png)

## Support the Project

GraphJin is an open source project made possible by the support of awesome backers. It has collectively saved teams 1000's of hours dev. time and allowing them to focus on their product and be 100x more productive. If your team uses it please consider becoming a sponsor.

<div float="left">
<a href="https://42papers.com">
<img src="https://user-images.githubusercontent.com/832235/135753560-39e34be6-5734-440a-98e7-f7e160c2efb5.png" width="75" target="_blank">
</a>
<a href="https://www.exo.com.ar/">
<img src="https://user-images.githubusercontent.com/832235/112428182-259def80-8d11-11eb-88b8-ccef9206b535.png" width="100" target="_blank">
</a>
</div>

## About GraphJin

After working on several products through my career I found that we spend way too much time on building API backends. Most APIs also need constant updating, and this costs time and money.

It's always the same thing, figure out what the UI needs then build an endpoint for it. Most API code involves struggling with an ORM to query a database and mangle the data into a shape that the UI expects to see.

I didn't want to write this code anymore, I wanted the computer to do it. Enter GraphQL, to me it sounded great, but it still required me to write all the same database query code.

Having worked with compilers before I saw this as a compiler problem. Why not build a compiler that converts GraphQL to highly efficient SQL.

This compiler is what sits at the heart of GraphJin, with layers of useful functionality around it like authentication, remote joins, rails integration, database migrations, and everything else needed for you to build production-ready apps with it.

## Better APIs Faster!

Lets take for example a simple blog app. You'll probably need the following APIs user management, posts, comments, votes. Each of these areas need apis for listing, creating, updating, deleting. Off the top of my head thats like 12 APIs if not more. This is just for managing things for rendering the blog posts, home page, profile page you probably need many more view apis that fetch a whole bunch of things at the same time. This is a lot and we're still talking something simple like a basic blogging app. All these APIs have to be coded up by someone and then the code maintained, updated, made secure, fast, etc. We are talking weeks to months of work if not more. Also remember your mobile and web developers have to wait around till this is all done.

With GraphJin your web and mobile developers can start building instantly. All they have to do is just build the GraphQL queries they need and GraphJin fetches the data. Nothing to maintain no backend API code, its secure, lighting fast and has tons of useful features like subscriptions, rate limiting, etc built-in. With GraphJin your building APIs in minutes not days.

## Features

- Works with Postgres, MySQL8 and YugabyteDB
- Complex nested queries and mutations
- Realtime updates with subscriptions
- Add custom business logic in Javascript
- Build infinite scroll, feeds, nested comments, etc
- Add data validations on insert or update
- Auto learns database tables and relationships
- Role and Attribute-based access control
- Cursor-based efficient pagination
- Full-text search and aggregations
- Automatic persisted queries
- JWT tokens supported (Auth0, JWKS, Firebase, etc)
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
- Instant Hot-deploy and rollbacks
- Add Custom resolvers

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
