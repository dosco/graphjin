# GraphJin, A New Kind of ORM

[![Apache 2.0](https://img.shields.io/github/license/dosco/graphjin.svg?style=for-the-badge)](https://github.com/dosco/graphjin/blob/master/LICENSE)
[![NPM Package](https://img.shields.io/npm/v/graphjin?style=for-the-badge)](https://www.npmjs.com/package/graphjin)
[![Docker Pulls](https://img.shields.io/docker/pulls/dosco/graphjin?style=for-the-badge)](https://hub.docker.com/r/dosco/graphjin/builds)
[![Discord Chat](https://img.shields.io/discord/628796009539043348.svg?style=for-the-badge&logo=discord)](https://discord.gg/6pSWCTZ)
[![GoDoc](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/dosco/graphjin/core/v3)
[![GoReport](https://goreportcard.com/badge/github.com/gojp/goreportcard?style=for-the-badge)](https://goreportcard.com/report/github.com/dosco/graphjin/core/v3)

## Build APIs in 5 minutes not weeks

Just use a simple GraphQL query to define your API and GraphJin automagically converts it into SQL and fetches the data you need. Build your backend APIs **100X** faster. Works with **NodeJS** and **GO**. Supports several databases, **Postgres**, **MySQL**, **Yugabyte**, **AWS Aurora/RDS** and **Google Cloud SQL**

The following GraphQL query fetches a list of products, their owners, and other category information, including a cursor for retrieving more products.
GraphJin would do auto-discovery of your database schema and relationships and generate the most efficient single SQL query to fetch all this data including a cursor to fetch the next 20 times. You don't have to do a single thing besides write the GraphQL query.

```graphql
query getProducts {
  products(
    # returns only 20 items
    limit: 20

    # orders the response items by highest price
    order_by: { price: desc }

    # only items with a price >= 20 and < 50 are returned
    where: { price: { and: { greater_or_equals: 20, lt: 50 } } }
  ) {
    id
    name
    price

    # also fetch the owner of the product
    owner {
      full_name
      picture: avatar
      email

      # and the categories the owner has products under
      category_counts(limit: 3) {
        count
        category {
          name
        }
      }
    }

    # and the categories of the product itself
    category(limit: 3) {
      id
      name
    }
  }
  # also return a cursor that we can use to fetch the next
  # batch of products
  products_cursor
}
```

## Secure out of the box

In production all queries are always read from locally saved copies not from what the client sends hence clients cannot modify the query. This makes
GraphJin very secure as its similar to building APIs by hand. The idea that GraphQL means that clients can change the query as they wish **does not** apply to GraphJin

## Great Documentation

Detailed docs on GraphQL syntax, usecases, JS and GO code examples and it's actively updated.

## [![Docs](https://img.shields.io/badge/Docs-graphjin.com-red?style=for-the-badge)](https://graphjin.com)

## [![Example Code](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=for-the-badge&logo=go&label=Example+Code)](https://pkg.go.dev/github.com/dosco/graphjin/tests/v3)

## Use with NodeJS

GraphJin allows you to use GraphQL and the full power of GraphJin to access to create instant APIs without writing and maintaining lines and lines of database code. GraphJin NodeJS currently only supports Postgres compatible databases working on adding MySQL support as well. Example app in [/examples/nodejs](https://github.com/dosco/graphjin/tree/master/examples/nodejs)

```console
npm install graphjin
```

```javascript
import graphjin from "graphjin";
import express from "express";
import http from "http";
import pg from "pg";

const { Client } = pg;
const db = new Client({
  host: "localhost",
  port: 5432,
  user: "postgres",
  password: "postgres",
  database: "appdb-development",
});

await db.connect();

// config can either be a file (eg. `dev.yml`) or an object
// const config = { production: true, default_limit: 50 };

var gj = await graphjin("./config", "dev.yml", db);
var app = express();
var server = http.createServer(app);

// subscriptions allow you to have a callback function triggerd
// automatically when data in your database changes
const res1 = await gj.subscribe(
  "subscription getUpdatedUser { users(id: $userID) { id email } }",
  null,
  { userID: 2 }
);

res1.data(function (res) {
  console.log(">", res.data());
});

// queries allow you to use graphql to query and update your database
app.get("/", async function (req, resp) {
  const res2 = await gj.query(
    "query getUser { users(id: $id) { id email } }",
    { id: 1 },
    { userID: 1 }
  );

  resp.send(res2.data());
});

server.listen(3000);
console.log("Express server started on port %s", server.address().port);
```

## Use with GO

You can use GraphJin as a library within your own code. The [serv](https://pkg.go.dev/github.com/dosco/graphjin/serv/v3) package exposes the entirely GraphJin standlone service as a library while the [core](https://pkg.go.dev/github.com/dosco/graphjin/core/v3) package exposes just the GraphJin compiler. The [Go docs](https://pkg.go.dev/github.com/dosco/graphjin/tests/v3#pkg-examples) are filled with examples on how to use GraphJin within your own apps as a sort of alternative to using ORM packages. GraphJin allows you to use GraphQL and the full power of GraphJin to access your data instead of a limiting ORM.

### Use GraphJin Core

```console
go get github.com/dosco/graphjin/core/v3
```

```golang
package main

import (
  "context"
  "database/sql"
  "fmt"
  "log"

  "github.com/dosco/graphjin/core/v3"
  _ "github.com/jackc/pgx/v4/stdlib"
)

func main() {
  db, err := sql.Open("pgx", "postgres://postgres:@localhost:5432/example_db")
  if err != nil {
    log.Fatal(err)
  }

  gj, err := core.NewGraphJin(nil, db)
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

  res, err := gj.GraphQL(ctx, query, nil, nil)
  if err != nil {
    log.Fatal(err)
  }

  fmt.Println(string(res.Data))
}
```

### Use GraphJin Service

```golang
import (
  "github.com/dosco/graphjin/serv/v2"
)

gj, err := serv.NewGraphJinService(conf, opt...)
if err != nil {
 return err
}

if err := gj.Start(); err != nil {
 return err
}

// if err := gj.Attach(chiRouter); err != nil {
//  return err
// }
```

## Standalone Service

### Quick install

```
# Mac (Homebrew)
brew install dosco/graphjin/graphjin

# Ubuntu (Snap)
sudo snap install --classic graphjin
```

Debian and Redhat ([releases](https://github.com/dosco/graphjin/releases))
Download the .deb or .rpm from the releases page and install with dpkg -i and rpm -i respectively.

### Quickly create and deploy new apps

```bash
graphjin new <app_name>
```

### Instantly deploy new versions

```bash
# Deploy a new config
graphjin deploy --host=https://your-server.com --secret="your-secret-key"

# Rollback the last deployment
graphjin deploy rollback --host=https://your-server.com --secret="your-secret-key"
```

### Secrets Management

```bash
# Secure save secrets like database passwords and JWT secret keys
graphjin secrets
```

### Database Management

```bash
# Create, Migrate and Seed your database
graphjin db
```

## Built in Web-UI to help craft GraphQL queries

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

## Highlevel

- Works with Postgres, MySQL8, YugabyteDB
- Also works with Amazon Aurora/RDS and Google Cloud SQL
- Supports REST, GraphQL and Websocket APIs

## More Features

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

[Quick Start](https://graphjin.com/posts/start)

[Documentation](https://graphjin.com)

[GraphJin GO Examples](https://pkg.go.dev/github.com/dosco/graphjin/core#pkg-examples)

## Reach out

We're happy to help you leverage GraphJin reach out if you have questions

[twitter/dosco](https://twitter.com/dosco)

[discord/graphjin](https://discord.gg/6pSWCTZ) (Chat)

## Production use

The popular [42papers.com](https://42papers.com) site for discovering trending papers in AI and Computer Science uses GraphJin for it's entire backend.

## License

[Apache Public License 2.0](https://opensource.org/licenses/Apache-2.0)

Copyright (c) 2022 Vikram Rangnekar
