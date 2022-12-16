---
chapter: 1
title: Quick Start
description: Quick guide to getting started
---

# Quick Start

Learn what is GraphJin and how to quickly get started using it in your own NodeJS or GO code.

#### TOC

## What is Graphjin?

Most apps need APIs and most APIs require custom code to talk to a database
and return a resulting JSON. Generating this result requires pulling related data from multiple tables etc and putting it all together. This is all a lot of code to write and maintain for every single API.

GraphJin changes all this just write a simple GraphQL query that defines the data you need and GraphJin automagically write the most efficient SQL needed to put all this JSON result together.

The below query will fetch a list of products their owners and various other category information including a cursor to fetch more products.

<span class="mark">
GraphJin will auto discover your database and learn all about it. It will figure out tables, relationships between tables `Foreign Keys`, functions, etc.</span>

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

You get back the result JSON.

```json
{
  "products": [
    {
      "category": [
        {
          "id": 1,
          "name": "Category 1"
        },
        {
          "id": 2,
          "name": "Category 2"
        }
      ],
      "id": 27,
      "name": "Product 27",
      "owner": {
        "category_counts": [
          {
            "category": {
              "name": "Category 1"
            },
            "count": 400
          },
          {
            "category": {
              "name": "Category 2"
            },
            "count": 600
          }
        ],
        "email": "user27@test.com",
        "full_name": "User 27",
        "picture": null
      },
      "price": 37.5
    }
  ],
  "products_cursor": "__gj/enc:/zH/RjGFlpjSsBSq0ZrfWswnTU3NTqdjU5xdF4k"
}
```

## Philosophy of GraphJin

1. Cover all data querying use-cases

2. Treat GraphQL as a schema for your APIs

3. Stay secure, fast and easy to use

## How to use GraphJin with your own code

### NodeJS

Create a NodeJS + Express API with just GraphJin

```js
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
  database: "42papers-development",
});

await db.connect();

// config can either be a file (eg. `dev.yml`) or an object
// const config = { production: true, default_limit: 50 };

var gj = await graphjin("./config", "dev.yml", db);
var app = express();
var server = http.createServer(app);

const query1 = `
    subscription getUpdatedUser { 
        users(id: $userID) { 
            id email 
        } 
    }
`;

const res1 = await gj.subscribe(query1, null, { userID: 2 });
res1.data(function (res) {
  console.log(">", res.data());
});

const query2 = `
    query getUser { 
        users(id: $id) { 
            id 
            email 
            products {
                id
                name
            }
        } 
    }
`;

app.get("/", async function (req, resp) {
  const res2 = await gj.query(query2, { id: 1 }, { userID: 1 });
  resp.send(res2.data());
});

server.listen(3000);
console.log("Express server started on port %s", server.address().port);
```

Alternatively you can also put the query into a query file

```graphql title="Fragment File ./config/queries/getUser.gql"
query getUser {
  users(id: $id) {
    id
    email
    products {
      id
      name
    }
  }
}
```

And then use the `queryByName` or `subscribeByName` API to refer to it. This way your queries are kept in one place and you can avail of the syntax highting and linting that your IDE provides for GraphQL.

```js
const res = await gj.queryByName("getUser", { id: 1 }, { userID: 1 });
console.log(res.data());
```

```js
const res = await gj.subscribeByName("getUser", null, { userID: 2 });
res.data(function (res) {
  console.log(res.data());
});
```

### GoLang

Create a Go + Chi Router API with just GraphJin

```go
package main

import (
  "context"
  "database/sql"
  "fmt"
  "log"

  "github.com/dosco/graphjin/core"
  "github.com/go-chi/chi/v5"
    _ "github.com/jackc/pgx/v5/stdlib"
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
    query getPosts {
        posts {
            id
            title
        }
        posts_cursor
    }`

    r := chi.NewRouter()
    r.Get("/", func(w http.ResponseoWriter, r *http.Request) {
        c := context.WithValue(r.Context(), core.UserIDKey, 1)
        res, err := gj.GraphQL(c, query, nil, nil)
        if err != nil {
            log.Error(err)
            return
        }
        w.Write(res.Data)
    })

    http.ListenAndServe(":3000", r)
    log.Println("Go server started on port 3000");
}
```

Alternatively you can also put the query into a query file

```graphql title="Fragment File ./config/queries/getUser.gql"
query getUser {
  users(id: $id) {
    id
    email
    products {
      id
      name
    }
  }
}
```

And then use the `QueryByName` or `SubscribeByName` API to refer to it. This way your queries are kept in one place and you can avail of the syntax highting and linting that your IDE provides for GraphQL.

```go
vars := json.RawMessage(`{ "id": 1 }`)
res := gj.GraphQLByName("getUser", vars, nil);
fmt.Println(string(res.Data));
```

```go
vars := json.RawMessage(`{ "id": 1 }`)
res := await gj.SubscribeByName("getUser", vars, nil);
for {
    msg := <-m.Result
    fmt.Println(string(res.Data))
}
```

## Learn More

https://www.youtube.com/watch?v=4zXy-4gFSpQ

https://www.youtube.com/watch?v=gzAiAbsCMVA
