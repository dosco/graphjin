---
title: "Catalog Graph"
description: "Let agents discover databases, tables, columns, relationships, operations, capabilities, and examples."
nav_group: "agentic"
doc_kind: "concept"
weight: 20
---

## What the catalog exposes

`gj_catalog` is the model-facing map of the usable graph. It can expose:

- Databases and sources.
- Tables and columns.
- Relationships and cross-database paths.
- Operations, syntax, examples, and source capabilities.
- Evidence for why something is available or blocked.

```yaml
sources:
  - name: graphjin
    kind: graphjin
    access:
      roots:
        gj_catalog: authenticated
        gj_security: admin
        gj_runtime: admin
        gj_artifacts: authenticated
```

## Why it matters

Agents should not guess table names, relationship paths, or write permissions. They should query the catalog first, then construct narrower GraphQL or MCP actions from evidence.

## Cold-start pattern

```graphql
query {
  gj_catalog(search: "find orders and customer relationships") {
    kind
    name
    summary
    evidence_json
  }
}
```

Catalog rows include JSON fields designed for model use: `details_json`, `evidence_json`, `examples_json`, `safety_json`, and `edges_json`. For goal-driven setup, search for `config_recipe` rows and inspect the exact recipe before applying config changes.

Saved queries, fragments, and workflows in `gj_catalog` use the artifact overlay. A caller sees global config files plus their own `gj_artifacts` rows; another caller does not see those user artifacts.

{{< verified by="TestCatalogSearchRanksRelationshipsAboveTablesForJoinIntent" file="core/catalog_test.go" line="179" >}}
{{< verified by="TestGraphQLControlPlaneCatalogConfigRecipeSearch" file="serv/control_plane_graphql_test.go" line="1365" >}}
{{< verified by="TestCatalogSnapshotMergesCallerScopedArtifacts" file="serv/artifact_overlay_test.go" line="173" >}}

## Narrower discovery

```graphql
query {
  tables: gj_catalog(where: { kind: { eq: "table" } }, limit: 20) {
    id
    name
    summary
    safety_json
  }

  joins: gj_catalog(search: "orders customer relationship") {
    id
    kind
    name
    evidence_json
  }
}
```

MCP clients should prefer `query_catalog(search: "...")` for goal-driven discovery and `graphql_help(for: "...")` only when the intent is unclear or a narrower help topic is needed.
