# Agentic GraphJin

`VISION.md` explains why GraphJin should become the shared operating surface for
an organization. This document explains how that surface works in an agentic
deployment.

Agentic GraphJin is not a resolver framework and not a pile of tool wrappers.
It is GraphJin running in sources mode for company end users who work through
agents. GraphJin gives those agents a governed graph over live data, source
code, security posture, workflows, config, and source-backed external systems.

The model-facing rule is simple:

```text
discover from gj_catalog
  -> inspect evidence
  -> check gj_security before risky actions
  -> join into application data, gj_code, workflows, or config
  -> validate or preview
  -> act through the governed GraphJin surface
  -> observe and refresh discovery
```

The important part is that the agent learns the company system from the graph
itself, not from memory, pasted schema, or guessed conventions.

## Graph Surfaces And Boundaries

In agentic mode, sources mode is assumed. GraphJin composes several kinds of
truth into one GraphQL/MCP operating loop. The boundaries matter because they
tell an agent where facts come from and where actions are allowed.

| Surface | Owner | Role |
| :--- | :--- | :--- |
| Application roots | Existing databases, MongoDB collections, OpenAPI/remote API sources, filesystem/object sources | Business data and external system state. These are source-owned facts. |
| `gj_catalog` | GraphJin catalog | Discovery spine for schema, relationships, language features, workflows, config facts, capabilities, entrypoints, examples, and evidence. |
| `gj_security` | GraphJin security report | Read-only security posture, effective policy, and findings. Agents check this before write-capable or control-plane actions. |
| `gj_code` | CodeSQL | Source intelligence over files, symbols, references, imports, docs, database references, parse state, change sets, and locks. |
| `gj_workflow` | GraphJin workflow source | Reusable JavaScript workflow definitions and metadata. |
| `gj_workflow_execution` | GraphJin workflow runtime | Mutation-only workflow execution. Returns an ephemeral result row and does not store run history. |
| `gj_config` | GraphJin config | Redacted current configuration and guarded config updates when policy allows them. |

Application roots remain the source of business truth. A Postgres table, MongoDB
collection, OpenAPI operation, remote API resolver, local filesystem table, S3
bucket projection, or GCS object table is not copied into a fake agent store.
GraphJin exposes it as a graph surface and keeps the operational boundary
visible.

GraphJin-owned system surfaces are compact and queryable. `gj_catalog`,
`gj_security`, `gj_workflow`, `gj_workflow_execution`, and `gj_config` are backed
by nanoDB through the GraphJin system source. nanoDB gives these surfaces
typed columns, indexes, full-text search, relationships, filtering, ordering,
limits, and atomic snapshot refreshes. It is for compact system truth, not for
replacing user databases or CodeSQL.

Code truth lives in CodeSQL. A repository can produce millions of files, syntax
nodes, symbols, references, docs, text chunks, and database references. That
belongs in durable indexed storage and is projected through the public
`gj_code` root.

## The Discovery Spine: `gj_catalog`

`gj_catalog` is the first root an agent should learn. It is the map of the
system, and every row is a catalog item selected primarily by `kind`.

The public `gj_catalog` row set combines:

- database/schema/table/column/function/index/relationship metadata
- source and config facts, with sensitive values redacted
- GraphJin language features, directives, operators, query patterns, mutation
  patterns, and common mistakes
- workflow metadata, variables, hashes, timestamps, and lifecycle facts
- MCP and GraphQL capabilities, including input/output shape and safety notes
- entrypoints for broad discovery tasks
- system capabilities such as `gj_security.query`
- details, examples, evidence, suggested next steps, and nearby graph edges
- catalog revision, workflow revisions, source hashes, and timestamps used to
  know when discovery changed
- full-text search metadata and ranked search scores

The stable row shape is intentionally model-friendly:

```text
id
kind
name
title
summary
database_name
schema_name
table_name
column_name
source
risk_level
confidence
sensitive
sensitivity
evidence_json
examples_json
suggested_next_json
details_json
edges_json
query_json
input_schema_json
output_schema_json
safety_json
enabled
capability_kind
graphql_query
graphql_mutation
created_at
updated_at
search_rank
```

