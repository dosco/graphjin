---
id: graphql
title: How to GraphQL
sidebar_label: GraphQL Syntax
---

GraphQL (GQL) is a simple query syntax that's fast replacing REST APIs. GQL is great since it allows web developers to fetch the exact data that they need without depending on changes to backend code. Also if you squint hard enough it looks a little bit like JSON :smiley:

The below query will fetch an `users` name, email and avatar image (renamed as picture). If you also need the users `id` then just add it to the query.

```graphql
query {
  user {
    full_name
    email
    picture: avatar
  }
}
```

Multiple tables can also be fetched using a single GraphQL query. This is very fast since the entire query is converted into a single SQL query which the database can efficiently run.

```graphql
query {
  user {
    full_name
    email
  }
  products {
    name
    description
  }
}
```

### Fetching data

To fetch a specific `product` by it's ID you can use the `id` argument. The real name id field will be resolved automatically so this query will work even if your id column is named something like `product_id`.

```graphql
query {
  products(id: 3) {
    name
  }
}
```

Postgres also supports full text search using a TSV index. Super Graph makes it easy to use this full text search capability using the `search` argument.

```graphql
query {
  products(search: "ale") {
    name
  }
}
```

### Fragments

Fragments make it easy to build large complex queries with small composible and re-usable fragment blocks.

```graphql
query {
  users {
    ...userFields2
    ...userFields1
    picture_url
  }
}

fragment userFields1 on user {
  id
  email
}

fragment userFields2 on user {
  first_name
  last_name
}
```

### Sorting

To sort or ordering results just use the `order_by` argument. This can be combined with `where`, `search`, etc to build complex queries to fit your needs.

```graphql
query {
  products(order_by: { cached_votes_total: desc }) {
    id
    name
  }
}
```

### Filtering

Super Graph supports complex queries where you can add filters, ordering, offsets and limits on the query. For example the below query will list all products where the price is greater than 10 and the id is not 5.

```graphql
query {
  products(where: { and: { price: { gt: 10 }, not: { id: { eq: 5 } } } }) {
    name
    price
  }
}
```

#### Nested where clause targeting related tables

Sometimes you need to query a table based on a condition that applies to a related table. For example say you need to list all users who belong to an account. This query below will fetch the id and email or all users who belong to the account with id 3.

