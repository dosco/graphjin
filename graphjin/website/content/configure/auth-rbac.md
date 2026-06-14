---
title: "Auth And RBAC"
description: "Configure authentication, role queries, table permissions, row filters, and column blocking."
nav_group: "configure"
doc_kind: "reference"
weight: 30
---

## Auth providers

GraphJin supports JWT/OIDC-style config, JWKS refresh, static public keys, and header authentication.

```yaml
auth:
  type: jwt
  jwt:
    jwks_url: https://issuer.example.com/.well-known/jwks.json
    audience: graphjin
```

{{< svg "auth-modes" "GraphJin authentication and authorization modes" >}}

## Identity mapping in source mode

Source mode centralizes common identity claims and then generates the lower-level filters and presets GraphJin already enforces in the compiler:

```yaml
identity:
  user_id_claim: sub
  role_claims: [role, roles]
  namespace_claim: account_id
  admin_roles: [admin]

sources:
  - name: app
    kind: database
    type: postgres
    access:
      read: account
      write: blocked
      delete: blocked
      namespace_column: account_id
      public_tables: [countries, plans]
      admin_tables: [audit_logs]
      blocked_tables: [internal_events]
```

{{< verified by="TestApplySourceAccessRulesGeneratesAccountFiltersAndClassifications" file="core/source_access_test.go" line="14" >}}

## Role query

Roles can come from SQL or GraphQL role queries. GraphQL role queries return fields that role predicates match against.

{{< verified by="TestGraphQLRoleQueryMatchesConfiguredRole" file="core/role_query_graphql_test.go" line="14" >}}

## Table permissions

Per-table role rules control query, insert, update, upsert, and delete operations. Rules can set limits, filters, column allow/block lists, presets, and operation blocks.

```yaml
roles:
  - name: user
    tables:
      - name: products
        query:
          filters:
            - "{ owner_id: { eq: $user_id } }"
          columns: ["id", "name", "price"]
        insert:
          columns: ["name", "price"]
          presets:
            owner_id: "$user_id"
        delete:
          block: true
```

In source mode, do not mix user-written `roles[].tables` rules with `sources:`. Migrate repeated account filters to `sources[].access` and keep legacy role table rules for database-only legacy configs.

## Column and aggregate enforcement

Column allow-lists also apply inside expressions and aggregate metrics. If a role cannot read `price`, `sum(of: price)` and expression aggregates that reference `price` should fail the same way a direct `price` field would.

{{< verified by="Example_queryWithExprRoleAllowlist" file="tests/query_test.go" line="2468" >}}