Agents do not need to memorize many discovery APIs. They learn one pattern:

```graphql
query {
  gj_catalog(
    search: "users email references"
    where: { kind: { eq: "column" }, table_name: { eq: "users" } }
    order_by: { search_rank: desc }
    limit: 20
  ) {
    id
    kind
    name
    title
    summary
    database_name
    schema_name
    table_name
    column_name
    evidence_json
    examples_json
    details_json
    edges_json
    safety_json
    suggested_next_json
    search_rank
  }
}
```

Use `search` for ranked intent matching and `where` for exact constraints.
Use `kind` to keep the result shape predictable.

Core catalog kinds include:

| Kind | Meaning |
| :--- | :--- |
| `database` | Configured database or source boundary. |
| `table` | Table-like object from SQL, MongoDB, OpenAPI/remote API, filesystem/object source, nanoDB, or another configured source. |
| `column` | Field on a table-like object, with type, key, index, sensitivity, and evidence facts. |
| `relationship` | Traversable edge between graph objects, including join evidence. |
| `function` | Database function metadata when discovered. |
| `workflow` | Reusable workflow metadata. Full source is read through `gj_workflow`, not inlined into catalog cards. |
| `directive` | GraphJin directives such as `@object`, `@through`, `@running`, and `@rank`. |
| `operator_set` | Filter, expression, comparison, JSON, array, spatial, and text operators. |
| `query_pattern` | Query idioms such as grouped summaries, expression aggregates, pagination, and analytics rows. |
| `mutation_pattern` | Mutation idioms such as insert, update, upsert, delete, nested writes, and CodeSQL edit flows. |
| `deprecated_feature` | Features or syntax models should avoid, with replacement guidance. |
| `config` | Redacted configuration and policy shape. |
| `entrypoint` | Curated starting point for broad or ambiguous tasks. |
| `capability` | Runtime or MCP capability with safety and I/O shape. |
| `system_capability` | GraphJin-owned behavior exposed as guidance, such as `gj_security.query`. |

## MCP Discovery And Usage

MCP is the model-facing control surface for an agentic GraphJin deployment. It
does not replace the graph. It teaches the model how to enter and use the graph
efficiently.

Agents use MCP for:

- catalog discovery helpers
- catalog overview, entrypoint, and capability resources
- prompt helpers for writing or repairing GraphJin queries and mutations
- validation tools such as where-clause checks
- schema, config, workflow, and query-repair actions
- structured `next` guidance returned by tools

The important split is:

| Path | Use it for |
| :--- | :--- |
| Direct GraphQL | Composed queries over `gj_catalog`, application roots, `gj_security`, `gj_code`, workflows, and config. |
| MCP catalog helpers | Fast model ergonomics over `gj_catalog`: discover entrypoints, search catalog rows, inspect one catalog card, and list capabilities. |
| MCP validation/repair/action tools | Operations that benefit from structured tool contracts, previews, repair hints, or multi-step safety checks. |

Catalog helper tools are conveniences over catalog queries:

| Tool | Agent use |
| :--- | :--- |
| `get_catalog_entrypoints` | Start broad discovery without guessing which `kind` to query first. |
| `query_catalog` | Search and filter `gj_catalog` with a compact tool call. |
| `get_catalog_card` | Inspect one returned item with details, examples, evidence, safety notes, and graph edges. |
| `get_catalog_capabilities` | Learn available GraphJin/MCP capabilities and their safety notes. |

The MCP loop for an end-user request is:

```text
user intent
  -> get_catalog_entrypoints when the domain is broad or ambiguous
  -> query_catalog or direct gj_catalog query for candidate nouns and patterns
  -> get_catalog_card for evidence, examples, safety, and nearby edges
  -> gj_security for policy rows and findings before write-capable actions
  -> validation, preview, query repair, workflow, config, or schema MCP tools
  -> direct GraphQL against application/system roots
  -> observe result and follow tool `next` guidance back into the graph
```

Models should treat MCP responses as guidance and the graph as evidence. A tool
may recommend a next step, but the agent still grounds table names, column
names, policy posture, workflow inputs, and code paths in GraphQL-visible rows.

## Discovery Recipes

### Start From Entrypoints