```graphql
query {
  users(where: {
      accounts: { id: { eq: 3 } }
    }) {
    id
    email
  }
}`
```

#### Logical Operators

| Name | Example                                                      | Explained                       |
| ---- | ------------------------------------------------------------ | ------------------------------- |
| and  | price : { and : { gt: 10.5, lt: 20 }                         | price > 10.5 AND price < 20     |
| or   | or : { price : { greater_than : 20 }, quantity: { gt : 0 } } | price >= 20 OR quantity > 0     |
| not  | not: { or : { quantity : { eq: 0 }, price : { eq: 0 } } }    | NOT (quantity = 0 OR price = 0) |

#### Other conditions

| Name                   | Example                                | Explained                                                                                                |
| ---------------------- | -------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| eq, equals             | id : { eq: 100 }                       | id = 100                                                                                                 |
| neq, not_equals        | id: { not_equals: 100 }                | id != 100                                                                                                |
| gt, greater_than       | id: { gt: 100 }                        | id > 100                                                                                                 |
| lt, lesser_than        | id: { gt: 100 }                        | id < 100                                                                                                 |
| gte, greater_or_equals | id: { gte: 100 }                       | id >= 100                                                                                                |
| lte, lesser_or_equals  | id: { lesser_or_equals: 100 }          | id <= 100                                                                                                |
| in                     | status: { in: [ "A", "B", "C" ] }      | status IN ('A', 'B', 'C')                                                                                 |
| nin, not_in            | status: { in: [ "A", "B", "C" ] }      | status IN ('A', 'B', 'C')                                                                                 |
| like                   | name: { like "phil%" }                 | Names starting with 'phil'                                                                               |
| nlike, not_like        | name: { nlike "v%m" }                  | Not names starting with 'v' and ending with 'm'                                                          |
| ilike                  | name: { ilike "%wOn" }                 | Names ending with 'won' case-insensitive                                                                 |
| nilike, not_ilike      | name: { nilike "%wOn" }                | Not names ending with 'won' case-insensitive                                                             |
| similar                | name: { similar: "%(b\|d)%" }          | [Similar Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-SIMILARTO-REGEXP)     |
| nsimilar, not_similar  | name: { nsimilar: "%(b\|d)%" }         | [Not Similar Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-SIMILARTO-REGEXP) |
| regex                  | name: { regex: "^([a-zA-Z]+)$" }       | [Regex Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-POSIX-TABLE)            |
| nregex, not_regex      | name: { nregex: "^([a-zA-Z]+)$" }      | [Regex Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-POSIX-TABLE)            |
| iregex                 | name: { iregex: "^([a-z]+)$" }         | [Regex Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-POSIX-TABLE)            |
| niregex, not_iregex    | name: { not_iregex: "^([a-z]+)$" }     | [Regex Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-POSIX-TABLE)            |
| has_key                | column: { has_key: 'b' }               | Does JSON column contain this key                                                                        |
| has_key_any            | column: { has_key_any: [ a, b ] }      | Does JSON column contain any of these keys                                                               |
| has_key_all            | column: [ a, b ]                       | Does JSON column contain all of this keys                                                                |
| contains               | column: { contains: [1, 2, 4] }        | Is this array/json column a subset of value                                                              |
| contained_in           | column: { contains: "{'a':1, 'b':2}" } | Is this array/json column a subset of these value                                                        |
| is_null                | column: { is_null: true }              | Is column value null or not                                                                              |

### Aggregations

You will often find the need to fetch aggregated values from the database such as `count`, `max`, `min`, etc. This is simple to do with GraphQL, just prefix the aggregation name to the field name that you want to aggregrate like `count_id`. The below query will group products by name and find the minimum price for each group. Notice the `min_price` field we're adding `min_` to price.

```graphql
query {
  products {
    name
    min_price
  }
}
```

| Name        | Explained                                                              |
| ----------- | ---------------------------------------------------------------------- |
| avg         | Average value                                                          |
| count       | Count the values                                                       |
| max         | Maximum value                                                          |
| min         | Minimum value                                                          |
| stddev      | [Standard Deviation](https://en.wikipedia.org/wiki/Standard_deviation) |
| stddev_pop  | Population Standard Deviation                                          |
| stddev_samp | Sample Standard Deviation                                              |
| variance    | [Variance](https://en.wikipedia.org/wiki/Variance)                     |
| var_pop     | Population Standard Variance                                           |
| var_samp    | Sample Standard variance                                               |

All kinds of queries are possible with GraphQL. Below is an example that uses a lot of the features available. Comments `# hello` are also valid within queries.

```graphql
query {
  products(
    # returns only 30 items
    limit: 30

    # starts from item 10, commented out for now
    # offset: 10,

    # orders the response items by highest price
    order_by: { price: desc }

    # no duplicate prices returned
    distinct: [price]

    # only items with an id >= 30 and < 30 are returned
    where: { id: { and: { greater_or_equals: 20, lt: 28 } } }
  ) {
    id
    name
    price
  }
}
```

### Directives

| Directive    | Arguments | Description                                                     |
|--------------|-----------|-----------------------------------------------------------------|
| @skip        | if: $var  | Skip this query selector when the `if` variable is true         |
| @include     | if: $var  | Include this query selector only when the `if` variable is true |
| @not_related |           | Tells the compiler to not relate this selector to its parent    |
| @through     | table: "" | Tells the compiler which join table it should use for selector  |


`@through(table: "name")` is to be used when there are multiple join tables that create a path between a child and parent in a nested query, this directive will tell the SQL compiler which of the through tables (join tables) to use for this relationship.

```

query {
  user(id: 5) {
    tags(where: { slug: { eq: $slug } }) @not_related {
      id
    }
    products @through(table: "purchases") {
      id
    }
  }
}
```

:::info
When super graph starts it builds an internal graph of all the related tables. Sometimes tables are not directly connected thought a foreign key but are connected two stops away though another table which people referr to as a join table. In this example if user and product had two seperate join tables maybe one for  purchased products and another for products you uploaded then you can use `@though` to specify which one to use to connect the tables together
:::

### Recursive Queries

A common pattern one encouters if recursive relationships which is when a table references itself. A good example of this is threaded comments when a comment is a reply to a previous comment and that to a previous. Another example could be an employee table where an employee references another employee (his boss). This has previously been hard to work with but Super Graph makes it a breeze. This works with any table that has a foreign key relationship to itself. The `find` parameter controls the direction of the fetch which can either be `parents` or `children`.

#### Given a comment fetch it's parent comments

```graphql
query {
  reply : comment(id: $id) {
    id
    comments(find: "parents") {
      id
      body
    }
  }
}
```

