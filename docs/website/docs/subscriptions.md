---
id: subscriptions
title: GraphQL Subscriptions
sidebar_label: Subscriptions
---

Easily the coolest features in GraphJin, GraphQL subscriptions can be used to subscribe to a GraphQL query and receive near-realtime updates on any database updates
related to that query.

```graphql
subscription newComments {
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

## Highly Scalable

In GraphJin subscriptions are designed to be highly scalable. You can easily handle tens of thousands of subscribers on a relatively a basic server, even your database server can be pretty basic. This is because GraphJin uses only a single database query for thousands of connections.

Compare this to traditional client side polling solutions which require as many query as subscribers. With GraphJin you can now add realtime updates to your application without the worry of database load.

For very large deployments it scales horizontally and vertically as in can leverage more CPU and memory added per instance as well as read-replicas or a distributed database like Yugabyte.

No additional configuration is needed for subscriptions except for the `poll_every_seconds: 3` config parameter to control how often super graph should check for updates. Default value is every 5 seconds.
