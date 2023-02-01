---
chapter: 1
title: Start
description: Quick guide to getting started
---

# Quick Start

Learn what is GraphJin and how to quickly get started using it in your own NodeJS or GO code.

#### TOC

## Why use GraphJin

🔑 Build apps faster
🔒 Secure, no dependencies
🚀 Fast, NodeJS+GO+WASM
😊 Easy small API
📚 Great documentation
🤟🏽 Actively developed

## What is Graphjin?

APIs are used by many apps to retrieve data from a database and return it in a JSON format. Writing and maintaining the code for this process can be time-consuming.

GraphJin simplifies this by allowing you to write a simple GraphQL query that defines the data you need. GraphJin will then automatically generate the necessary SQL code to retrieve and combine the data in a JSON result.

GraphJin requires almost no configuration it does automatic disovery of your database schema and relationships and can instantly start converting (compiling) your GraphQL into an efficient SQL query.

## Why GraphQL?

Traditionally you might think of GraphQL as something you use in a client app but we think thats not a great idea for most folks as it adds unnecessary complexity and bloat on the client. Instead we treat GraphQL as a simple and easy language to define the data your need and its structure. And GraphJin does the work of writing the most efficient SQL to fetch that data for you.

The following GraphQL query fetches a list of products, their owners, and other category information, including a cursor for retrieving more products.

<mark>GraphJin will auto discover your database and learn all about it. It will figure out tables, relationships between tables `Foreign Keys`, functions, etc.</mark>

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

## Why should I care?

Imagine you are building a simple blog app. You'll likely need APIs for user management, posts, comments, and votes, each of which requires multiple APIs for listing, creating, updating, and deleting. That's a minimum of 12 APIs, and that's just for managing the data. To render the blog posts, home page, and profile page, you'll probably need even more APIs to fetch a variety of data at the same time. This can take weeks or months to code and maintain, and as your team grows, you'll need to ensure that everyone is making efficient database calls and avoiding issues like N+1 calls and SQL injection bugs.

Instead of spending all of this time and effort coding and maintaining individual APIs, wouldn't it be easier to use a tool like GraphJin to define exactly what you want to happen with a quick and simple GraphQL query? GraphJin handles all of the behind-the-scenes work for you, so you never have to worry about inefficiencies or security issues. With GraphJin, you can build APIs in minutes instead of days.

## Use GraphJin in NodeJS

Create a NodeJS + Express API with just GraphJin. <mark>For futher details goto the [GraphJin NodeJS docs](nodejs)</mark>

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

[GraphJin NodeJS docs](nodejs)

## Use GraphJin in GO

Create a Go + Chi Router API with just GraphJin. <mark>For futher details goto the [GraphJin GO docs](go)</mark>

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

[GraphJin GO docs](go)

<!--
## Learn More

https://www.youtube.com/watch?v=4zXy-4gFSpQ

https://www.youtube.com/watch?v=gzAiAbsCMVA -->