#### Given an employee find his whole team.

```graphql
query {
  employee(id: $id) {
    id
    teamMembers: employees(find: "children") {
      id
      name
    }
  }
}
```




### Custom Functions

Any function defined in the database like the below `add_five` that adds 5 to any number given to it can be used
within your query. The one limitation is that it should be a function that only accepts a single argument. The function is used within you're GraphQL in similar way to how aggregrations are used above. Example below

```grahql
query {
  thread(id: 5) {
    id
    total_votes
    add_five_total_votes
  }
}
```

Postgres user-defined function `add_five`

```
CREATE OR REPLACE FUNCTION add_five(a integer) RETURNS integer AS $$
BEGIN

    RETURN a + 5;
END;
$$ LANGUAGE plpgsql;
```

In GraphQL mutations is the operation type for when you need to modify data. Super Graph supports the `insert`, `update`, `upsert` and `delete`. You can also do complex nested inserts and updates.

When using mutations the data must be passed as variables since Super Graphs compiles the query into an prepared statement in the database for maximum speed. Prepared statements are functions in your code that when called accept arguments and your variables are passed in as those arguments.

### Insert

```json
{
  "data": {
    "name": "Art of Computer Programming",
    "description": "The Art of Computer Programming (TAOCP) is a comprehensive monograph written by computer scientist Donald Knuth",
    "price": 30.5
  }
}
```

```graphql
mutation {
  product(insert: $data) {
    id
    name
  }
}
```

#### Bulk insert

```json
{
  "data": [
    {
      "name": "Art of Computer Programming",
      "description": "The Art of Computer Programming (TAOCP) is a comprehensive monograph written by computer scientist Donald Knuth",
      "price": 30.5
    },
    {
      "name": "Compilers: Principles, Techniques, and Tools",
      "description": "Known to professors, students, and developers worldwide as the 'Dragon Book' is available in a new edition",
      "price": 93.74
    }
  ]
}
```

```graphql
mutation {
  product(insert: $data) {
    id
    name
  }
}
```

### Update

```json
{
  "data": {
    "price": 200.0
  },
  "product_id": 5
}
```

```graphql
mutation {
  product(update: $data, id: $product_id) {
    id
    name
  }
}
```

#### Bulk update

```json
{
  "data": {
    "price": 500.0
  },
  "gt_product_id": 450.0,
  "lt_product_id:": 550.0
}
```

```graphql
mutation {
  product(
    update: $data
    where: { price: { gt: $gt_product_id, lt: lt_product_id } }
  ) {
    id
    name
  }
}
```

### Delete

```json
{
  "data": {
    "price": 500.0
  },
  "product_id": 5
}
```

```graphql
mutation {
  product(delete: true, id: $product_id) {
    id
    name
  }
}
```

#### Bulk delete

```json
{
  "data": {
    "price": 500.0
  }
}
```

```graphql
mutation {
  product(delete: true, where: { price: { eq: { 500.0 } } }) {
    id
    name
  }
}
```

### Upsert

```json
{
  "data": {
    "id": 5,
    "name": "Art of Computer Programming",
    "description": "The Art of Computer Programming (TAOCP) is a comprehensive monograph written by computer scientist Donald Knuth",
    "price": 30.5
  }
}
```

```graphql
mutation {
  product(upsert: $data) {
    id
    name
  }
}
```

#### Bulk upsert

```json
{
  "data": [
    {
      "id": 5,
      "name": "Art of Computer Programming",
      "description": "The Art of Computer Programming (TAOCP) is a comprehensive monograph written by computer scientist Donald Knuth",
      "price": 30.5
    },
    {
      "id": 6,
      "name": "Compilers: Principles, Techniques, and Tools",
      "description": "Known to professors, students, and developers worldwide as the 'Dragon Book' is available in a new edition",
      "price": 93.74
    }
  ]
}
```

```graphql
mutation {
  product(upsert: $data) {
    id
    name
  }
}
```

Often you will need to create or update multiple related items at the same time. This can be done using nested mutations. For example you might need to create a product and assign it to a user, or create a user and his products at the same time. You just have to use simple json to define you mutation and Super Graph takes care of the rest.

### Nested Insert

Create a product item first and then assign it to a user

```json
{
  "data": {
    "name": "Apple",
    "price": 1.25,
    "created_at": "now",
    "updated_at": "now",
    "user": {
      "connect": { "id": 5 }
    }
  }
}
```

```graphql
mutation {
  product(insert: $data) {
    id
    name
    user {
      id
      full_name
      email
    }
  }
}
```

