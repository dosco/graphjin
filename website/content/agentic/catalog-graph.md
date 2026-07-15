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

## Coordinated discovery and semantic recall

The service starts from an immutable filesystem discovery generation. On a warm
start it serves that generation immediately while one replica refreshes schema
metadata in the background. In a horizontal deployment, mount
`discovery_cache.path` on a shared read-after-write filesystem and configure
Redis. Redis holds only leases, fencing tokens, active generation IDs, status,
and notifications; schema snapshots and catalog indexes remain files.
For a single GraphJin process Redis is optional: discovery and semantic builds
use in-process coordination while retaining the same filesystem generations.

```yaml
discovery_cache:
  enabled: true
  path: .graphjin/discovery
  refresh_interval: 5m
  startup_wait: 2m
  retain_generations: 2

catalog_search:
  semantic:
    enabled: true
    provider: openai
    embedding_model: text-embedding-3-small
    api_key_env: OPENAI_API_KEY
    dimensions: tiny # 128; small=256, medium=512, default=provider-native
```

Semantic search is a recall layer, not a join planner. It retrieves likely
business concepts and table endpoints, then GraphJin follows caller-visible,
real foreign-key relationships to add short paths. Exact identifiers remain
lexical-first, and provider errors or the two-second embedding deadline return
lexical results. The index embeds bounded table identity, column-facet, and
relationship-neighborhood documents instead of one vector per column.

### How the built-in agent uses semantic recall

When the service successfully initializes semantic catalog search, it adds a
private feature profile to the built-in agent. The required first discovery
step does not change: GraphJin still seeds every run with
`query_catalog(search: <full user instruction>, explain: true)` before the
model runs. Lexical-only agents receive the existing prompt and tool schema.

The semantic profile teaches the agent to use short business-intent phrases in
the user's terminology. It should not search with guessed table names, SQL,
GraphQL, provider terms, or sample values. For a multi-entity request it starts
with the combined relationship intent, treats the returned semantic matches as
candidates, inspects their catalog card IDs, and accepts joins only from real
relationship paths returned by the catalog.

If the seed is incomplete, the internal agent may make one adaptive coverage
call with two or three unique phrases. Expansion is appropriate only when a
required endpoint or verified relationship path is missing, columns are needed
but only tables were found, or the result is empty or materially ambiguous.
The typical batch contains the compact combined intent, the first missing
concept, and a second concept or relationship/action phrase. Exact and already
well-covered searches are not expanded.

GraphJin embeds all uncached phrases in that coverage call with one Ax request,
then evaluates each phrase independently. It keeps per-phrase IDs and
explanations, pins exact identifiers, reserves one distinct result per phrase,
fuses the remaining ranks, and finally follows at most two caller-visible
foreign-key paths. Retrieval metadata tells the agent whether results were
exact, hybrid, or lexical fallback. A provider failure, dimension mismatch,
warming index, or two-second deadline falls back to lexical groups for the
whole batch without making the service unavailable.

The `searches` field is private to the service-owned agent. It is deliberately
absent from the public MCP `query_catalog` schema, YAML configuration, and the
direct-core API.

See [Discovery Cache And Semantic Search](/configure/discovery-semantic-search/)
for the complete startup matrix, Redis outage behavior, generation validation,
dimension presets, incremental rebuild rules, retrieval thresholds, storage
estimate, security boundaries, and operational checklist.