Entrypoints are catalog rows that tell a model how to begin broad discovery
without guessing.

```graphql
query {
  gj_catalog(where: { kind: { eq: "entrypoint" } }, order_by: { name: asc }) {
    id
    name
    summary
    query_json
    suggested_next_json
  }
}
```

### Find Data Sources

Existing SQL databases, MongoDB, OpenAPI/remote API surfaces, and filesystem or
object-store tables appear as source-backed graph roots. The catalog presents
them as databases, tables, columns, and relationships so the agent can plan
against live shape instead of knowing the adapter type in advance.

```graphql
query {
  gj_catalog(
    search: "customer account subscription"
    where: { kind: { in: ["database", "table"] } }
    order_by: { search_rank: desc }
    limit: 20
  ) {
    id
    kind
    name
    title
    summary
    database_name
    schema_name
    table_name
    source
    evidence_json
    details_json
    search_rank
  }
}
```

### Inspect Columns And Safety

Column catalog items give the model type and sensitivity facts before it writes
filters, selects fields, or returns data to a user.

```graphql
query {
  gj_catalog(
    where: {
      kind: { eq: "column" }
      table_name: { eq: "orders" }
    }
    order_by: { column_name: asc }
  ) {
    id
    name
    table_name
    column_name
    summary
    sensitive
    sensitivity
    risk_level
    evidence_json
    examples_json
    safety_json
  }
}
```

### Verify Relationships Before Nesting

Relationships are catalog items. Agents should use them before nesting related
selectors or filtering through another table.

```graphql
query {
  gj_catalog(
    search: "orders customers join"
    where: { kind: { eq: "relationship" } }
    order_by: { search_rank: desc }
    limit: 10
  ) {
    id
    name
    summary
    database_name
    table_name
    column_name
    evidence_json
    edges_json
    search_rank
  }
}
```

### Learn GraphJin Syntax In Context

GraphJin GraphQL is its own DSL. Models should discover syntax from the same
catalog they use for schema facts.

```graphql
query {
  gj_catalog(
    search: "running totals expression aggregate upsert nested insert"
    where: {
      kind: {
        in: ["directive", "operator_set", "query_pattern", "mutation_pattern"]
      }
    }
    order_by: { search_rank: desc }
    limit: 20
  ) {
    id
    kind
    name
    summary
    examples_json
    details_json
    safety_json
    search_rank
  }
}
```

## `gj_security`: Policy Before Action

`gj_security` is a read-only security report over GraphJin's effective posture.
Summary, policy, and finding rows all live under the same `gj_security` root.
It is not the enforcement point and it is not mutated directly. It gives agents
evidence before they request config, workflow, schema, filesystem, CodeSQL, or
other write-capable actions.

`gj_security.kind` values are:

| Kind | Meaning |
| :--- | :--- |
| `summary` | One row named `summary` with active mode, production state, policy count, finding count, severity counts, and generated time in `summary_json`. |
| `policy` | One row per guarded capability/action. It compares the secure default for the active mode with the actual configured behavior. |
| `finding` | A generated warning when current configuration weakens the secure default, or when agentic mode is missing production protections. |

Effective policy means "what GraphJin will currently allow for this capability."
Each policy row includes:

| Field | Meaning |
| :--- | :--- |
| `capability` | The governed surface, such as `config`, `workflow`, `schema`, `codesql`, `filesystem`, or `dynamic_graphql`. |
| `action` | The kind of operation, such as `read`, `write`, `execute`, `query`, or `reload`. |
| `default_effective` | The secure default for the active mode: `allow`, `block`, `read_only`, or `read_write`. |
| `effective` | The actual current behavior after config and source/table permissions are applied. |
| `weakens_default` | True when current config is more permissive than the secure default. |
| `read_only` | True when the effective behavior is `read_only`. |
| `override_key` | The config knob or permission that changed the behavior. |
| `recommendation` | What a human or trusted agent should do if the posture is too open. |
| `evidence_json` | The facts GraphJin used to produce the row. |

Findings are not a second API. They are `gj_security` rows where
`kind: "finding"`. GraphJin creates them from policy rows that weaken the secure
default, plus agentic-mode safety checks. Examples:

