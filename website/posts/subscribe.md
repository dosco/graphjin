---
chapter: 5
title: Subscribe
description: Use subscriptions to get notified when the query result changes
---

# Subscribe

You can subscribe to a query and receive live updates whenever the result of the query changes. The query can be as large and nested as you need it to be.

<mark>
This is by far one of the coolest features of GraphJin and you'd be hardpressed to find similar functionality elsewhere.
</mark>

#### TOC

---

```graphql
subscription getNewComments {
  comments(where: { post_id: { eq: $post_id } }) {
    id
    body
    author: user {
      id
      name
      bio
    }
  }
}
```

### Using subscriptions

If you use the standalone service then subscriptions will work over websockets using a GraphQL client like [Apollo](https://www.apollographql.com/docs/react/) or [URQL](https://formidable.com/open-source/urql/)

If your using GraphJin as a library in your NodeJS or Go code then read ahead.

```graphql
subscription getUserUpdates {
  users(id: $id) {
    id
    email
    phone
  }
}
```

```go title="NodeJS code"
const res = await gj.subscribeByName("getUserUpdates", { "id": 3 })

res.data(function(res) {
    console.log(res.data())
})
```

```go title="GO code"
c := context.Background()
vars := json.RawMessage(`{ "id": 3 }`)

m, err := gj.SubscribeByName(c, "getUserUpdates", vars, nil)
if err != nil {
  return err
}
for {
  msg := <-m.Result
  print(string(msg.Data))
}
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

### Highly scalable

In GraphJin subscriptions are designed to be highly scalable. You can easily handle tens of thousands of subscribers on a relatively a basic server, even your database server can be pretty basic. This is because GraphJin uses only a single database query for thousands of connections.

Compare this to traditional client side polling solutions which require as many query as subscribers. With GraphJin you can now add realtime updates to your application without the worry of database load.

For very large deployments it scales horizontally and vertically as in can leverage more CPU and memory added per instance as well as read-replicas or a distributed database like Yugabyte.

No additional configuration is needed for subscriptions except for the `poll_every_seconds: 3` config parameter to control how often GraphJin should check for updates.
