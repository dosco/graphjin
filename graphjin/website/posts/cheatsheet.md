---
chapter: 7
title: Cheatsheet
description: Validation, Roles, Access Control, GraphQL directives, Config options
---

# Cheatsheet

Quick reference to configuration and GraphQL features.

#### TOC

---

### Add validation

When inserting or updating rows you often want to add valition on your variables this can easy be done using the `@validate` directive.

```graphql
mutation
@validate(variable: "email", format: "email", min: 1, max: 100)
@validate(variable: "full_name", requiredIf: { id: 1007 })
@validate(variable: "id", greaterThan: 1006)
@validate(variable: "id", lessThanOrEqualsField: id) {
  users(insert: { id: $id, email: $email, full_name: $full_name }) {
    id
    email
    full_name
  }
}
```

```json
{
  "id": 1007,
  "email": "not_an_email"
}
```

| Arguments                | Example                         | Explained                                                               |
| ------------------------ | ------------------------------- | ----------------------------------------------------------------------- |
| format                   | format: "email"                 | Value must be of a format, eg: email, uuid                              |
| required                 | required: true                  | Variable is required                                                    |
| requiredIf               | requiredIf: { id: 123 }         | Variable is required if another variable equals a value                 |
| requiredUnless           | requiredUnless: { id: 123 }     | Variable is required unless another variable equals a value             |
| requiredWith             | requiredWith: id                | Variable is required if one of a list of other variables exist          |
| requiredWithAll          | requiredWithAll: [id, name]     | Variable is required if all of a list of other variables exist          |
| requiredWithout          | requiredWithout: [id, name]     | Variable is required if one of a list of other variables does not exist |
| requiredWithoutAll       | requiredWithoutAll: [id, name]  | Variable is required if none of a list of other variables exist         |
| max                      | max: 5                          | Maximum value a variable can be                                         |
| min                      | min: 3                          | Minimum value a variable can be                                         |
| equals                   | equals: 5                       | Variable equals a value                                                 |
| notEquals                | notEquals: 5                    | Variable does not equal a value                                         |
| oneOf                    | oneOf: [1,2,3]                  | Variable equals one of the following values                             |
| greaterThan              | greaterThan: 5                  | Variable is greater than a value                                        |
| greaterThanOrEquals      | greaterThanOrEquals: 5          | Variable is greater than or equal to a value"                           |
| lessThan                 | lessThan: 5                     | Variable is less than a value                                           |
| lessThanOrEquals         | lessThanOrEquals: 5             | Variable is less than or equal to a value                               |
| equalsField              | equalsField: id                 | Variable equals the value of another variable                           |
| notEqualsField           | notEqualsField: id              | Variable does not equal the value of another variable                   |
| greaterThanField         | greaterThanField: count         | Variable is greater than the value of another variable                  |
| greaterThanOrEqualsField | greaterThanOrEqualsField: count | Variable is greater than or equals the value of another variable        |
| lessThanField            | lessThanField: count            | Variable is less than the value of another variable                     |
| lessThanOrEqualsField    | lessThanOrEqualsField: count    | Variable is less than or equals the value of another variable           |

| Format              | Explained                |
| ------------------- | ------------------------ |
| alpha               | a-z, A-Z                 |
| alphaNumeric        | a-z, A-Z, 0-9            |
| alphaUnicode        | alpha and unicode        |
| alphaUnicodeNumeric | alphaNumeric and unicode |
| numeric             | +, - , . 0-9             |
| number              | 0-9                      |
| email               | valid email address      |
| uuid3               | uuid version 3           |
| uuid4               | uuid version 4           |
| uuid5               | uuid version 5           |
| ulid                | ulid id format           |

### The "where:" clause

This ability to finely filter and target the data you need is a powerful feature of GraphJin. This is used in several places:

- Table selector to find the right rows
- Used in table filters in the config file
- Used in `@skip` and `@include` directives
- Used with mutations `updates` and `upsert`

```graphql title="Query with a where clause"
query getProducts {
  products(
    where: { and: [{ not: { id: { is_null: true } } }, { price: { gt: 10 } }] }
    limit: 3
  ) {
    id
    name
    price
  }
}
```

