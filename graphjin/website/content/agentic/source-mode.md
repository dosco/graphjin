---
title: "Source Mode"
description: "Use source-local configuration, capabilities, and reloads for agentic deployments."
nav_group: "agentic"
doc_kind: "guide"
weight: 40
---

## Sources as the unit of ownership

Source mode replaces one monolithic database assumption with named sources. A source can represent a database, filesystem, code index, OpenAPI surface, or GraphJin system surface.

```yaml
sources:
  - name: app
    kind: database
    type: postgres
    capabilities:
      data.read: true
      data.write: true
  - name: docs
    kind: file
    capabilities:
      files.list: true
      files.read: true
```

Capabilities are centralized in the source capability registry and should not be invented ad hoc by catalog, security, MCP, or config code.

{{< verified by="TestSourceCardsUseCapabilityRegistry" file="core/internal/catalog/build_test.go" line="84" >}}
{{< verified by="TestSecurityNanoRowsSourceCapabilities" file="serv/control_plane_graphql_test.go" line="1447" >}}

## Access defaults

```yaml
identity:
  namespace_claim: account_id
  user_id_claim: sub

sources:
  - name: app
    kind: database
    type: postgres
    access:
      read: account
      write: blocked
      delete: blocked
      namespace_column: account_id
      missing_namespace_column: block
```

The generated filters use trusted identity values from the request context. Client variables named `account_id` or `user_id` are not trusted for generated source-mode checks.

## Agentic environment

`GO_ENV=agentic` requires `agentic.yml`. Agentic configs can inherit production settings and then enable model-facing discovery surfaces deliberately.

```yaml
inherits: prod
mode: agentic
```

{{< verified by="TestReadInConfigAgenticCanInheritProd" file="serv/serv_test.go" line="56" >}}

## Source-scoped reloads

Config updates that touch only one source can use a source-scoped reload path. GraphJin stages the config, validates the runtime, swaps the changed source, and preserves unrelated sources when the transaction succeeds.

{{< verified by="TestHandleUpdateCurrentConfig_SourcePatchUsesSourceScopedReload" file="serv/mcp_config_transaction_test.go" line="598" >}}
{{< verified by="TestGraphQLConfigUpdateSourcesPatchPreservesSourcesAndRecordsCatalogEvent" file="serv/control_plane_graphql_test.go" line="1827" >}}
