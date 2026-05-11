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
  - [Window Functions](#window-functions)
  - [Full-Text Search](#full-text-search)
  - [JSON Operations](#json-operations)
  - [GraphQL Fragments](#graphql-fragments)
  - [Polymorphic Relationships](#polymorphic-relationships)
  - [Directives](#directives)
  - [Remote API Joins](#remote-api-joins)
  - [OpenAPI Integration](#openapi-integration)
  - [Database Functions](#database-functions)
- [Mutation Capabilities](#mutation-capabilities)
  - [Simple Inserts](#simple-inserts)
  - [Bulk Inserts](#bulk-inserts)
  - [Nested Inserts](#nested-inserts)
  - [Connect & Disconnect](#connect--disconnect)
  - [Validation](#validation)
  - [Updates](#updates)
- [Real-time Subscriptions](#real-time-subscriptions)
- [File Uploads](#file-uploads)
- [Filesystem Tables](#filesystem-tables)
- [CodeSQL Source Indexes](#codesql-source-indexes)
- [Apollo Federation v2](#apollo-federation-v2)
- [Security Features](#security-features)
  - [Role-Based Access Control](#role-based-access-control)
  - [Row-Level Security](#row-level-security)
  - [Column Blocking](#column-blocking)
  - [Read-Only Databases](#read-only-databases)
  - [Query Allow Lists](#query-allow-lists)
  - [Response Caching (SWR)](#response-caching-swr)
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

### Window Functions

Tag any aggregate or function field with `@window` to emit a SQL window function — `<func>(...) OVER (PARTITION BY ... ORDER BY ... <frame>)`. Window queries return one row per input row (no GROUP BY collapse), so they compose with regular column selections.

```graphql
query {
  orders {
    user_id
    total
    rank: row_number @window(
      partition: ["user_id"],
      order: ["total desc nulls last"]
    )
    running: sum_total @window(
      partition: ["user_id"],
      order: ["created_at"],
      frame: "rows between 5 preceding and current row"
    )
  }
}
```

**Frame grammar** — full standard SQL is accepted; numeric offsets are parsed as integers (no SQL-fragment passthrough):

```
ROWS|RANGE UNBOUNDED PRECEDING
ROWS|RANGE CURRENT ROW
ROWS|RANGE <n> PRECEDING
ROWS|RANGE <n> FOLLOWING
ROWS|RANGE BETWEEN <bound> AND <bound>
```

where `<bound>` is `UNBOUNDED PRECEDING` / `UNBOUNDED FOLLOWING` / `CURRENT ROW` / `<n> PRECEDING` / `<n> FOLLOWING`.

**Order entries** accept `"col [asc|desc] [nulls first|last]"`. NULLS placement is honoured by Snowflake / Postgres / Oracle, silently ignored elsewhere (per dialect spec).

**Empty `@window`** is valid — emits a bare `OVER ()` for ranking functions that don't need a partition or order:

```graphql
{ products { id, total: sum_price @window } }
```

**Validation** — partition and order columns are validated against the table; numeric offsets are parsed as non-negative integers; frame text is canonicalised before emission. The frame argument cannot smuggle SQL fragments past the parser.

**Dialect support** — Postgres, MySQL 8.0+, MariaDB 10.2+, MSSQL 2012+, Oracle, SQLite 3.25+, Snowflake, CockroachDB. (MySQL 5.7 and pre-10.2 MariaDB don't support window functions; the emitted SQL would error at exec time on those.)

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

Combine database data with external REST APIs as child fields on a parent table:

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

`remote_api` is the right tool for ad-hoc URL joins. For APIs that publish an OpenAPI spec, the [OpenAPI integration](#openapi-integration) (below) is usually a better fit — it derives auth, parameter wiring, and the response shape from the spec instead of asking you to wire each endpoint by hand.

### OpenAPI Integration

Drop an OpenAPI 3 spec into `config/specs/`, declare credentials and join wiring in your environment config, and every classifiable operation in the spec becomes a GraphQL field. Two shapes are emitted automatically:

**Row-join fields** — `GET /resource/{id}` declared in `joins:` shows up as a child field on a parent DB table:

```yaml
# config/dev.yml
openapi:
  interaction_studio:
    base_url: https://${IS_ACCOUNT}.personalization.salesforce.com/api
    auth:
      scheme: token_exchange
      token_url: https://${IS_ACCOUNT}.personalization.salesforce.com/api/token
      request:
        body:
          apiKeyId: ${IS_API_KEY}
          apiKeySecret: ${IS_API_SECRET}
      response:
        token_field: access_token
        expires_field: expires_in
    joins:
      getUserById:
        parent_table: users
        parent_column: email
        param: userId
        expose_as: is_profile
```

```graphql
query {
  users(where: { id: { eq: 42 } }) {
    id
    email
    is_profile {           # → GET /users/{userId} with userId = users.email
      lastSeenAt
      segments { id name }
    }
  }
}
```

**Top-level virtual tables** — `GET /resource/{id}` (single) or `GET /resources` (list) without a `joins:` entry surface as their own root fields, with path/query parameters mapped to GraphQL field arguments:

```graphql
query {
  is_get_user_by_id(userId: "u_123") {   # path param
    lastSeenAt
  }
  is_list_audit_logs(actorId: "u_123") { # query param
    items { ts action }
  }
}
```

**What gets classified** — every GET operation in the spec is auto-categorised:

| Mode | Path shape | Behaviour |
|---|---|---|
| Row-join | `GET /resource/{id}` with a matching `joins:` entry | Child field on the parent DB table; parent column populates the path param |
| Top-level (single) | `GET /resource/{id}` without a `joins:` entry | Root field; path param becomes a required field argument |
| Top-level (list) | `GET /resources` with optional query filters | Root field; each query param becomes an optional field argument |
| Skipped | Non-JSON response, mutating verb (POST/PUT/DELETE), or unsupported auth | Logged at boot; not exposed |

**Authentication** — declared in YAML, applied automatically to every request to the upstream:

| Scheme | Use for |
|---|---|
| `bearer` | static or pass-through tokens |
| `basic` | username/password |
| `api_key` | header or query param |
| `oauth2_client_credentials` | machine-to-machine OAuth |
| `token_exchange` | vendor-specific POST → JSON token flows |

**Per-spec concurrency, response shaping, and overrides** — `result_path` strips JSON wrappers, `expose_as` renames a field on collision, `concurrency_limit` caps parallel calls per spec. See the [OpenAPI Integration config reference](CONFIG.md#openapi-integration) for the full surface.

**Boot log** reports what loaded and what was skipped:

```
openapi: loaded interaction_studio.yaml (5 active, 12 skipped)
openapi: GET /exports/{jobId} — skipped: non-JSON response (application/octet-stream)
openapi: GET /users/{userId} — exposed as is_profile (row-join on users.email)
```

The integration is read-only today (GET only). Mutating verbs are a deferred feature — the [Filesystem Tables](#filesystem-tables) abstraction is the recommended path for write-side object-store operations.

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

## File Uploads

The GraphQL endpoint accepts `multipart/form-data` POSTs alongside JSON, following the [graphql-multipart-request-spec](https://github.com/jaydenseric/graphql-multipart-request-spec). Files are placed at variable paths declared in the request's `map` field.

```yaml
uploads:
  enabled: true
  max_size: 25_000_000              # bytes; defaults to 25 MB
  allowed_mime: ["image/*", "application/pdf"]
  storage: avatars                   # optional: filesystem table to stream into
  storage_key_prefix: "{date}/"     # optional: {date} → YYYY/MM/DD
```

**Two modes**, controlled by whether `storage` is set:

**Inline base64** (no `storage`) — the file becomes a JSON object inside the variable:

```json
{ "filename": "logo.png", "content_type": "image/png",
  "size": 12345, "data": "<base64 bytes>" }
```

Mutations bind this object to a JSONB column or to a PL/pgSQL function that decodes `data` into `bytea`.

**Streamed to a filesystem table** (`storage: avatars`) — the body is written to the named [filesystem table](#filesystem-tables) and the variable becomes a stable reference:

```json
{ "key": "2026/05/08/abc123.png",
  "url":  "https://s3.../...?presigned",
  "size": 12345,
  "content_type": "image/png" }
```

This keeps large bodies out of the GraphQL request/response and gives mutations a queryable handle to the stored object.

**Generated keys** are `<prefix>/<8-byte-hex><ext>` — collisions are near-impossible and the upstream filename never reaches storage (useful when filenames are user-supplied).

**Validation** — total request bounded by `max_size`, MIME type checked against `allowed_mime` (glob patterns supported: `image/*`, `application/pdf`).

---

## Filesystem Tables

Object stores show up as ordinary tables in the GraphQL schema. Declare a filesystem in config and it gets the same query surface as a database table — no per-storage GraphQL plumbing needed.

```yaml
filesystems:
  - name: avatars
    backend: s3
    bucket: my-bucket
    prefix: avatars/
    region: us-east-1
    presign_ttl: 15m

  - name: invoices
    backend: gcs
    bucket: invoices
    prefix: 2026/

  - name: uploads_local
    backend: local
    root: /var/lib/graphjin/uploads
```

Every filesystem table exposes the same columns regardless of backend:

| Column | Type | Description |
|---|---|---|
| `key` | text (PK) | Object's path within the table's effective root |
| `size` | bigint | Bytes |
| `content_type` | text | MIME type, best-effort |
| `etag` | text | Backend-defined identifier |
| `modified_at` | timestamp | Last-modified timestamp (RFC3339) |
| `url` | text | Presigned GET URL by default |
| `data` | text | base64 body — populated only when `inline_data: true` |

**List queries**:

```graphql
{ avatars(prefix: "users/", limit: 50, after: $cursor) {
    key size content_type modified_at url
  }
}
```

**Single-key fetch** (HEAD-equivalent + presigned URL):

```graphql
{ avatars(key: "users/42.png") { key size url } }
```

**Inline body** (small files only — heavyweight path):

```graphql
{ avatars(key: "users/42.png", inline_data: true) {
    key size url data    # data is base64
  }
}
```

**Per-table options**:

| Field | Purpose |
|---|---|
| `presign_ttl` | URL validity (default 15 min) |
| `public_base_url` | When set, replaces presigned URLs with `<base>/<key>` — useful for CDN fronting |
| `endpoint` | S3 endpoint override (MinIO, localstack, R2, etc.) |
| `max_list_page_size` | Caps `limit:` for list queries (default 1000) |

**Authentication** — uses each platform's standard credential chain, never embedded in GraphJin config:
- **S3**: env vars / `~/.aws/credentials` / IRSA / EC2 IMDS
- **GCS**: Application Default Credentials (env, GCE/GKE metadata server, `gcloud auth`)
- **Local**: filesystem permissions

**Build-tag gating** — slim builds drop SDK weight. `-tags no_s3` excludes the S3 backend, `-tags no_gcs` excludes GCS. Local is always built in.

**Custom backends** — register through `core.OptionSetFilesystemBackend(name, factory)` to plug in Azure Blob, R2, or anything implementing the `fstable.Backend` interface.

---

## CodeSQL Source Indexes

CodeSQL makes a source tree behave like another database in GraphJin. Configure a folder, and GraphJin creates a managed SQLite cache under `config/codesql/`, indexes source files with tree-sitter, reconciles new/changed/deleted files on startup, and watches the tree while the service runs.

```yaml
databases:
  warehouse:
    type: snowflake
    connection_string: user@account/warehouse/public?warehouse=compute_wh

  app_code:
    type: codesql
    path: /srv/app
```

The cache filename uses the database config name as a prefix, for example `config/codesql/app_code-<source-root-hash>.sqlite`. Legacy single-database config uses `default-<hash>.sqlite`.

CodeSQL stores three useful layers:

| Layer | Tables | Purpose |
|-------|--------|---------|
| Source/index state | `code_languages`, `code_grammars`, `code_query_packs`, `code_files`, `code_file_versions`, `code_index_status`, `code_parse_errors` | What was indexed, when, with which grammar/query-pack versions |
| Raw syntax | `code_nodes`, `code_captures` | Tree-sitter nodes, byte/range positions, named/error/missing flags, query captures |
| Code intelligence | `code_symbols`, `code_scopes`, `code_locals`, `code_refs`, `code_imports`, `code_edges`, `code_injections`, docs/text FTS | Searchable symbols, imports, refs, locals, injections, docs, and file text |

```graphql
query {
  code_symbols(
    where: { name: { iregex: "handler|resolver|workflow" } }
    order_by: { name: asc }
    limit: 20
  ) {
    name
    kind
    language
    start_row
  }
}
```

Because CodeSQL is projected through SQLite, it composes with the rest of GraphJin: an agent can query operational systems and the code that operates them from one MCP surface. That is the practical unlock for organizations: the LLM can inspect tables, workflows, imports, call sites, and docs together without raw shell access or a separate source-code service. It can answer questions like "which endpoints write orders?", "which code paths mention this column?", and "what changed around this data flow?" using the same audited GraphJin interface.

Boundaries are intentionally clear in v1: CodeSQL stores syntax structure, captures, scopes, local bindings, imports, references, calls, comments/docs, parse errors, and injected-language regions. It does not claim semantic type resolution, full cross-file symbol binding, inheritance correctness, or LSP-grade go-to-definition.

---

## Apollo Federation v2

GraphJin can register as a federation subgraph so it composes alongside other services behind Apollo Router / Cosmo / Hive Gateway:

```yaml
federation:
  enabled: true
  version: "v2.5"
  keys:                              # auto-derived from PKs by default
    users: ["id"]
    orders: ["id", "tenant_id"]      # composite keys via override
  shareable: ["Tag.name"]
  inaccessible: ["Users.encrypted_password"]
  tags:
    Users.full_name: ["pii"]
```

**`_service { sdl }`** — returns a federation-flavoured SDL describing every non-blocked, primary-keyed table:

```graphql
extend schema @link(url: "https://specs.apollo.dev/federation/v2.5",
  import: ["@key","@shareable","@external","@requires","@provides","@inaccessible","@tag"])

type Users @key(fields: "id") {
  id: ID!
  full_name: String! @tag(name: "pii")
  email: String! @shareable
  encrypted_password: String! @inaccessible
}

scalar _Any
type _Service { sdl: String! }
union _Entity = Users | Products | ...

extend type Query {
  _service: _Service!
  _entities(representations: [_Any!]!): [_Entity]!
}
```

**Composition succeeds out-of-the-box** — Apollo Router can register the subgraph and route non-entity queries straight to GraphJin.

**`_entities` resolution is on the roadmap** — the engine returns a clear "not yet implemented" error today rather than silent failures, so cross-subgraph entity references will surface the gap to gateway operators instead of producing confusing partial responses.

**Detection is fast** — token-bounded substring scan over the raw query, so JSON traffic costs one extra MIME parse only when federation is enabled.

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

### Response Caching (SWR)

Cache GraphQL responses with automatic invalidation on mutations. Pluggable backend — Redis for shared cache across instances, in-memory LRU as the no-config fallback.

```yaml
caching:
  ttl: 3600           # hard expiry (seconds); entry is dropped after this
  fresh_ttl: 300      # soft expiry; entries past this trigger SWR refresh
  exclude_tables: ["sessions"]
```

**Stale-while-revalidate** — when an entry is past `fresh_ttl` but inside `ttl`:

1. The stale response is returned to the caller immediately (single-digit ms)
2. A background worker re-runs the query under the same role and overwrites the cache entry
3. Concurrent refreshes for the same key are deduplicated via singleflight
4. The worker pool is bounded so a thundering herd of stale hits can't spawn unbounded goroutines

**Automatic invalidation** — mutations that touch a row drop every cached query that depended on that row. Indexing is row-level for small results (≤500 rows) and falls back to table-level for large results — so analytic queries don't pay row-tracking overhead.

**Cache key** — includes operation name, query text, variables, role, and (for ABAC) user ID. Anonymous queries (no operation name, no APQ key) bypass the cache entirely.

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
| Snowflake | Yes | Yes | No | No | No |
| CockroachDB | Yes | Yes | Yes | Yes | Yes |

Also works with: **AWS Aurora/RDS**, **Google Cloud SQL**, **YugabyteDB**. Snowflake supports key pair (JWT) authentication.

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
