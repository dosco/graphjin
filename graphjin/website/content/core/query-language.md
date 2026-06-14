---
title: "Query Language"
description: "Fields, aliases, variables, fragments, directives, search, JSON paths, and expressions."
nav_group: "core"
doc_kind: "reference"
weight: 20
---

## Basic selection

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

{{< verified by="Example_query" file="tests/query_test.go" line="18" >}}

## Primary-key lookup and object shape

```graphql
query Product($id: ID!) {
  products(id: $id) {
    id
    name
    owner {
      email
    }
  }
}
```

```json
{ "id": 2 }
```

Primary-key lookup returns a single object. Collection queries return arrays.

{{< verified by="Example_queryByID" file="tests/query_test.go" line="558" >}}

## Variables and defaults

```graphql
query Products($limit: Int = 5) {
  products(limit: $limit, order_by: { id: asc }) {
    id
    name
  }
}
```

{{< verified by="Example_queryWithVariablesDefaultValue" file="tests/query_test.go" line="942" >}}

Use variables for values, not for whole query structures. In filters, keep the filter object inline and place variables at leaf values.

## Fragments

```graphql
fragment userFields on users {
  id
  email
}

query {
  users {
    ...userFields
  }
}
```

{{< verified by="Example_queryWithFragments1" file="tests/query_test.go" line="1004" >}}

## Aliases

Aliases are useful when the database column name is not the public field name you want to expose.

```graphql
query {
  users {
    id
    fullName: full_name
  }
}
```

{{< verified by="Example_queryWithAlternateFieldNames" file="tests/query_test.go" line="532" >}}

## Directives

GraphJin supports GraphQL-style conditional fields plus GraphJin-specific add/remove directives for dynamic response shaping.

```graphql
query ($showProducts: Boolean!) {
  users {
    id
    products @include(if: $showProducts) {
      id
    }
  }
}
```

{{< verified by="Example_queryWithSkipAndIncludeDirective1" file="tests/query_test.go" line="1192" >}}

GraphJin also supports role and variable variants such as `@include(ifRole:)`, `@skip(ifRole:)`, `@include(ifVar:)`, and `@skip(ifVar:)`, plus relationship directives such as `@through(table:)` and `@through(column:)`.

## Expressions

Expression fields let you compute values from selected columns while staying inside the compiled query path.

```graphql
query {
  products(where: { id: { lteq: 100 } }) {
    doubled: sum(expr: { mul: [id, 2] })
  }
}
```

{{< verified by="Example_queryWithExprMul" file="tests/query_test.go" line="2390" >}}

## JSON paths and search

```graphql
query {
  products(limit: 10, order_by: { id: asc }, where: { metadata_foo: { eq: true } }) {
    id
    metadata_foo
  }
}
```

```graphql
query SearchProducts($query: String!) {
  products(search: $query, limit: 5) {
    id
    name
  }
}
```

JSON path shorthand and full-text search are backend-dependent features. Use [Database Support](/reference/database-support/) when writing portable docs or examples.

{{< verified by="Example_queryJSONPathOperationsAlternativeSyntax" file="tests/query_test.go" line="129" >}}
{{< verified by="Example_queryBySearch" file="tests/query_test.go" line="586" >}}
