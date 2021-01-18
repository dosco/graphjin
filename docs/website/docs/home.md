---
id: home
title: GraphJin
hide_title: true
sidebar_label: Home
---

import useBaseUrl from '@docusaurus/useBaseUrl'; // Add to the top of the file below the front matter.

<div class="hero shadow--lw margin-bottom--lg">
  <div class="container">
    <div class="row">
      <div class="col col--2">
        <img
          class=""
          alt="GraphJin Logo"
          src={useBaseUrl('img/graphjin-logo.svg')}
          height="70"
        />
      </div>
      <div class="col col--10"><h1 class="hero__title">GraphJin</h1></div>
    </div>
    <p class="hero__subtitle">Fetch data without code!</p>
    <p>Stop fighting ORM's and complex SQL just to fetch the data you need. Instead try GraphJin it automagically tranforms GraphQL into efficient SQL. </p>
    <div class="margin-bottom--lg">
    <a class="button button--secondary button--outline button--lg" href="start">
      Skip Intro
    </a>
  </div>
  </div>
</div>

### Work on the things that matter, leave the boring database stuff to us.

80% of all web app development is either reading from or writing to a database. 100x your developer productivity and save valuable time by making that super simple.

### Fetching data with GraphQL

Just imagine the code or SQL you'll need to fetch this data, the user, all his posts, all the votes on the posts, the authors information and the related tags. Oh yeah and you also need efficient cursor based pagination. And Remember you also need to maintain this code forever.

Instead just describe the data you need in GraphQL and give that to GraphJin it'll automatically learn your database and generate the most efficient SQL query fetching your data in the JSON structure you expected.

```graphql
query {
  user(id: 5) {
    id
    first_name
    last_name
    picture_url
    posts(first: 20, order_by: { score: desc }) {
      slug
      title
      created_at
      votes_total
      votes {
        created_at
      }
      author {
        id
        name
      }
      tags {
        id
        name
      }
    }
    posts_cursor
  }
}
```

### Instant results

Here's the data GraphJin fetched using the GraphQL above, it's even in the JSON structure you
wanted it in. All this without you writing any code or SQL.

```json
{
  "data": {
    "user": {
      "id": 5,
      "threads": [
        {
          "id": 1,
          "title": "This is a sample tite for this thread.",
          "topics": [
            {
              "id": 3,
              "name": "CloudRun"
            }
          ]
        }]
      },
      "posts": [
        {
          "id": 1477,
          "body": "These are some example contents for this post.",
          "slug": "monitor-negotiate-store-1476",
          "votes": [],
          "author": {
            "id": 5,
            "email": "jordanecruickshank@ferry.io"
          },
          "created_at": "2020-05-13T13:51:21.729501"
        },
        ...
      ],
      "posts_cursor": "a8d4j2k9d83dy373hd2nskw2sjs8"
  }
}
```

## How do I use it?

GraphJin can be used in two ways. You can run it as a standalone service serving as an API backend for your app or as a library within your own app code. GraphJin is built in GO a secure and high-performance language from Google used to build cloud infratructure.

### Using it as a service

```bash
go get github.com/dosco/graphjin
graphjin new <app_name>
cd <app_name>
docker-compose run api db:setup
docker-compose up
```

### Using it in your own code

```bash
go get github.com/dosco/graphjin/core
```

```go
package main

import (
  "database/sql"
  "fmt"
  "time"
  "github.com/dosco/graphjin/core"
  _ "github.com/jackc/pgx/v4/stdlib"
)

func main() {
  db, err := sql.Open("pgx", "postgres://postgrs:@localhost:5432/example_db")
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

  ctx = context.WithValue(ctx, core.UserIDKey, 1)

  res, err := sg.GraphQL(ctx, query, nil)
  if err != nil {
    log.Fatal(err)
  }

  fmt.Println(string(res.Data))
}
```

## Follow us for updates

For when you need help or just want to stay in the loop
[Twitter](https://twitter.com/dosco) or [Discord](https://discord.gg/6pSWCTZ).

## Why I created GraphJin

After working on several products through my career I found that we spend way too much time on building API backends. Most APIs also need constant updating, and this costs time and money.

It's always the same thing, figure out what the UI needs then build an endpoint for it. Most API code involves struggling with an ORM to query a database and mangle the data into a shape that the UI expects to see.

I didn't want to write this code anymore, I wanted the computer to do it. Enter GraphQL, to me it sounded great, but it still required me to write all the same database query code.

Having worked with compilers before I saw this as a compiler problem. Why not build a compiler that converts GraphQL to highly efficient SQL.

This compiler is what sits at the heart of GraphJin, with layers of useful functionality around it like authentication, remote joins, rails integration, database migrations, and everything else needed for you to build production-ready apps with it.

## Apache License 2.0

Apache Public License 2.0 | Copyright © 2018-present Vikram Rangnekar
