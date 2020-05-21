---
id: home
title: Super Graph
hide_title: true
sidebar_label: Home
---

import useBaseUrl from '@docusaurus/useBaseUrl'; // Add to the top of the file below the front matter.

<div class="hero shadow--lw margin-bottom--lg">
  <div class="container">
    <div class="row">
      <div class="col col--2">  
        <img 
          class="avatar__photo avatar__photo--xl"
          alt="Super Graph Logo" 
          src={useBaseUrl('img/super-graph-logo.svg')} 
          height="70" 
        />
      </div>
      <div class="col col--10"><h1 class="hero__title">Super Graph</h1></div>
    </div>
    <p class="hero__subtitle">Fetch data without code!</p>
    <div class="margin-bottom--lg">
      <a class="button button--secondary button--outline button--lg" href="start">
        Skip Intro
      </a>
    </div>
    <p>Stop fighting ORM's and complex SQL just to fetch the data you need. Instead try Super Graph it automagically tranforms GraphQL into efficient SQL.</p>
  </div>
</div>

:::info cut development time
80% of all web app development is either reading from or writing to a database. 100x your developer productivity and save valuable time by making that super simple.
:::

### Fetching data with GraphQL

Just imagine the code or SQL you'll need to fetch this data, the user, all his posts, all the votes on the posts, the authors information and the related tags. Oh yeah and you also need efficient cursor based pagination. And Remember you also need to maintain this code forever.

Instead just describe the data you need in GraphQL and give that to Super Graph it'll automatically learn your database and generate the most efficient SQL query fetching your data in the JSON structure you expected.

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

Here's the data Super Graph fetched using the GraphQL above, it's even in the JSON structure you
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
          "created_at": "2020-05-13T13:51:21.729501+00:00"
        },
        ...
      ],
      "posts_cursor": "a8d4j2k9d83dy373hd2nskw2sjs8"
  }
}
```

## How do I use it?

Super Graph can be used in two ways. You can run it as a standalone service serving as an API backend for your app or as a library within your own app code. Super Graph is built in GO a secure and high-performance language from Google used to build cloud infratructure.

### Using it as a service

```bash
go get github.com/dosco/super-graph
super-graph new <app_name>
```

### Using it in your own code

```bash
go get github.com/dosco/super-graph/core
```

```go
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

## Follow us for updates

For when you need help or just want to stay in the loop
[Twitter](https://twitter.com/dosco) or [Discord](https://discord.gg/6pSWCTZ).

## Why I created Super Graph

After working on several products through my career I found that we spend way too much time on building API backends. Most APIs also need constant updating, and this costs time and money.

It's always the same thing, figure out what the UI needs then build an endpoint for it. Most API code involves struggling with an ORM to query a database and mangle the data into a shape that the UI expects to see.

I didn't want to write this code anymore, I wanted the computer to do it. Enter GraphQL, to me it sounded great, but it still required me to write all the same database query code.

Having worked with compilers before I saw this as a compiler problem. Why not build a compiler that converts GraphQL to highly efficient SQL.

This compiler is what sits at the heart of Super Graph, with layers of useful functionality around it like authentication, remote joins, rails integration, database migrations, and everything else needed for you to build production-ready apps with it.

## Apache License 2.0

Apache Public License 2.0 | Copyright © 2018-present Vikram Rangnekar
