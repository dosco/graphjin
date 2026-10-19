---
title: "Apollo Federation"
description: "Expose GraphJin as a database-backed Apollo Federation v2 subgraph."
nav_group: "integrations"
doc_kind: "guide"
weight: 60
---

## Enable federation

```yaml
federation:
  enabled: true
  version: v2.5
  keys:
    users: ["email"]
  shareable:
    - Users.email
  inaccessible:
    - Users.encrypted_password
  tags:
    Users.full_name: ["pii"]
```

GraphJin can emit federation SDL, answer `_service { sdl }`, derive keys from primary keys, and apply configured `@shareable`, `@inaccessible`, and `@tag` annotations.

{{< svg "federation-supergraph" "Apollo Federation v2 supergraph integration" >}}

{{< verified by="TestBuildFederationSDL_Smoke" file="core/federation_test.go" line="13" >}}
{{< verified by="TestBuildFederationSDL_FieldDirectives" file="core/federation_test.go" line="84" >}}

## Federation queries

```graphql
query {
  _service {
    sdl
  }
}
```

GraphJin dispatches `_service` and `_entities` internally, while regular database queries continue through the normal compiler path.

{{< verified by="TestHandleFederationQuery_Service" file="core/federation_dispatch_test.go" line="42" >}}
{{< verified by="TestHandleFederationQuery_PassthroughForRegularQuery" file="core/federation_dispatch_test.go" line="102" >}}

## Why use it

Use federation when GraphJin should be the database-backed subgraph in a larger supergraph. You still get schema discovery, compiled database access, and GraphJin policy without writing resolver glue.

Remote tables and blocked tables are not exported as federated entities. Entity keys come from primary keys unless overridden, so tables without a stable key need explicit config before they belong in a supergraph.
