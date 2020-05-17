---
id: react
title: ReactJS Examples
sidebar_label: ReactJS Examples
---

## Apollo Client

We recommend using the new [Apollo Client v3](https://www.apollographql.com/docs/react/v3.0-beta/) `@apollo/client` this is the latest version of the Apollo GraphQL javascript client library. Previous versions of this library were called `apollo-boost`.

This library contains react hooks `useQuery`, `useMutation`, `useLazyQuery`, etc that make it easy to add GraphQL queries to your React app.

```bash
npm install @apollo/client graphql
```

### Creating a client

```jsx title="App.js"
import React from "react";
import { ApolloClient, ApolloProvider } from "@apollo/client";

const client = new ApolloClient({ uri: `/api/v1/graphql` });
```

### Or a client with caching enabled

```jsx title="App.js"
import React from "react";
import { ApolloClient, ApolloProvider, InMemoryCache } from "@apollo/client";

// Enable GraphQL caching really speeds up your app
const cache = new InMemoryCache({
  freezeResults: true,

  // Set `dataIdFromObject` id is not called `id`
  // dataIdFromObject: obj => { return obj.slug }
});

const client = new ApolloClient({ uri: `/api/v1/graphql`, cache: cache });
```

### Use the client in components to query for data

In this example we create a component `UserProfile` that user's name by his `id` and displays that on the page.

```jsx
import React  from "react";
import { gql, useQuery } from "@apollo/client";

const UserProfile = { id } => {
  const query = gql`
    query getUser {
      user(id: $id) {
        id
        name
      }
    }`;

  const variables = { id };
  const { error, loading, data } = useQuery(query, { variables });

  if (loading) {
    return <p>Loading</p>;
  }

  return <h1>{data.name}</h1>
}

```

### Use the client in components to post data back

In this example we create a component `UpdateUserProfile` displays a text input which when changed causes a mutation query to be trigger to update the users name in the backend database. You can also insert or delete in mutation. More complex mutations like bulk insert, or nested insert and updates are also supported. Nested inserts are create to create an entry and create or update a related entitiy in the same request.

```jsx
import React  from "react";
import { gql, useQuery } from "@apollo/client";

const UpdateUserProfile = { id } => {
  const query = gql`
    mutation setUserName {
      user(id: $id, update: $data) {
        id
        name
      }
    }`;

  const [setUserName] = useMutation(query);

  const updateName = (e) => {
    let name = e.target.value;
    let variables = { data: { name } };

    setUserName({ variables })
  }

  return <input type="text" onChange={updateName}>
}

```