- `mcp.allow_config_updates: true` can create a high finding because config
  writes changed from `block` to `allow`.
- A CodeSQL or filesystem source with `read_only: false` can create a high
  finding because source writes changed from `read_only` to `read_write`.
- `security_mode: agentic` without production protections can create a critical
  finding because agentic deployments are expected to run with production data
  protections.

A high config-write finding can look like this:

```json
{
  "kind": "finding",
  "severity": "high",
  "capability": "config",
  "action": "write",
  "default_effective": "block",
  "effective": "allow",
  "weakens_default": true,
  "override_key": "mcp.allow_config_updates",
  "recommendation": "Only enable config updates in trusted sessions."
}
```

GraphJin enforces through the actual guarded surfaces: production security,
allow-lists, source/table `read_only`, and MCP/config permissions. `gj_security`
reports posture so the agent can plan safely; changing enforcement still happens
through config, source permissions, workflow permissions, schema permissions, or
CodeSQL/filesystem permissions.

Agents should check high and critical findings before acting:

```graphql
query {
  summary: gj_security(id: "summary") {
    id
    kind
    mode
    title
    summary
    summary_json
  }

  findings: gj_security(
    where: {
      kind: { eq: "finding" }
      severity: { in: ["high", "critical"] }
    }
    order_by: { severity_rank: desc }
  ) {
    id
    severity
    title
    recommendation
    evidence_json
  }

  policy: gj_security(where: { kind: { eq: "policy" } }) {
    id
    mode
    source
    source_kind
    capability
    action
    default_effective
    effective
    weakens_default
    read_only
    override_key
    recommendation
    evidence_json
  }
}
```

A model should interpret that result as:

```text
no high/critical findings for the intended capability
  -> continue with validation or preview

high/critical finding exists
  -> prefer a read-only path, ask for review, or explain which config/source
     permission must change before acting

policy.effective is read_only or block
  -> do not attempt a write; choose a read-only query or report that the action
     is blocked by policy
```

`gj_catalog` also advertises security guidance as a system capability:

```graphql
query {
  gj_catalog(
    where: {
      kind: { eq: "system_capability" }
      name: { eq: "gj_security.query" }
    }
    limit: 1
  ) {
    name
    summary
    graphql_query
    details_json
    examples_json
    safety_json
  }
}
```

If policy needs to change, the agent changes config through guarded `gj_config`
updates when the deployment permits it, or asks a human to update source config.
It does not mutate `gj_security`.

## `gj_code`: Data To Source Code

`gj_code` is the public CodeSQL projection. It lets an agent move from business
data to the code that reads, writes, validates, transforms, or exposes that
data.

Common `gj_code.kind` values include:

```text
file
symbol
reference
import
edge
db_reference
injection
doc
text_chunk
parse_error
ast_node
capture
index_status
change_set
lock
```

CodeSQL records database references with table and column identity. When catalog
code relations are configured, catalog table/column identity can join to
`gj_code.db_object_id`. Even without relying on a join, the agent can use the
catalog's `database_name`, `schema_name`, `table_name`, and `column_name` to
query code references directly:

```graphql
query {
  gj_code(
    search: "users email"
    where: {
      kind: { eq: "db_reference" }
      table_name: { eq: "users" }
      column_name: { eq: "email" }
    }
    order_by: { search_rank: desc }
    limit: 20
  ) {
    id
    kind
    name
    path
    language
    db_object_id
    database_name
    schema_name
    table_name
    column_name
    ref_kind
    start_row
    start_col
    end_row
    end_col
    search_rank
  }
}
```

For source edits, the model must read before writing:

```graphql
query {
  gj_code(
    where: { kind: { eq: "symbol" }, name: { eq: "LoadUser" } }
    limit: 5
  ) {
    id
    kind
    name
    path
    hash
    start_byte
    end_byte
    code
    code_context
  }
}
```

Then it previews a guarded change set before applying:

```graphql
mutation {
  gj_code(insert: {
    kind: "change_set"
    action: "preview"
    title: "Update user email validation"
    edits: [{
      op: "replace"
      path: "users/handler.go"
      expected_hash: "current-file-hash"
      replacements: [{
        start_byte: 120
        end_byte: 156
        old_text: "old code"
        new_text: "new code"
      }]
    }]
  }) {
    id
    kind
    status
    diff
    errors_json
  }
}
```

