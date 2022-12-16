---
chapter: 2
title: Query
description: Query tables, Nested queries, Cursor pagination, Sorting, Searching, Using functions
---

## Query

#### TOC

### Fetch from various related tables

> Fetch the lastest 10 products and their owners

```graphql
query {
  products(limit: 10, order: { created_at: desc }) {
    id
    name
    owner {
      id
      email
    }
  }
}
```

> Fetch last 5 users. Use the common shared user fragment

```graphql
#import "./fragments/User.gql"

query getUser {
  users(order_by: { created_at: desc }, limit: 5) {
    ...User
  }
}
```

```graphql title="Fragment File ./fragments/User.gql"
fragment User on users {
  id
  email
  full_name
  created_at
  category_counts {
    category {
      id
      name
    }
    count
  }
}
```

### Fetch from multiple unrelated tables

> Fetch the current user and the last 5 latest products and purchases
> with a single query so we can quickly render a home page

```graphql
query getCurrentUserLatestProductsAndPurchases {
  currentUser: users(id: $user_id) {
    id
    email
  }
  latestProducts: products(last: 5) {
    id
    name
    customer {
      email
    }
  }
  latestPurchases: purchases(last 5) {
    id
  }
  products_cursor
  purchases_cursor
}
```

### Cursor Pagination

The ability to fetch the next batch of results is key to building any efficient app. This is needed if your building an infinite scroll or a fetch next page feature. This is hard to do across all your APIs and even harder to do this efficiently. But for us this is a breeze.

<span class="mark">
GraphJin returns an opaque cursor (it's encrypted) to get the next batch of results all you have to do is pass that cursor back with the next API call
</span>

```json title="Query Variables"
{ "cursor": "__gj/enc:/zH/MJoTLbQF4l0GuoDsYmCrpjPeaaIlNpfm4uFU4PQ" }
```

> We need to fetch the next batch of products starting from the oldest

```graphql
query getOldestProducts {
  products(first: 10, after: $cursor) {
    slug
    name
  }
  products_cursor
}
```

> We need to fetch the next batch of products starting from the newest

```graphql
query getNewestProducts {
  products(last: 10, before: $cursor) {
    slug
    name
  }
  products_cursor
}
```

```json title="Result"
"data": {
   "products": [
    {
      "slug": "eius-nulla-et-8",
      "name" "Pale Ale"
    },
    {
      "slug": "sapiente-ut-alias-12",
      "name" "Brown Ale"
    }
    ...
  ],
  "products_cursor": "__gj/enc:/zH/RjGFlpjSsBSq0ZrfWswnTU3NTqdjU5xdF4k"
}
```

```yaml title="Config for cursor encryption key"
secret_key: supercalifajalistics
```

### Various GraphQL features

> Fetch products by their id but allow us to control if we want the product id and owner returned as well. Also rename email column on the owner to `ownerEmail`

```json title="Query Variables"
{
  "product_id" 2,
  "include_id": false,
  "dont_include_owner": true
}
```

```graphql
query getProductsWithSpecificOwners {
  products(id: $product_id) {
    id @include(if: $include_id)
    name
    description
    owner @skip(if: $dont_include_owner) {
      id
      ownerEmail: email
    }
  }
}
```

> If you rather use camel case for my queries instead of the snake case that my database tables and columns use. GraphJin will auto translate between the two.

```yaml title="Config File ./config/dev.yml"
enable_camelcase: true
```

```graphql
query getUsers {
  users {
  fullName
  createdAt
  categoryCounts {
    count
  }
}
```

### Filtering options

> Fetch all products from a list of ids where the price is greather than 20 or lesser than 22

```graphql
query {
  products(
    limit: 3
    where: {
      and: {
        id: { in: [1, 2, 3, 4, 5] }
        or: { price: { greater_than: 20 }, price: { lesser_than: 22 } }
      }
    }
  ) {
    id
    name
    price
  }
}
```

### Fetch based on the value of a related table

> We need to fetch all products owned by a user where we only have the users
> email. Instead of first looking up the user then his products we need to
> do this with a single query

```graphql
query getProductsWithSpecificOwners {
  products(where: { owner: { email: { eq: $owner_email } } }, limit: 2) {
    id
    owner {
      id
      email
    }
  }
}
```

### Dynamically changing the sort order of the result

> You have a UI where customers can change how the products displayed are ordered. Since ordering requires databases index for each sort order you want to limit the ordering options to a set of fixed choices.

```yaml title="Config File"
tables:
  - name: products
    order_by:
      most_expensive_products: ["price desc", "created_at desc"]
      least_expensive_products: ["price asc"]
```

```json title="Query Variables"
{ "order": "most_expensive_products" }
```

```graphql
query getProducts {
  products(order_by: $order, limit: 5) {
    id
    name
    description
    price
  }
  products_cursor
}
```

### Fetching by a list of ids

> You have a seperate search service (eg. Elastic Search) that has returned a list of ids, you now have to fetch those products from the database. Also you have to ordering of the returned products is in the same order as the list of ids.

```json title="Query Variables"
{ "ids": [5, 3, 2, 1, 9] }
```

```graphql
query getProducts {
  products(
    order_by: { id: [$ids, "asc"] }
    where: { id: { in: $ids } }
    limit: 5
  ) {
    id
    name
    description
    price
  }
}
```

### Using functions

> You can either use custom database functions or built-in ones. Your function can either return a record or custom type or a single value. Records and custom types are treated as if they are as table themselves. The `is_hot_product(id)` function uses the `id` of the product to check if its considered a hot product or not.

```graphql title="Arguments as a list"
query getProduct {
		products(id: 51) {
			id
			name
      is_hot_product(args: [id])
		}
	}`
```

```graphql title="Using named arguments"
query getProduct {
		products(id: 51) {
			id
			name
      is_hot_product(id: id)
		}
	}`
```

> The `get_oldest5_products` function returns a custom type or a record and is therefore treated as if its a table and you can use all the standard table argument like `limit`, `where`, `order_by` etc. Additionally you can also pass it the arguments that the function itself requires.

```graphql title="Argument as a list"
query {
  get_oldest5_products(limit: 3, args: [4, $tag]) {
    id
    name
  }
}
```

```graphql title="Using named arguments"
query {
  get_oldest_users(limit: 2, user_count: 4, tag: $tag) {
    id
    name
  }
}
```

### Variable Limit

> You can use a variable for the number of records to return. The default max is 20 but that can be customized per table.

```yaml title="Config File"
default_limit: 25
roles:
  - name: user
    tables:
      - name: products
        query:
          limit: 10
```

```graphql
query {
  products(limit: $limit) {
    id
  }
}
```

### Graph or recursive queries

> These are common for tables that reference themselves like with theaded comments or some data with a hierarchical order.

```sql title="A table that references itself"
CREATE TABLE comments (
  id BIGSERIAL PRIMARY KEY,
  body TEXT,
  commenter_id BIGINT REFERENCES users(id),
  reply_to_id BIGINT REFERENCES comments(id),
);
```

```graphql
query getCommentThread {
  comments {
    id
    body
    comments(find: "parents") {
      id
      body
    }
  }
}
```
