---
chapter: 9
title: NodeJS
description: Using the Javascript API
---

# NodeJS

#### TOC

### Add GraphJin

Add Graphjin to your Node application. On install it will create a `./config` folder with a sample `dev.yml` and `prod.yml` config files.

```shell
npm i graphjin
```

<mark>
👋 In production it is <b>very</b> important that you run GraphJin in production mode to do this you can use the `prod.yml` config which already has `production: true` or if you're using a config object then set it manually
</mark>

```yaml title="Config File prod.yml"
# When enabled GraphJin runs with production level security defaults.
# For example only queries from saved in the queries folder can be used.
production: true
```

```js title="Javascript config object"
const config = { production: true, default_limit: 50 };
```

### Using GraphJin

```js
import graphjin from "graphjin";

// config can be a filename
const cf = process.env.NODE_ENV === "production" ? "prod.yml" : "dev.yml";

// or config can be an object
// const config = { production: true, default_limit: 50 }

const gj = await graphjin("./config", cf, db);
```

### Whats `db` ?

Its the database client, currently we only support the popular
Postgres client `pg`. Remeber to call `db.connect()`

```js
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
```

### Your first query

The `query` is the graphql query, the `variables` are the variables required by this query and the options are things like `{ userID: 1 }` to set the user identifier for the query ($user_id).

```js
const result = await gj.query("query", <variables>, <options>)
```

If you would rather use a `.gql` or `.graphql` file for the query place it under `./config/queries` and use the `queryByName` API instead. <mark>`query name` is the filename of the query (minus the extension)</mark>

```js
const result = await gj.queryByName("query name", <variables>, <options>)
```

Lets put this all together and query for the `full_name` and `email` of a user by his `id` ($id). Keep in mind you will need to have a `users` table with `full_name` and `email` columns in your database for this to work.

```js
const res = await gj.query(
  "query getUser { users(id: $id) { full_name email } }",
  { id: 1 },
  { userID: 1 }
);
```

Alternatively using `queryByName`

```graphql title="./config/queries/getUser.gql"
query getUser {
  users(id: $id) {
    full_name
    email
  }
}
```

```js
const res = await gj.queryByName("getUser", { id: 1 }, { userID: 1 });
```

Get the result

```js
console.log(res.data());
```

```json title="Result"
{
  "users": {
    "full_name": "Andy Anderson",
    "email": "andyskates@hotmail.com"
  }
}
```

### Using subscriptions

Did you ever need to have database changes streamed back to you in realtime. For example new sales that happened, comments added to a blog post, new likes that you want to stream back over websockets, whatever. This is not easy to implement efficiently. But with GraphJin its just as easy as making the above query and is designed to be very efficient.

A subscription query is just a normal query with the prefix `subscription`.

```js
const result = await gj.subscribe("query", <variables>, <options>)
```

Use the `subscribe` API that works similar to `query` in production mode
only allows you to use queries from the queries folder.

```js
const res = await gj.subscribe(
  "subscription getUpdatedUser { users(id: $userID) { id email } }",
  { id: 1 },
  { userID: 1 }
);
```

Alterntively you can use the `subscribeByName` API which is similar to the `queryByName` API.

```js
const res = await gj.subscribeByName(
  "getUpdatedUser",
  { id: 1 },
  { userID: 1 }
);
```

Getting the updates back from a subscription is a little different you have to use a callback since the results keep coming.

```js
res.data(function (res1) {
  console.log(res1.data());
});
```

```json title="Result"
{"users":{"email":"user3@test.com","id":3,"phone":null}}
{"users":{"email":"user3@test.com","id":3,"phone":"650-447-0000"}}
{"users":{"email":"user3@test.com","id":3,"phone":"650-447-0001"}}
{"users":{"email":"user3@test.com","id":3,"phone":"650-447-0002"}}
{"users":{"email":"user3@test.com","id":3,"phone":"650-447-0003"}}
{"users":{"email":"user3@test.com","id":3,"phone":"650-447-0004"}}
{"users":{"email":"user3@test.com","id":3,"phone":"650-447-0005"}}
{"users":{"email":"user3@test.com","id":3,"phone":"650-447-0006"}}
{"users":{"email":"user3@test.com","id":3,"phone":"650-447-0007"}}
{"users":{"email":"user3@test.com","id":3,"phone":"650-447-0008"}}
```