Apply only after the preview diff is correct:

```graphql
mutation {
  gj_code(
    id: "change_set:123"
    update: { kind: "change_set", id: 123, action: "apply" }
  ) {
    id
    kind
    status
    files_changed
    files_reindexed
    errors_json
  }
}
```

Long-running edit sessions can use `gj_code` rows with `kind: "lock"` to acquire
leases for ranges or whole-file reservations. Short preview/apply flows rely on
the change-set checks: hashes, exact ranges, and `old_text`.

## Workflows And Config

Workflows are discovered through `gj_catalog`, read through `gj_workflow`, and
executed through `gj_workflow_execution`.

```graphql
query {
  gj_catalog(
    search: "billing reconciliation workflow"
    where: { kind: { eq: "workflow" } }
    order_by: { search_rank: desc }
  ) {
    id
    name
    summary
    details_json
    evidence_json
    suggested_next_json
    search_rank
  }
}
```

```graphql
query {
  gj_workflow(where: { name: { eq: "billing_reconcile" } }) {
    name
    description
    tags_json
    variables_json
    path
    source_hash
    runtime
    timeout_seconds
    catalog_item_id
    catalog_revision
  }
}
```

Execution is a mutation because it performs work, but the returned row is
ephemeral:

```graphql
mutation {
  gj_workflow_execution(insert: {
    workflow_name: "billing_reconcile"
    variables: { account_id: 42 }
  }) {
    id
    workflow_name
    status
    result_json
    error
    duration_ms
  }
}
```

Config is similar: discover the shape and policy through `gj_catalog` and
`gj_security`, read redacted state through `gj_config`, then use guarded
`gj_config` mutations only when the deployment permits config writes.

```graphql
query {
  gj_config(id: "current") {
    id
    source_mode
    active_database
    sources
    databases
    mcp
    redacted_paths
    catalog_revision
  }
}
```

## Sources In The Agentic Story

Agentic GraphJin does not make agents switch mental models for each source.
Different systems keep their native ownership, but the agent reaches them
through the same graph language.

| Source type | Agent-facing behavior |
| :--- | :--- |
| SQL databases | GraphJin compiles GraphQL into dialect-specific SQL. Schema metadata feeds `gj_catalog`. |
| MongoDB | GraphJin emits a JSON DSL that becomes aggregation pipelines. Collections participate as graph roots. |
| OpenAPI sources | Configured OpenAPI specs become table-like graph surfaces for external operations. Catalog discovery gives the model names, shape, and source boundary. |
| Remote API resolvers | Remote API calls attach to graph fields while preserving that the data comes from an external service. |
| Filesystem/object sources | Local directories, S3, GCS, and similar backends expose key/size/content-type/etag/modified/url/data-shaped virtual tables. |
| CodeSQL sources | Source trees become queryable code databases, projected to agents through `gj_code`. |
| GraphJin system source | The control-plane graph exposes catalog, security, workflows, config, and compact system state. |

This is why `gj_catalog` is the first stop. It tells the agent what kind of
surface it is touching before the agent chooses a query, mutation, workflow, or
edit path.

## Model-Facing Prompt Contract

The prompt that teaches models to use GraphJin is not one giant static string.
It is a layered contract:

- MCP server instructions define the catalog-first operating loop.
- Catalog overview resources expose entrypoints, capabilities, and guidance.
- Tool descriptions and output schemas explain how to call catalog, validation,
  repair, schema, workflow, and config tools.
- `next` guidance in tool responses gives machine-readable follow-up actions.
- Prompt helpers such as `write_query`, `write_where_clause`, `write_mutation`,
  and `fix_query_error` generate schema-aware guidance when the client supports
  prompts.
- `gj_catalog` itself contains language, operator, pattern, capability, and
  safety items so the model can learn the local DSL from the graph.

The stable model instructions are:

1. Use `gj_catalog` first. Search for intent and filter by `kind`.
2. Inspect `details_json`, `evidence_json`, `examples_json`, `safety_json`, and
   `edges_json` before selecting tables, columns, relationships, operators,
   workflows, actions, or code paths.