Or it's reverse, create the user first and then his product

```json
{
  "data": {
    "email": "thedude@rug.com",
    "full_name": "The Dude",
    "created_at": "now",
    "updated_at": "now",
    "product": {
      "name": "Apple",
      "price": 1.25,
      "created_at": "now",
      "updated_at": "now"
    }
  }
}
```

```graphql
mutation {
  user(insert: $data) {
    id
    full_name
    email
    product {
      id
      name
      price
    }
  }
}
```

### Nested Update

Update a product item first and then assign it to a user

```json
{
  "data": {
    "name": "Apple",
    "price": 1.25,
    "user": {
      "connect": { "id": 5 }
    }
  }
}
```

```graphql
mutation {
  product(update: $data, id: 5) {
    id
    name
    user {
      id
      full_name
      email
    }
  }
}
```

Or it's reverse, update a user first and then his product

```json
{
  "data": {
    "email": "newemail@me.com",
    "full_name": "The Dude",
    "product": {
      "name": "Banana",
      "price": 1.25
    }
  }
}
```

```graphql
mutation {
  user(update: $data, id: 1) {
    id
    full_name
    email
    product {
      id
      name
      price
    }
  }
}
```

### Pagination

This is a must have feature of any API. When you want your users to go through a list page by page or implement some fancy infinite scroll you're going to need pagination. There are two ways to paginate in Super Graph.

#### Limit-Offset
This is simple enough but also inefficient when working with a large number of total items. Limit, limits the number of items fetched and offset is the point you want to fetch from. The below query will fetch 10 results at a time starting with the 100th item. You will have to keep updating offset (110, 120, 130, etc ) to walk thought the results so make offset a variable.

```graphql
query {
  products(limit: 10, offset: 100) {
    id
    slug
    name
  }
}
```

#### Cursor

This is a powerful and highly efficient way to paginate a large number of results. Infact it does not matter how many total results there are this will always be lighting fast. You can use a cursor to walk forward or backward through the results. If you plan to implement infinite scroll this is the option you should choose.

When going this route the results will contain a cursor value this is an encrypted string that you don't have to worry about just pass this back in to the next API call and you'll received the next set of results. The cursor value is encrypted since its contents should only matter to Super Graph and not the client. Also since the primary key is used for this feature it's possible you might not want to leak it's value to clients.

You will need to set this config value to ensure the encrypted cursor data is secure. If not set a random value is used which will change with each deployment breaking older cursor values that clients might be using so best to set it.

```yaml
# Secret key for general encryption operations like
# encrypting the cursor data
secret_key: supercalifajalistics
```

Paginating forward through your results

```json
{
  "variables": {
    "cursor": "MJoTLbQF4l0GuoDsYmCrpjPeaaIlNpfm4uFU4PQ="
  }
}
```

```graphql
query {
  products(first: 10, after: $cursor) {
    slug
    name
  }
}
```

Paginating backward through your results

```graphql
query {
  products(last: 10, before: $cursor) {
    slug
    name
  }
}
```

```graphql
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
  "products_cursor": "dJwHassm5+d82rGydH2xQnwNxJ1dcj4/cxkh5Cer"
}
```

Nested tables can also have cursors. Requesting multiple cursors are supported on a single request but when paginating using a cursor only one table is currently supported. To explain this better, you can only use a `before` or `after` argument with a cursor value to paginate a single table in a query.

```graphql
query {
  products(last: 10) {
    slug
    name
    customers(last: 5) {
      email
      full_name
    }
  }
}
```

Multiple order-by arguments are supported. Super Graph is smart enough to allow cursor based pagination when you also need complex sort order like below.

```graphql
query {
  products(
    last: 10
    before: $cursor
    order_by: [ price: desc, total_customers: asc ]) {
    slug
    name
  }
}
```

## Using Variables

Variables (`$product_id`) and their values (`"product_id": 5`) can be passed along side the GraphQL query. Using variables makes for better client side code as well as improved server side SQL query caching. The built-in web-ui also supports setting variables. Not having to manipulate your GraphQL query string to insert values into it makes for cleaner
and better client side code.

```javascript
// Define the request object keeping the query and the variables seperate
var req = {
  query: "{ product(id: $product_id) { name } }",
  variables: { product_id: 5 },
};

// Use the fetch api to make the query
fetch("http://localhost:8080/api/v1/graphql", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(req),
})
  .then((res) => res.json())
  .then((res) => console.log(res.data));
```