#### Logical Operators

| Name | Example                                                      | Explained                       |
| ---- | ------------------------------------------------------------ | ------------------------------- |
| and  | price : { and : { gt: 10.5, lt: 20 }                         | price > 10.5 AND price < 20     |
| or   | or : { price : { greater_than : 20 }, quantity: { gt : 0 } } | price >= 20 OR quantity > 0     |
| not  | not: { or : { quantity : { eq: 0 }, price : { eq: 0 } } }    | NOT (quantity = 0 OR price = 0) |

#### Other operators

| Name                    | Example                                | Explained                                                                                                |
| ----------------------- | -------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| eq, equals              | id : { eq: 100 }                       | id = 100                                                                                                 |
| neq, not_equals         | id: { not_equals: 100 }                | id != 100                                                                                                |
| gt, greater_than        | id: { gt: 100 }                        | id > 100                                                                                                 |
| lt, lesser_than         | id: { gt: 100 }                        | id < 100                                                                                                 |
| gteq, greater_or_equals | id: { gteq: 100 }                      | id >= 100                                                                                                |
| lteq, lesser_or_equals  | id: { lesser_or_equals: 100 }          | id <= 100                                                                                                |
| in                      | status: { in: [ "A", "B", "C" ] }      | status IN ('A', 'B', 'C')                                                                                |
| nin, not_in             | status: { in: [ "A", "B", "C" ] }      | status IN ('A', 'B', 'C')                                                                                |
| like                    | name: { like "phil%" }                 | Names starting with 'phil'                                                                               |
| nlike, not_like         | name: { nlike "v%m" }                  | Not names starting with 'v' and ending with 'm'                                                          |
| ilike                   | name: { ilike "%wOn" }                 | Names ending with 'won' case-insensitive                                                                 |
| nilike, not_ilike       | name: { nilike "%wOn" }                | Not names ending with 'won' case-insensitive                                                             |
| similar                 | name: { similar: "%(b\|d)%" }          | [Similar Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-SIMILARTO-REGEXP)     |
| nsimilar, not_similar   | name: { nsimilar: "%(b\|d)%" }         | [Not Similar Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-SIMILARTO-REGEXP) |
| regex                   | name: { regex: "^([a-zA-Z]+)$" }       | [Regex Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-POSIX-TABLE)            |
| nregex, not_regex       | name: { nregex: "^([a-zA-Z]+)$" }      | [Regex Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-POSIX-TABLE)            |
| iregex                  | name: { iregex: "^([a-z]+)$" }         | [Regex Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-POSIX-TABLE)            |
| niregex, not_iregex     | name: { not_iregex: "^([a-z]+)$" }     | [Regex Docs](https://www.postgresql.org/docs/9/functions-matching.html#FUNCTIONS-POSIX-TABLE)            |
| has_key                 | column: { has_key: 'b' }               | Does JSON column contain this key                                                                        |
| has_key_any             | column: { has_key_any: [ a, b ] }      | Does JSON column contain any of these keys                                                               |
| has_key_all             | column: [ a, b ]                       | Does JSON column contain all of this keys                                                                |
| contains                | column: { contains: [1, 2, 4] }        | Is this array/json column a subset of value                                                              |
| contained_in            | column: { contains: "{'a':1, 'b':2}" } | Is this array/json column a subset of these value                                                        |
| is_null                 | column: { is_null: true }              | Is column value null or not                                                                              |

### Aggregation functions

If you need aggregated values from the database such as `count`, `max`, `min`, etc. This is simple to do with GraphQL, just prefix the aggregation name to the field name that you want to aggregrate like `count_id`. The below query will group products by name and find the minimum price for each group. Notice the `min_price` field we're adding `min_` to price. You can also use the function operation.

```graphql title="Using a function prefix min_"
query getProducts {
  products {
    name
    min_price
  }
}
```

```graphql title="Using just a function"
query getProducts {
  products {
    name
    minumumPrice: min(args: [price])
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

### Query directives

Directives are used to modify a query, a table selector, a field, etc

```graphql title="Query with directives"
query getProducts {
  products {
    name
    price
    owner @include(if: $include_owner) {
      full_name
    }
  }
}
```

| Directive   | Arguments      | Description                                                     |
| ----------- | -------------- | --------------------------------------------------------------- |
| @schema     | name: "string" | Set the database schema to use with this selector               |
| @skip       | if: $var       | Skip this query selector when the `if` variable is true         |
| @include    | if: $var       | Include this query selector only when the `if` variable is true |
| @notRelated |                | Tells the compiler to not relate this selector to its parent    |
| @through    | table: ""      | Tells the compiler which join table it should use for selector  |

`@through(table: "name")` is to be used when there are multiple join tables that create a path between a child and parent in a nested query, this directive will tell the SQL compiler which of the through tables (join tables) to use for this relationship.

#### Special Directives

| Directive     | Arguments                   | Description                                              |
| ------------- | --------------------------- | -------------------------------------------------------- |
| @cacheControl | maxAge: 500, scope: private | Sets the HTTP Cache-Control headers for APQ Get requests |

Special directives are different from standard directives since they can only be applied to the operation and not GraphQL selectors. See the below example for how the `@cacheControl` directive is used. Script is used in a similar manner see the next section for how to use it.

```
query @cacheControl(maxAge: 500) {
  users {
    id
  }
}
```

### Roles for access control

We use the concept of roles to auto. apply access control like filters, etc to a query. Out of the box we have two roles `user` when a user id is provided and `anon` for when its not. Each role has its own set of table level configuration. Additionally you can define your own roles (eg. `admin`)

The role can either be specified at query time or auto. derived using the `roles_query` and the `match` config parameters. The `role_query` is an SQL query to fetch the data required to make a decision on what the role should be. And `match` is like an `if` statement using SQL again to pick the matching role.

In the below example if the id is less than 10 or the internal column is set to true then the query is assigned the `admin` role.

```yaml
# Variables used require a type suffix eg. $user_id:bigint
roles_query: "SELECT id, internal FROM users WHERE id = $user_id:bigint"

roles:
  - name anon
    ...
  - name user
    ...
  - name: admin #custom role
    match: id < 10 or internal = true
    tables:
      - name: users
        filters: []
```

### Database schema file

A database schema file `db.graphql` is a special GraphQL (SDL) file that contains your database schema. This file is generated in development mode when the config option `enable_schema: true` is enabled.

Once this config option is enabled this schema file will be used in production mode instead of doing a database discovery which is useful for depolying to enviroments like serverless functions (AWS Lamda) to improve startup time.

```graphql title="Database schema file /config/db.graphql"
# dbinfo:postgres,120005,public

type purchases {
  id: Bigint! @id @unique
  quantity: Integer
  updated_at: TimestampWithTimeZone
  returned_at: TimestampWithTimeZone
  created_at: TimestampWithTimeZone!
  product_id: Bigint @relation(type: products, field: id)
  customer_id: Bigint @relation(type: users, field: id)
}

type users {
  phone: Text
  category_counts: Json
  avatar: Text
  updated_at: TimestampWithTimeZone
  stripe_id: Text
  full_name: Text!
  disabled: Boolean
  created_at: TimestampWithTimeZone!
  email: Text! @unique
  id: Bigint! @id @unique
}

type categories {
  description: Text
  updated_at: TimestampWithTimeZone
  id: Bigint! @id @unique
  created_at: TimestampWithTimeZone!
  name: Text!
}
```

### Introspection query

An introspection query is used to fetch a typed schema of a GraphQL API. In development mode GraphJin supports this query out of the box. The result of this query is a large complex JSON object that is mostly meant for tools such as IDE autocomplete plugins, client generators, etc to read. The result of this query is not designed for other software to parse and use.

If you require this introspection query result to be saved to a file in development mode then set the config option `enable_introspection: true` and a file `intro.json` will be generated in the config folder.

### GraphJin Configuration

Configuration can either be passed in via code or read in from a enviroment specific (dev.yml, prod.yml, etc) config file. Config files can inherit from another config file for example the `prod.yml` file inherits the `dev.yml` file to only override a few parameters.

```yaml title="dev.yml"
# When production mode is 'true' only queries
# from the allow list are permitted.
production: false

# Secret key for general encryption operations like
# encrypting the cursor data
secret_key: supercalifajalistics

# When set to true a database schema file will be generated in dev mode and
# used in production mode. Auto database discovery will be disabled
# in production mode.
enable_schema: false

# When set to true an introspection json file will be generated in
# this file can be used with other tooling to generate typed clients
# dev mode and enable autocomplete in an IDE, etc.
enable_introspection: false

# Subscriptions poll the database to query for updates
# this sets the duration (in seconds) between requests.
# Defaults to 5 seconds
subs_poll_every_seconds: 5

# Default limit value to be used on queries and as the max
# limit on all queries where a limit is defined as a query variable.
# Defaults to 20
default_limit: 20

# Disables all aggregation functions like count, sum, etc
# disable_agg_functions: false

# Disables all functions like count, length, etc
# disable_functions: false

# Enables using camel case terms in GraphQL which are converted
# to snake case in SQL
# enable_camelcase: false

# Set session variable "user.id" to the user id
# Enable this if you need the user id in triggers, etc
# Note: This will not work with subscriptions
set_user_id: false

# DefaultBlock ensures that in anonymous mode (role 'anon') all tables
# are blocked from queries and mutations. To open access to tables in
# anonymous mode they have to be added to the 'anon' role config.
default_block: false

# Define additional variables here to be used with filters
# Variables used require a type suffix eg. $user_id:bigint
variables:
  #admin_account_id: "5"
  admin_account_id: "sql:select id from users where admin = true limit 1"

# Define variables set to values extracted from http headers
header_variables:
  remote_ip: "X-Forwarded-For"

# Field and table names that you wish to block
blocklist:
  - ar_internal_metadata
  - schema_migrations
  - secret
  - password
  - encrypted
  - token

resolvers:
  - name: payments
    type: remote_api
    table: customers
    column: stripe_id
    json_path: data
    debug: false
    url: http://payments/payments/$id
    pass_headers:
      - cookie
    set_headers:
      - name: Host
        value: 0.0.0.0
      # - name: Authorization
      #   value: Bearer <stripe_api_key>

tables:
  - # You can create new fields that have a real db table backing them
    name: me
    table: users

  - name: users
    order_by:
      new_users: ["created_at desc", "id asc"]
      id: ["id asc"]

# Variables used require a type suffix eg. $user_id:bigint
roles_query: "SELECT * FROM users WHERE id = $user_id:bigint"

# Out of the box are two roles `user` and `anon`, the former is assigned when a user id is available and the later when its not.

# If `auth.type` is set to a valid auth type then all tables are blocked for the anon role unless added to the role like below.

roles:
  # Configs for the `anon` role includes per table configs
  - name: anon
    tables:
      - name: users
        query:
          limit: 10

  # Configs for the `user` role includes per table configs
  - name: user
    tables:
      - name: me
        query:
        	# Use filters to enforce table wide things like `{ disabled: false }`
          # where you never want disabled users to be shown.
          filters: ["{ id: { _eq: $user_id } }"]

      - name: products
        query:
          limit: 50
          filters: ["{ user_id: { eq: $user_id } }"]
          disable_functions: false

        insert:
          filters: ["{ user_id: { eq: $user_id } }"]
          presets:
            - user_id: "$user_id"
            - created_at: "now"

        update:
          filters: ["{ user_id: { eq: $user_id } }"]
          presets:
            - updated_at: "now"

        delete:
          block: true

  - name: admin
    match: id = 1000
    tables:
      - name: users
        filters: []
```

```yaml title="prod.yml"
# Inherit config from this other config file
# so I only need to overwrite some values
inherits: dev

# When production mode is 'true' only queries
# from the allow list are permitted.
production: true

# Secret key for general encryption operations like
# encrypting the cursor data
secret_key: supercalifajalistics

# Subscriptions poll the database to query for updates
# this sets the duration (in seconds) between requests.
# Defaults to 5 seconds
subs_poll_every_seconds: 5

# Default limit value to be used on queries and as the max
# limit on all queries where a limit is defined as a query variable.
# Defaults to 20
default_limit: 20
```