3. Resolve ambiguity by comparing catalog items. Do not guess from names alone.
4. Check `gj_security` before config, workflow, schema, filesystem, CodeSQL, or
   other write-capable actions.
5. Validate filters with the validation surface when column types, operators, or
   real values matter.
6. Prefer workflows for broad or repeatable data work after discovery.
7. Use `gj_code` for source intelligence and preview/apply flows. Do not mutate
   raw CodeSQL internals directly.
8. Use `gj_config`, `gj_workflow`, and `gj_workflow_execution` for governed
   control-plane actions when policy allows them.
9. Observe results and errors, then return to catalog/security/code when the
   facts needed for the next step change.

## End-To-End Agent Loops

### Answer A Data Question

```text
intent
  -> gj_catalog: find candidate tables, columns, relationships, query patterns
  -> gj_security: check policy rows and findings if the path is broad or sensitive
  -> validate filters and limits
  -> query application roots or run a workflow
  -> observe rows/result
  -> return to gj_catalog if follow-up changes the entity, relationship, or syntax
```

### Trace Data To Code

```text
intent
  -> gj_catalog: identify the table or column
  -> gj_code: find db_reference rows for that identity
  -> gj_code: follow file, symbol, reference, and import rows
  -> gj_security: confirm CodeSQL/filesystem write policy before edit-capable actions
  -> gj_code: read code/code_context plus hash
  -> gj_code: preview change_set
  -> gj_code: apply only after preview review
  -> re-read gj_code and gj_catalog revision if needed
```

### Create Or Run A Workflow

```text
intent
  -> gj_catalog: discover existing workflow items and workflow patterns
  -> gj_security: confirm workflow execute/write policy and review findings
  -> gj_workflow: inspect reusable workflow metadata or source
  -> gj_workflow_execution: execute when appropriate
  -> gj_workflow: create/update/delete only when policy allows it and after previewing intent
  -> observe result_json/error/duration_ms
```

### Change Configuration

```text
intent
  -> gj_catalog: inspect config and capability items
  -> gj_security: inspect config policy rows and high/critical findings
  -> gj_config: read redacted current state
  -> gj_config: update only when policy allows it
  -> observe catalog_revision and refresh discovery
```

## Design Principles

### Evidence Before Action

Every meaningful action should be grounded in graph evidence: catalog item,
relationship edge, security policy, code reference, workflow metadata, config
fact, validation result, preview diff, or execution result.

### Query By Kind

`kind` is the model's steering wheel. It keeps broad roots understandable:

```graphql
gj_catalog(where: { kind: { eq: "table" } }) { ... }
gj_catalog(where: { kind: { eq: "relationship" } }) { ... }
gj_security(where: { kind: { eq: "finding" } }) { ... }
gj_code(where: { kind: { eq: "db_reference" } }) { ... }
```

### Preserve Source Boundaries

One graph does not mean one storage system. Databases, APIs, files, code, config,
security posture, and workflows each keep their ownership model. GraphJin makes
the boundaries visible and traversable.

### Prefer Governed Verbs

Use saved queries, workflows, guarded config mutations, validation tools, query
repair, CodeSQL previews, and security checks. The point is not to make agents
powerless; it is to make useful actions inspectable and auditable.

### Refresh From The Graph

Catalog snapshots, security rows, workflow registries, CodeSQL indexes, and
source schemas can change. Agents should treat `catalog_revision`, source
hashes, workflow revisions, and observed errors as signals to re-read the graph.

## What Success Looks Like

An agent using GraphJin should be able to answer these questions through one
composable surface:

- What data, APIs, files, code, workflows, config, and capabilities exist?
- Which source owns this fact?
- Which fields are safe and useful?
- How do these entities relate?
- Which security findings or policy rows matter before action?
- Which code paths touch this table or column?
- Which workflow already does this?
- What GraphJin syntax should I use here?
- What is the smallest safe action I can take next?

That is the point of Agentic GraphJin: the model does not merely receive a
prompt. It enters a governed graph, discovers evidence, acts through explicit
boundaries, and keeps humans and agents on the same map.
