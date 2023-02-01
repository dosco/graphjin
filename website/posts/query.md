---
chapter: 2
title: Query
description: Query tables, Nested queries, Cursor pagination, Sorting, Searching, Using functions
---

# Query

#### TOC

### Query basics

Everything in GraphJin resolves around the GraphQL query. Every query must have a type and a name. Types are `query` for queries, `mutation` for update, insert, upsert, delete and `subscription` for live queries.

Queries have selectors (tables) which can have arguments to [filter `where:`](cheatsheet#crafting-the-where-clause), target `id:`, limit `limit:` or [sort `order_by:`](query#sorting-the-query-result) the result.

Queries can also use variables (eg. `$name`). These variables can either be passed in with the query or preset in the config. Some variables are special like `$user_id` which requires a user id to be set on the query. There is also a concept called [roles](cheatsheet#roles-for-access-control) that you can use for access control.

Query selectors (tables) can have other selectors nested under them. The name of the nested selector is the same of the foreign key (relationship) column minus the `_id` prefix/suffix. For example if the products table has a foreign key column `owner_id` pointing to the users table then you would use `owner` as the nested selector.

```graphql
query getProducts {
  id
  name
  owner {
    id
    full_name
  }
}
```

To use a nested selector to a table thats related to the current table though another table (join tables) you should use the name of the final table and GraphJin will figure out how to connect the two. If you want to enforce the middle table use the directive `@through(table: "name")` directive.

[Directives](cheatsheet#using-query-directives) look like this `@directiveName(argument: value)` and are added to selectors or fields.

### Fetch from various related tables

GraphJin builds a weighted graph of all your database relationships and can figure out on its own how to write the most efficient query to fetch the data your need so use as many nested (related) tables in your query as you need GraphJin will figure out the rest.

![AltText {priority}{768x432}](/images/tables-graph.png)

> Fetch the lastest 10 products and their owners

![AltText {priority}{768x432}](/images/related-tables-1.png)

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

> Fetch a user and his purchases. GraphJin will auto join with user_purchase join-table. You don't have to do anything it will figure out the relationship on its own

![AltText {priority}{768x432}](/images/related-tables-2.png)

```graphql
query {
  user(id: 1) {
    id
    fullName
    purchases: products {
      id
      name
    }
  }
}
```

> You can add a many nested tables as you need. As long as they use foreign keys the overall query should be pretty fast. Tables joins are very fast and fine to use in almost all cases.

```graphql
query {
  user(id: 1) {
    id
    fullName
    purchases: products {
      id
      name
      tags {
        id
        name
      }
      images {
        id
        link
      }
    }
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

### Use Fragments

Fragments are a great way to keep common fields together and use them in various queries. You can nest fragments inside fragments if you like just remember to use the `#import` statement at the top of the file.

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

### Cursor Pagination

The ability to fetch the next batch of results is key to building any efficient app. This is needed if your building an infinite scroll or a fetch next page feature. This is hard to do across all your APIs and even harder to do this efficiently. But for us this is a breeze.

<mark>
GraphJin returns an opaque cursor (it's encrypted) to get the next batch of results all you have to do is pass that cursor back with the next API call
</mark>

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

### Sorting the query result

> Need to return the top 10 latest products with the highest costing product on top.

```graphql
query getLatestProducts {
  products(order_by: [created_at: desc, price: desc]) {
    id
    name
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

### Rename columns

> We want to rename fields in the resulting json. Lets call product `beer` and change the `id` and `name` columns to `sku` and `heading`

```graphql
query getProduct {
  beer: products(id: $product_id) {
    sku: id
    heading: name
    description
  }
}
```

```json title="Query Variables"
{
  "product_id" 2,
}
```

```json title="Result"
"data": {
   "beer": {
      "sku": 123,
      "heading": "Pale Ale"
      "description": "Something delicious to drink"
    },
}
```

### Skip or Include columns and tables

> Fetch products by their id but allow us to control if we want the product id and owner returned as well.

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
    id @include(ifVar: $include_id)
    name
    description
    owner @skip(ifVar: $dont_include_owner) {
      id
      email
    }
  }
}
```

> Skip or include based on the current users role `@skip(ifRole: "user")` or `@include(ifRole: "anon")`. Skipping sets the field to `null` but if you want to entirely drop the field then for example say you want to pick one of two similiar fields by the users role then you can use the `@add` and `@remove` directives.

```graphql title="Using @skip and @include"
query getProductsWithSpecificOwners {
  products(id: $product_id) {
    id @skip(ifRole: "anon")
    name
    description
    owner @include(ifRole: "user") {
      id
      email
    }
  }
}
```

```graphql title="Using @add and @remove"
query getProductsWithSpecificOwners {
  currentUser: users(id: $user_id) @add(ifRole: "user") {
    id
    name
    email
  }
  someUsers: users(limit: 3) @remove(ifRole: "user") {
    id
    name
  }
}
```

> You can also use full filter expressions (eg. `{ id: { in: [1,2,3] } }` ) to define when to skip or include a column or a table. For this you must use the `includeIf` or `skipIf` field arguments.

> In this below example we hide users who's `keep_private` column is set to true.

```graphql
query getProductsWithSpecificOwners {
  products(id: $product_id) {
    id(includeIf: { user_id: { eq: $user_id } })
    name
    description
    owner(skipIf: { keep_private: { eq: true } }) {
      id
      email
    }
  }
}
```

### User roles

By default we support two roles `user` for authenticated users (eg. `$user_id` is set) and `anon` for anonymous users or users who are not authenticated. This is called [Role based access control](cheatsheet#roles-for-access-control) and you can follow the link to learn more.

> I want to use the same query for both roles (user and anon) so I need to to hide and show tables and columns based on the users role.

```graphql title="@add or @remove directive"
@add(ifRole: "user")
@remove(ifRole: "user")
```

```graphql
query {
  products(limit: 2, order_by: { id: asc }) @add(ifRole: "user") {
    id
    name
  }
  users(limit: 3, order_by: { id: asc }) @remove(ifRole: "user") {
    id
  }
}
```

```json title="Result"
{
  "products": [{ "id": 1, "name": "Product 1" }]
}
```

### Use camel-case names

> If you rather use camel case for my queries instead of the snake case that my database tables and columns use. GraphJin will auto translate between the two.

```yaml title="Config File dev.yml"
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
