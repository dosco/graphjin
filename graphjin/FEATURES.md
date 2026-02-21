# GraphJin Features - Complete Reference

GraphJin is a high-performance GraphQL to SQL compiler that automatically generates optimized database queries from GraphQL. This document covers all 50+ features with real examples.

## Table of Contents

- [The Magic of GraphJin](#the-magic-of-graphjin)
- [Query Capabilities](#query-capabilities)
  - [Basic Queries](#basic-queries)
  - [Filtering & WHERE Clauses](#filtering--where-clauses)
  - [Ordering & Pagination](#ordering--pagination)
  - [Relationship Queries](#relationship-queries)
  - [Recursive Queries](#recursive-queries)
  - [Aggregations](#aggregations)
  - [Full-Text Search](#full-text-search)
  - [JSON Operations](#json-operations)
  - [GraphQL Fragments](#graphql-fragments)
  - [Polymorphic Relationships](#polymorphic-relationships)
  - [Directives](#directives)
  - [Remote API Joins](#remote-api-joins)
  - [Database Functions](#database-functions)
- [Mutation Capabilities](#mutation-capabilities)
  - [Simple Inserts](#simple-inserts)
  - [Bulk Inserts](#bulk-inserts)
  - [Nested Inserts](#nested-inserts)
  - [Connect & Disconnect](#connect--disconnect)
  - [Validation](#validation)
  - [Updates](#updates)
- [Real-time Subscriptions](#real-time-subscriptions)
- [Security Features](#security-features)
  - [Role-Based Access Control](#role-based-access-control)
  - [Row-Level Security](#row-level-security)
  - [Column Blocking](#column-blocking)
  - [Read-Only Databases](#read-only-databases)
  - [Query Allow Lists](#query-allow-lists)
- [Advanced Features](#advanced-features)
  - [Synthetic Tables](#synthetic-tables)
  - [Views Support](#views-support)
  - [Multi-Schema Support](#multi-schema-support)
  - [Transaction Support](#transaction-support)
  - [CamelCase Conversion](#camelcase-conversion)
- [Multi-Database Support](#multi-database-support)
- [Configuration Reference](#configuration-reference)

---

## The Magic of GraphJin

GraphJin eliminates weeks of backend API development by automatically converting GraphQL queries into highly optimized SQL. Here's what makes it magical:

### Zero-Code API Generation

Write a GraphQL query, and GraphJin automatically:
- Discovers your database schema and relationships
- Generates optimized SQL with proper JOINs
- Returns nested JSON exactly as requested
- Handles pagination, filtering, and ordering

```graphql
query {
  products(limit: 3, order_by: { id: asc }) {
    id
    name
    owner {
      id
      fullName: full_name
    }
  }
}
```

This single query automatically generates optimized SQL that fetches products with their owners in **one database query** - no N+1 problem.

### Single Optimized SQL Query

Complex nested queries compile to a single SQL statement using LATERAL JOINs:

```graphql
query getProducts {
  products(limit: 20, order_by: { price: desc }) {
    id
    name
    price
    owner {
      full_name
      email
      category_counts(limit: 3) {
        count
        category { name }
      }
    }
    category(limit: 3) { id, name }
  }
  products_cursor
}
```

### Production Security

In production mode, queries are read from locally saved copies - clients cannot modify queries at runtime. This provides security equivalent to hand-written APIs.

---

## Query Capabilities

### Basic Queries

Simple field selection with aliases:

```graphql
query {
  products(limit: 3, order_by: { id: asc }) {
    id
    count_likes
    owner {
      id
      fullName: full_name  # Field alias
    }
  }
}
```

Query by ID returns a single object:

```graphql
query {
  products(id: $id) {
    id
    name
  }
}
# Variables: { "id": 2 }
# Returns: {"products":{"id":2,"name":"Product 2"}}
```

### Filtering & WHERE Clauses

GraphJin supports 15+ filter operators:

| Operator | Description | Example |
|----------|-------------|---------|
| `eq` | Equals | `{ id: { eq: 1 } }` |
| `neq` | Not equals | `{ id: { neq: 1 } }` |
| `gt` | Greater than | `{ price: { gt: 10 } }` |
| `gte`, `greater_or_equals` | Greater or equal | `{ price: { gte: 10 } }` |
| `lt` | Less than | `{ price: { lt: 100 } }` |
| `lte`, `lesser_or_equals` | Less or equal | `{ price: { lte: 100 } }` |
| `in` | In list | `{ id: { in: [1,2,3] } }` |
| `nin` | Not in list | `{ id: { nin: [1,2] } }` |
| `is_null` | Is null | `{ id: { is_null: true } }` |
| `iregex` | Case-insensitive regex | `{ name: { iregex: "product" } }` |
| `has_key` | JSON has key | `{ metadata: { has_key: "foo" } }` |
| `has_key_any` | JSON has any key | `{ metadata: { has_key_any: ["foo","bar"] } }` |

**Logical operators** - `and`, `or`, `not`:

```graphql
query {
  products(where: {
    and: [
      { not: { id: { is_null: true } } },
      { price: { gt: 10 } }
    ]
  }, limit: 3) {
    id
    name
    price
  }
}
```

**Filter on related tables**:

```graphql
query {
  products(where: { owner: { id: { eq: $user_id } } }) {
    id
    owner { id, email }
  }
}
```

**Regex matching**:

```graphql
query {
  products(where: {
    or: {
      name: { iregex: $name },
      description: { iregex: $name }
    }
  }) {
    id
  }
}
```

### Ordering & Pagination

**Basic ordering**:

```graphql
query {
  products(order_by: { price: desc }, limit: 5) {
    id
    name
    price
  }
}
```

**Distinct values**:

```graphql
query {
  products(
    limit: 5,
    order_by: { price: desc },
    distinct: [price],
    where: { id: { gte: 50, lt: 100 } }
  ) {
    id
    name
    price
  }
}
```

**Nested ordering** (order by related table):

```graphql
query {
  products(order_by: { users: { email: desc }, id: desc }, limit: 5) {
    id
    price
  }
}
```

**Order by custom list**:

```graphql
query {
  products(
    order_by: { id: [$list, "asc"] },
    where: { id: { in: $list } }
  ) {
    id
    price
  }
}
# Variables: { "list": [3, 2, 1, 5] }
# Returns products in order: 3, 2, 1, 5
```

**Cursor-based pagination** (efficient infinite scroll):

```graphql
query {
  products(
    first: 3,
    after: $cursor,
    order_by: { price: desc }
  ) {
    name
  }
  products_cursor  # Encrypted cursor for next page
}
```

**Dynamic order_by** (configurable ordering):

```go
conf.Tables = []core.Table{{
    Name: "products",
    OrderBy: map[string][]string{
        "price_and_id": {"price desc", "id asc"},
        "just_id":      {"id asc"},
    },
}}
```

```graphql
query {
  products(order_by: $order, limit: 5) {
    id
    price
  }
}
# Variables: { "order": "price_and_id" }
```

### Relationship Queries

**Parent to children** (one-to-many):

```graphql
query {
  users(limit: 2) {
    email
    products {  # User's products
      name
      price
    }
  }
}
```

**Children to parent** (many-to-one):

```graphql
query {
  products(limit: 2) {
    name
    owner {  # Product's owner
      email
    }
  }
}
```

**Many-to-many via join table**:

```graphql
query {
  products(limit: 2) {
    name
    customer {  # Customers who purchased (via purchases table)
      email
    }
    owner {
      email
    }
  }
}
```

**Multiple top-level tables**:

```graphql
query {
  products(id: $id) {
    id
    name
  }
  users(id: $id) {
    id
    email
  }
  purchases(id: $id) {
    id
  }
}
```

### Recursive Queries

Query self-referential data structures like comment trees:

**Find all parents** (ancestors):

```graphql
query {
  comments(id: 50) {
    id
    comments(find: "parents", limit: 5) {
      id
    }
  }
}
# Returns: comment 50 with its parent chain
```

**Find all children** (descendants):

```graphql
query {
  comments(id: 95) {
    id
    replies: comments(find: "children") {
      id
    }
  }
}
# Returns: {"comments":{"id":95,"replies":[{"id":96},{"id":97},{"id":98},{"id":99},{"id":100}]}}
```

**Aggregations on recursive results**:

```graphql
query {
  comments(id: 95) {
    id
    replies: comments(find: "children") {
      count_id  # Count all children
    }
  }
}
```

### Aggregations

Built-in aggregate functions:

| Function | Example |
|----------|---------|
| `count_<column>` | `count_id` |
| `sum_<column>` | `sum_price` |
| `max_<column>` | `max_price` |
| `min_<column>` | `min_price` |
| `avg_<column>` | `avg_price` |

```graphql
query {
  products(where: { id: { lteq: 100 } }) {
    count_id
    max_price
  }
}
# Returns: {"products":[{"count_id":100,"max_price":110.5}]}
```

### Full-Text Search

```graphql
query {
  products(search: "Product 3", limit: 5) {
    id
    name
  }
}
```

Supports PostgreSQL `tsvector`, MySQL `FULLTEXT`, and SQLite `FTS5`.

### JSON Operations

**Filter on JSON fields**:

```graphql
query {
  quotations(where: {
    validity_period: {
      issue_date: { lte: "2024-09-18T03:03:16+0000" }
    }
  }) {
    id
    validity_period
  }
}
```

**Underscore syntax for JSON paths**:

```graphql
query {
  products(where: { metadata_foo: { eq: true } }) {
    id
    metadata
  }
}
# Filters where metadata->foo = true
```

**Check for JSON keys**:

```graphql
query {
  products(where: { metadata: { has_key_any: ["foo", "bar"] } }) {
    id
  }
}
```

**JSON column as virtual table**:

```go
conf.Tables = []core.Table{{
    Name:  "category_counts",
    Table: "users",
    Type:  "json",
    Columns: []core.Column{
        {Name: "category_id", Type: "int", ForeignKey: "categories.id"},
        {Name: "count", Type: "int"},
    },
}}
```

```graphql
query {
  users(id: 1) {
    id
    category_counts {
      count
      category { name }
    }
  }
}
```

### GraphQL Fragments

Reuse field selections across queries:

```graphql
fragment productFields on product {
  id
  name
  price
}

fragment ownerFields on user {
  id
  email
}

query {
  products(limit: 2) {
    ...productFields
    owner {
      ...ownerFields
    }
  }
}
```

### Polymorphic Relationships

Query union types for polymorphic associations:

```go
conf.Tables = []core.Table{{
    Name:    "subject",
    Type:    "polymorphic",
    Columns: []core.Column{{Name: "subject_id", ForeignKey: "subject_type.id"}},
}}
```

```graphql
query {
  notifications {
    id
    verb
    subject {
      ...on users { email }
      ...on products { name }
    }
  }
}
# Returns: {"notifications":[
#   {"id":1,"subject":{"email":"user1@test.com"},"verb":"Joined"},
#   {"id":2,"subject":{"name":"Product 2"},"verb":"Bought"}
# ]}
```

### Directives

**Role-based inclusion/exclusion**:

```graphql
query {
  products @include(ifRole: "user") {
    id
    name
  }
  users @skip(ifRole: "user") {
    id
  }
}
```

**Variable-based inclusion/exclusion**:

```graphql
query {
  products @include(ifVar: $showProducts) {
    id
  }
}
# Variables: { "showProducts": true }
```

**Field-level directives**:

```graphql
query {
  products {
    id @skip(ifRole: "user")
    name @include(ifRole: "user")
  }
}
```

**Add/Remove directives** (exclude from response entirely):

```graphql
query {
  products @add(ifRole: "user") {  # Only added if user role
    id
  }
  users @remove(ifRole: "user") {  # Removed if user role
    id
  }
}
```

**Conditional field values**:

```graphql
query {
  products {
    id(includeIf: { id: { eq: 1 } })  # null if id != 1
    name
  }
}
```

**@object directive** (force single object response):

```graphql
query {
  me @object {
    email
  }
}
# Returns: {"me":{"email":"..."}} instead of {"me":[{...}]}
```

### Remote API Joins

Combine database data with external REST APIs:

```go
conf.Resolvers = []core.ResolverConfig{{
    Name:      "payments",
    Type:      "remote_api",
    Table:     "users",
    Column:    "stripe_id",
    StripPath: "data",
    Props:     core.ResolverProps{"url": "http://api.stripe.com/payments/$id"},
}}
```

```graphql
query {
  users {
    email
    payments {  # Fetched from Stripe API
      desc
      amount
    }
  }
}
```

### Database Functions

**Scalar functions as fields**:

```graphql
query {
  products(id: 51) {
    id
    name
    is_hot_product(args: { id: id })  # Calls database function
  }
}
```

**Table-returning functions**:

```graphql
query {
  get_oldest5_products(limit: 3) {
    id
    name
  }
}
```

**Functions with named arguments**:

```graphql
query {
  get_oldest_users(limit: 2, args: { user_count: 4, tag: $tag }) {
    id
    full_name
  }
}
```

**Functions with positional arguments**:

```graphql
query {
  get_oldest_users(args: { a0: 4, a1: "tag_value" }) {
    id
  }
}
```

---

## Mutation Capabilities

### Simple Inserts

```graphql
mutation {
  users(insert: {
    id: $id,
    email: $email,
    full_name: $fullName
  }) {
    id
    email
  }
}
```

### Bulk Inserts

**Array variable**:

```graphql
mutation {
  users(insert: $data) {
    id
    email
  }
}
# Variables: { "data": [
#   { "id": 1002, "email": "user1@test.com" },
#   { "id": 1003, "email": "user2@test.com" }
# ]}
```

**Inline array**:

```graphql
mutation {
  users(insert: [
    {id: $id1, email: $email1},
    {id: $id2, email: $email2}
  ]) {
    id
    email
  }
}
```

### Nested Inserts

Insert across multiple related tables atomically:

```graphql
mutation {
  purchases(insert: $data) {
    quantity
    customer {
      id
      full_name
    }
    product {
      id
      name
      price
    }
  }
}
```

```json
{
  "data": {
    "id": 3001,
    "quantity": 5,
    "customer": {
      "id": 1004,
      "email": "new@customer.com",
      "full_name": "New Customer"
    },
    "product": {
      "id": 2002,
      "name": "New Product",
      "price": 99.99,
      "owner_id": 3
    }
  }
}
```

All inserts happen in a single transaction - if any fails, all roll back.

**Presets** (auto-fill fields):

```go
conf.AddRoleTable("user", "products", core.Insert{
    Presets: map[string]string{"owner_id": "$user_id"},
})
```

```graphql
mutation {
  products(insert: { name: "Product", price: 10 }) {
    id
    owner { id }  # Automatically set to current user
  }
}
```

### Connect & Disconnect

Link to existing records instead of creating new ones:

**Connect on insert**:

```graphql
mutation {
  products(insert: {
    name: "New Product",
    owner: { connect: { id: 6 } }  # Link to existing user
  }) {
    id
    owner { email }
  }
}
```

**Recursive connect**:

```graphql
mutation {
  comments(insert: {
    body: "Parent comment",
    comments: {
      find: "children",
      connect: { id: 5 }  # Make comment 5 a child
    }
  }) {
    id
  }
}
```

### Validation

Use `@constraint` directive for input validation:

```graphql
mutation
  @constraint(variable: "email", format: "email", min: 1, max: 100)
  @constraint(variable: "full_name", requiredIf: { id: 1007 })
  @constraint(variable: "id", greaterThan: 1006) {
  users(insert: { id: $id, email: $email }) {
    id
  }
}
```

**Available constraints**:

| Constraint | Description |
|------------|-------------|
| `format` | `"email"`, custom regex |
| `min` | Minimum length |
| `max` | Maximum length |
| `required` | Field is required |
| `requiredIf` | Required if condition matches |
| `greaterThan` | Numeric comparison |
| `lessThan` | Numeric comparison |
| `equals` | Exact match |
| `lessThanOrEqualsField` | Compare to another field |

### Updates

**Simple update**:

```graphql
mutation {
  products(id: $id, update: { name: "Updated Name" }) {
    id
    name
  }
}
```

**Update with WHERE**:

```graphql
mutation {
  products(where: { id: 100 }, update: { tags: ["new", "tags"] }) {
    id
    tags
  }
}
```

**Update multiple related tables**:

```graphql
mutation {
  purchases(id: $id, update: {
    quantity: 6,
    customer: { full_name: "Updated Customer" },
    product: { description: "Updated Description" }
  }) {
    quantity
    customer { full_name }
    product { description }
  }
}
```

**Connect and disconnect on update**:

```graphql
mutation {
  users(id: $id, update: {
    products: {
      connect: { id: 99 },
      disconnect: { id: 100 }
    }
  }) {
    products { id }
  }
}
```

---

## Real-time Subscriptions

Subscribe to data changes with automatic polling:

```graphql
subscription {
  users(id: $id) {
    id
    email
    phone
  }
}
```

```go
conf := &core.Config{SubsPollDuration: 1}  // Poll every second
gj, _ := core.NewGraphJin(conf, db)

m, _ := gj.Subscribe(ctx, gql, vars, nil)
for msg := range m.Result {
    fmt.Println(msg.Data)  // Triggered on every change
}
```

**Cursor-based subscriptions** (for feeds/chat):

```graphql
subscription {
  chats(first: 1, after: $cursor) {
    id
    body
  }
  chats_cursor
}
```

---

## Security Features

### Role-Based Access Control

Define roles and their permissions:

```go
// Define role detection query
conf.RolesQuery = `SELECT * FROM users WHERE id = $user_id`
conf.Roles = []core.Role{
    {Name: "admin", Match: "role = 'admin'"},
    {Name: "user", Match: "id IS NOT NULL"},
}
```

### Row-Level Security

Filter rows based on user context:

```go
conf.AddRoleTable("user", "products", core.Query{
    Filters: []string{`{ owner_id: { eq: $user_id } }`},
})
```

Now users only see their own products.

### Column Blocking

Restrict which columns a role can access:

```go
conf.AddRoleTable("anon", "users", core.Query{
    Columns: []string{"id", "name"},  // Only these columns allowed
})
```

**Block entire tables**:

```go
conf.AddRoleTable("disabled_user", "users", core.Query{Block: true})
```

**Disable functions**:

```go
conf.AddRoleTable("anon", "products", core.Query{
    DisableFunctions: true,
})
```

### Read-Only Databases

Mark a database as read-only to block all mutations (insert, update, delete) and DDL operations while still allowing queries:

```yaml
databases:
  analytics:
    type: postgres
    host: analytics-db.example.com
    dbname: analytics
    read_only: true  # All mutations blocked, queries allowed
```

Once set in config, `read_only` cannot be disabled at runtime — even by MCP tools or LLM-driven config updates. This tamper protection ensures reporting and replica databases are never accidentally modified.

### Query Allow Lists

In production mode, only pre-approved queries can run:

```go
conf := &core.Config{
    Production: true,  // Enables allow list enforcement
}
```

Queries are saved locally during development and locked in production.

---

## Advanced Features

### Synthetic Tables

Create virtual tables that map to real tables:

```go
conf.Tables = []core.Table{{Name: "me", Table: "users"}}
conf.AddRoleTable("user", "me", core.Query{
    Filters: []string{`{ id: $user_id }`},
    Limit:   1,
})
```

```graphql
query {
  me @object {
    email
  }
}
# Returns current user's data
```

### Views Support

Query database views with relationship configuration:

```go
conf.Tables = []core.Table{{
    Name: "hot_products",
    Columns: []core.Column{
        {Name: "product_id", Type: "int", ForeignKey: "products.id"},
    },
}}
```

```graphql
query {
  hot_products(limit: 3) {
    product {
      id
      name
    }
  }
}
```

### Multi-Schema Support

Query tables from different database schemas:

```graphql
query {
  test_table @schema(name: "custom_schema") {
    column1
    column2
  }
}
```

### Transaction Support

Execute queries within a transaction:

```go
tx, _ := db.BeginTx(ctx, nil)
defer tx.Rollback()

res, _ := gj.GraphQLTx(ctx, tx, query, vars, nil)
tx.Commit()
```

### CamelCase Conversion

Automatically convert between camelCase (GraphQL) and snake_case (SQL):

```go
conf := &core.Config{EnableCamelcase: true}
```

```graphql
query {
  hotProducts {  # Queries hot_products table
    countProductID  # Maps to count_product_id
    products { id }
  }
}
```

---

## Multi-Database Support

GraphJin supports 8 databases with the same GraphQL syntax:

| Database | Queries | Mutations | Subscriptions | Arrays | Full-Text |
|----------|---------|-----------|---------------|--------|-----------|
| PostgreSQL | Yes | Yes | Yes | Yes | Yes |
| MySQL | Yes | Yes | Polling | No | Yes |
| MariaDB | Yes | Yes | Polling | No | Yes |
| MSSQL | Yes | Yes | No | No | No |
| Oracle | Yes | Yes | No | No | No |
| SQLite | Yes | Yes | No | No | FTS5 |
| MongoDB | Yes | Yes | Yes | Yes | Yes |
| CockroachDB | Yes | Yes | Yes | Yes | Yes |

Also works with: **AWS Aurora/RDS**, **Google Cloud SQL**, **YugabyteDB**

### Cross-Database Joins

When tables live in different databases, GraphJin automatically handles cross-database joins. Write a normal nested query — GraphJin fetches the parent from one database, extracts the foreign key, queries the child table in the target database, and stitches the results together:

```graphql
query {
  orders {
    id
    total
    customer {   # 'customer' lives in a different database
      name
      email
    }
  }
}
```

Configure the relationship in your config:

```yaml
databases:
  main:
    type: postgres
    default: true
  crm:
    type: postgres
    tables: [customers]

tables:
  - name: orders
    columns:
      - name: customer_id
        related_to: customers.id
```

The join is transparent — no special query syntax needed. GraphJin handles ID extraction, cross-database querying, and result stitching automatically.

---

## Configuration Reference

Key configuration options:

```go
conf := &core.Config{
    // Database
    DBType: "postgres",  // postgres, mysql, mongodb, etc.

    // Security
    SecretKey:        "encryption-key",  // For cursor encryption
    DisableAllowList: false,             // Enforce allow list in production
    Production:       true,              // Production mode

    // Features
    EnableCamelcase:  true,              // camelCase to snake_case
    DefaultLimit:     20,                // Default query limit

    // Subscriptions
    SubsPollDuration: 2,                 // Seconds between polls

    // Variables
    Vars: map[string]string{
        "product_price": "50",
    },
}
```

**Table configuration**:

```go
conf.Tables = []core.Table{
    {
        Name:  "products",
        OrderBy: map[string][]string{
            "by_price": {"price desc", "id asc"},
        },
        Columns: []core.Column{
            {Name: "category_id", ForeignKey: "categories.id"},
        },
    },
}
```

**Role configuration**:

```go
conf.AddRoleTable("user", "products", core.Query{
    Filters:          []string{`{ owner_id: { eq: $user_id } }`},
    Columns:          []string{"id", "name", "price"},
    DisableFunctions: false,
    Limit:            100,
})

conf.AddRoleTable("user", "products", core.Insert{
    Presets: map[string]string{"owner_id": "$user_id"},
})
```

---

## Why GraphJin?

| Traditional Approach | With GraphJin |
|---------------------|---------------|
| Write REST endpoints for each use case | Write one GraphQL query |
| Manual SQL query optimization | Automatic LATERAL JOIN optimization |
| N+1 query problems | Single optimized query |
| Weeks of API development | Minutes |
| Maintain resolver code | Zero backend code |
| Manual security checks | Declarative role-based security |
| Database-specific code | Same code works on 8 databases |

GraphJin is production-ready, high-performance, and saves development teams thousands of hours.
