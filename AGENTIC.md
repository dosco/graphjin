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
start with the user's instruction
  -> search gj_catalog for that intent
  -> inspect evidence
  -> check gj_security and gj_runtime before risky actions
  -> join into application data, gj_code, workflows, or config
  -> validate or preview
  -> act through the governed GraphJin surface
  -> observe and refresh discovery
```

The important part is that the agent learns the company system from the graph
itself, not from memory, pasted schema, or guessed conventions. A model should
not need to know root names like `gj_config` or `gj_security` before it begins.
Its first reliable move is an intent search such as
`query_catalog(search: "<user instruction>")`; catalog rows then teach the exact
roots, capabilities, safety checks, and next actions.

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
search by intent, then inspect the best row by id.

With MCP, the cold-start call is:

```json
query_catalog({
  "search": "add admin role account security config",
  "limit": 10
})
```

When the agent has direct GraphQL access, the same search is:

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

- a compact bootstrap prompt in `mcpServerInstructions`
- `graphql_help` topic routing into catalog-backed help rows
- `query_catalog` discovery and `query_catalog(id: "...")` detail lookups
- validation tools such as where-clause checks
- approved saved-query execution
- structured `next` guidance returned by tools

The important split is:

| Path | Use it for |
| :--- | :--- |
| Direct GraphQL | Composed queries over `gj_catalog`, application roots, `gj_security`, `gj_code`, workflows, and config. |
| MCP bootstrap helpers | Teach a fresh model the catalog-first loop: `query_catalog`, `graphql_help`, `validate_where_clause`, and `execute_saved_query`. |
| Catalog help rows | Durable replacement for old discovery tool descriptions: syntax, schema, relationships, workflows, config, security, code, fragments, saved queries, and errors. |
| Control-plane GraphQL | Governed workflow/config/security/code actions through normal GraphJin roots when policy allows them. |

Sources-mode MCP intentionally keeps the tool list small:

| Tool | Agent use |
| :--- | :--- |
| `query_catalog` | Goal-driven search/filter over `gj_catalog`, or one detailed row with `query_catalog(id: "...")`. |
| `graphql_help` | Topic router and fallback. It returns bootstrap steps, topic routes, old-to-new tool replacements, catalog rows, examples, safety notes, next guidance, and the exact internal `gj_catalog` query it used. |
| `validate_where_clause` | Validate filters against discovered table and column metadata. |
| `execute_saved_query` | Run an approved saved query after inspecting its `saved_query` catalog row. |

The MCP loop for an end-user request is:

```text
user intent
  -> query_catalog(search: "<user intent>") for candidate nouns, patterns, help, and capabilities
  -> graphql_help(for: "discovery") only when the path or topic route is unclear
  -> graphql_help(for: "<topic>") when a returned catalog row points to topic help
  -> query_catalog(id: "...") for evidence, examples, safety, and nearby edges
  -> gj_security for policy rows and findings before write-capable actions
  -> gj_runtime for recent decision-support events before config/workflow/schema actions
  -> validation, preview, workflow, config, or schema GraphQL/control-plane action
  -> direct GraphQL against application/system roots
  -> observe result and follow tool `next` guidance back into the graph
```

Models should treat MCP responses as guidance and the graph as evidence. A tool
may recommend a next step, but the agent still grounds table names, column
names, policy posture, workflow inputs, and code paths in GraphQL-visible rows.

### Bootstrap Prompt Chain

The old MCP surface taught models by placing many specialized tool
descriptions into the prompt. Sources mode compresses that prompt mass into one
bootstrap chain:

```text
mcpServerInstructions
  -> query_catalog(search: "<user instruction>")
  -> catalog rows with details_json / examples_json / safety_json / suggested_next_json
  -> query_catalog(id: "<best row>")
  -> optional graphql_help(for: "discovery" | "<topic>") when routing is unclear
  -> query_catalog(id: "help:<topic>")
  -> direct gj_catalog, gj_security, gj_code, workflow, config, or app-data query
```

The model only needs one memorized first move for goal-driven work:
`query_catalog(search: "<the user's instruction>")`. `graphql_help` is still the
topic router and fallback when the model does not know how to shape the next
catalog query. The larger knowledge surface lives in catalog help rows, not in a
long list of MCP tool descriptions.

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
| `finding` | A generated warning when current configuration weakens the secure default. |

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
- `mode: agentic` applies production-oriented source and control-plane
  defaults; weakening production protections with options like
  `disable_production_security: true` can create a critical finding.

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
    sources_used
    active_database
    sources
    databases
    mcp
    redacted_paths
    catalog_revision
  }
}
```

## Config And Security Change Playbook

Config changes are privileged actions. An agent must never invent a YAML shape
from memory, paste secrets, or bypass GraphJin policy. It should discover the
local config surface, inspect policy, read redacted state, apply the smallest
allowed update, and verify the new posture.

### 1. Discover The Admin Intent

For goal-driven config work, start with the user's actual words:

```json
query_catalog({
  "search": "add support role account scoped access config security",
  "limit": 10
})
```

The useful rows are usually `help`, `config`, `system_capability`,
`capability`, `table`, `database`, `source`, and future `config_recipe` rows.
Inspect the best row before acting:

```json
query_catalog({ "id": "help:config" })
```

If the result points to a capability, inspect that too:

```json
query_catalog({ "id": "capability.gj_config.update" })
```

### 2. Check Policy And Runtime First

Before config, workflow, schema, file, or code-source writes, inspect security
and recent runtime context. `gj_security` explains policy. `gj_runtime` explains
recent redacted operational outcomes and next actions. It is bounded decision
support, not forensic audit history.

```graphql
query {
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
    capability
    action
    source
    root
    recommendation
    evidence_json
  }

  runtime: gj_runtime(
    where: { kind: { in: ["status", "event"] } }
    order_by: { created_at: desc }
    limit: 20
  ) {
    kind
    phase
    status
    severity
    source
    root
    reason
    next_action
    details_json
  }
}
```

If policy blocks the action, the correct agentic behavior is to report the
blocking policy and the smallest config permission that would need to change. Do
not keep trying mutations.

### 3. Read Redacted Config State

Read the current singleton only when the caller has permission:

```graphql
query {
  gj_config(id: "current") {
    id
    catalog_revision
    sources_used
    active_database
    sources
    roles
    mcp
    redacted_paths
    config_json
  }
}
```

`gj_config` is redacted state. It should not expose raw JWTs, connection strings,
or secrets. Agents should preserve unknown config sections unless the user
explicitly asked to change them.

### 4. Build The Smallest Patch

In source mode, table access belongs under `sources[].access`. Roles describe
identity and matching. They do not carry per-table filters or mutation presets.

| User intent | Configure | Do not configure |
| :--- | :--- | :--- |
| Map JWT claims | `identity.user_id_claim`, `identity.role_claims`, `identity.namespace_claim`, `identity.admin_roles` | Per-source JWT claim names |
| Add a role | `roles[].name`, `roles[].comment`, `roles[].match` | `roles[].tables` |
| Account-scope a database | `sources[].access.read/write/delete`, `namespace_column` | Repeated `roles[].tables.query.filters` |
| Shared read-only reference data | `public_tables` | `read: public` on the whole source unless intended |
| Admin-only data | `admin_tables` or root access `admin` | Hidden ad hoc table filters |
| Fully blocked data | `blocked_tables` | Returning empty rows as a fake block |
| System root access | `sources[].kind: graphjin.access.roots` | Source-specific JWT interpretation |
| Mutable account artifacts | `artifacts` config and `gj_artifacts` root | alternate artifact-store keys or config-folder mutation |

Typical source-mode security shape:

```yaml
identity:
  user_id_claim: sub
  role_claims: [role, roles]
  namespace_claim: account_id
  admin_roles: [admin]

sources:
  - name: app
    kind: database
    access:
      read: account
      write: blocked
      delete: blocked
      namespace_column: account_id
      missing_namespace_column: block
      public_tables: [countries, currencies, plans]
      admin_tables: [audit_logs]
      blocked_tables: [internal_events]

  - name: graphjin
    kind: graphjin
    access:
      roots:
        gj_catalog: authenticated
        gj_artifacts: account
        gj_workflow: admin
        gj_workflow_execution: account
        gj_runtime: admin
        gj_security: admin
        gj_config: admin
```

For V1, `identity.query` is the source-mode spelling for the existing
`roles_query` enrichment path. It is not arbitrary enrichment for `user_id`,
`account_id`, or trusted variables. Agents should not configure both names with
different values.

Adding a JWT-backed role in source mode should look like role identity metadata:

```yaml
roles:
  - name: support
    comment: Support staff
    match: "role = 'support'"
```

It should not reintroduce legacy table rules:

```yaml
# Rejected in source mode.
roles:
  - name: user
    tables:
      orders:
        query:
          filters:
            - "{ account_id: { eq: $account_id } }"
```

Mutable user or account artifacts use `artifacts` and the `gj_artifacts` root:

```yaml
artifacts:
  enabled: true
  source: app
  schema: _graphjin
  auto_init: true
  globals_path: ./config
```

Config-folder fragments, saved queries, and workflows remain global,
read-only artifacts. Database-backed artifacts are account-scoped by default and
can override same-name globals without changing config files.

### 5. Apply And Verify

When config writes are enabled and policy allows them, apply the smallest update
through the singleton. For list fields such as `roles` and `sources`, preserve
existing entries unless the user explicitly asked to remove them:

```graphql
mutation {
  gj_config(
    id: "current"
    update: {
      roles: [
        {
          name: "support"
          comment: "Support staff"
          match: "role = 'support'"
        }
      ]
    }
  ) {
    id
    catalog_revision
    updated_at
  }
}
```

`gj_config` mutation requires both authorization to the `gj_config` root and the
deployment's config-write gate, such as `mcp.allow_config_updates`. If either is
blocked, the agent should stop with the policy evidence instead of trying a
different route.

After the mutation, re-read `gj_security`, check recent `gj_runtime` events, and
compare `catalog_revision`. If the revision did not change or a runtime event
reports a failed config reload, stop and report the exact reason.

### Current And Target Agent Affordances

Current behavior gives models enough to operate safely:

- `query_catalog(search: "<intent>")` for goal-driven discovery.
- `graphql_help(for: "...")` for topic routing and exact catalog query shapes.
- `gj_security` for effective policy and findings.
- `gj_runtime` for bounded, redacted decision support.
- `gj_config(id: "current")` for redacted state and guarded updates.
- Current `gj_config.update` support is limited to implemented update fields
  such as `sources`, `roles`, `relationships`, `databases`, `tables`,
  `blocklist`, `functions`, and `mcp`.

The next hardening layer should make config changes even more explicit:

- `config_recipe` catalog rows for add-role, identity claims, source access,
  table classifications, GraphJin roots, artifacts, and legacy migration.
- Recipe fields such as `required_capabilities`, `preflight`,
  `mutation_template`, `postflight`, `warnings`, and `rollback_hint`.
- Machine-actionable errors with `next_action`, for example pointing
  `roles[].tables` failures to the legacy migration recipe.
- A config validation or dry-run path before live `gj_config` mutation.

Until those recipe rows and dry-run semantics exist everywhere, docs and catalog
help should describe them as target hardening, not as guaranteed runtime rows.

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

- MCP server instructions define the catalog-first operating loop and the
  exact sources-mode tool chain.
- `query_catalog` is the goal-driven entrypoint. It searches with the user's
  instruction and returns catalog rows with details, examples, safety, and next
  guidance.
- The `graphql_help` tool description is the topic router and fallback. It lists
  valid topics and tells the model where old discovery surfaces went.
- `graphql_help` responses expose `bootstrap`, `topic_routes`,
  `replaces_tools`, examples, safety notes, next guidance, and the exact
  `gj_catalog` GraphQL query executed internally.
- `query_catalog(id: "...")` is the detailed lookup path, including
  `query_catalog(id: "help:<topic>")` when topic help is needed.
- `next` guidance in tool responses gives machine-readable follow-up actions.
- `gj_catalog` itself contains language, operator, pattern, capability, and
  safety items so the model can learn the local DSL from the graph.
- `errors[].extensions.graphjin_repair` carries deterministic repair guidance
  for every client instead of requiring a separate repair MCP tool.

The stable model instructions are:

1. Start goal-driven work with `query_catalog(search: "<user instruction>")`.
2. Call `graphql_help(for: "discovery")` only when the route or query shape is
   unclear.
3. Use topic routes to choose the right help topic, then inspect
   `query_catalog(id: "help:<topic>")` when needed.
4. Use `gj_catalog` for evidence. Search for intent and filter by `kind`.
5. Inspect `details_json`, `evidence_json`, `examples_json`, `safety_json`, and
   `edges_json` before selecting tables, columns, relationships, operators,
   workflows, actions, or code paths.
6. Resolve ambiguity by comparing catalog items. Do not guess from names alone.
7. Check `gj_security` before config, workflow, schema, filesystem, CodeSQL, or
   other write-capable actions.
8. Check `gj_runtime` before config, workflow, or schema actions and after
   GraphJin errors.
9. Validate filters with the validation surface when column types, operators, or
   real values matter.
10. Prefer workflows for broad or repeatable data work after discovery.
11. Use `gj_code` for source intelligence and preview/apply flows. Do not mutate
   raw CodeSQL internals directly.
12. Use `gj_config`, `gj_workflow`, and `gj_workflow_execution` for governed
   control-plane actions when policy allows them.
13. Observe results and errors, then return to catalog/security/runtime/code when the
   facts needed for the next step change.

## End-To-End Agent Loops

### Answer A Data Question

```text
intent
  -> query_catalog(search: "<data question>")
  -> gj_catalog: find candidate tables, columns, relationships, query patterns
  -> gj_security: check policy rows and findings if the path is broad or sensitive
  -> validate filters and limits
  -> query application roots or run a workflow
  -> observe rows/result
  -> return to gj_catalog if follow-up changes the entity, relationship, or syntax
```

### Cold Start Example: "How Many Sales Did We Have Last Week?"

Assume a model knows nothing about GraphJin except the sources-mode MCP tools
in its prompt:

```text
query_catalog
graphql_help
validate_where_clause
execute_saved_query
```

The model should not guess an `orders` table, a date column, or whether "sales"
means paid orders, completed transactions, or revenue. It should bootstrap from
the catalog with the user's intent:

```json
query_catalog({
  "search": "how many sales did we have last week",
  "limit": 10
})
```

If the returned rows are too broad, route to topic help:

```json
graphql_help({ "for": "discovery" })
graphql_help({ "for": "saved_queries" })
graphql_help({ "for": "tables" })
graphql_help({ "for": "query" })
```

Because sources-mode MCP does not expose arbitrary GraphQL execution by
default, the model should search for an approved saved query first:

```json
query_catalog({
  "search": "sales count last week orders transactions revenue",
  "where": { "kind": { "eq": "saved_query" } },
  "limit": 10
})
```

If a matching saved query exists, inspect its contract:

```json
query_catalog({ "id": "saved_query:sales_count_by_period" })
```

Then execute it with the resolved date range. For example, if "today" is
`2026-05-16` in `America/Vancouver`, the previous completed calendar week is:

```text
start: 2026-05-04T00:00:00-07:00
end:   2026-05-11T00:00:00-07:00
```

```json
execute_saved_query({
  "name": "sales_count_by_period",
  "variables": {
    "start": "2026-05-04T00:00:00-07:00",
    "end": "2026-05-11T00:00:00-07:00"
  }
})
```

If no saved query exists, discover the real schema and query pattern:

```json
query_catalog({
  "search": "sales orders transactions purchases completed paid",
  "where": {
    "kind": { "in": ["table", "column", "relationship", "query_pattern"] }
  },
  "limit": 20
})
```

Inspect likely table and column rows with `query_catalog(id: "...")`, then
validate the candidate filter:

```json
validate_where_clause({
  "table": "orders",
  "where": {
    "created_at": {
      "gte": "2026-05-04T00:00:00-07:00",
      "lt": "2026-05-11T00:00:00-07:00"
    },
    "status": { "in": ["paid", "completed"] }
  }
})
```

At that point, if the deployment also gives the client direct GraphQL access,
the discovered query might be:

```graphql
query {
  orders(
    where: {
      created_at: {
        gte: "2026-05-04T00:00:00-07:00"
        lt: "2026-05-11T00:00:00-07:00"
      }
      status: { in: ["paid", "completed"] }
    }
  ) {
    count_id
  }
}
```

If direct GraphQL is not available and no approved saved query or workflow
exists, the correct agentic behavior is to stop with evidence: report the
validated query shape, explain that execution requires an approved saved query,
workflow, or direct GraphQL access, and avoid fabricating the count.

### Cold Start Example: Data Plus Code Provenance

Now take a broader question:

```text
How many paid sales did we have last week, and where in the code is the sale
status set?
```

The model still starts with an intent search:

```json
query_catalog({
  "search": "paid sales last week count code where status set",
  "limit": 10
})
```

If the route is unclear, it asks for discovery and then routes into both the
data and code sides:

```json
graphql_help({ "for": "discovery" })
graphql_help({ "for": "saved_queries" })
graphql_help({ "for": "tables" })
graphql_help({ "for": "columns" })
graphql_help({ "for": "code" })
```

For the data answer, it should prefer an approved saved query:

```json
query_catalog({
  "search": "sales paid orders transactions last week count",
  "where": { "kind": { "eq": "saved_query" } },
  "limit": 10
})
```

If it finds `saved_query:sales_count_by_period`, inspect and execute it:

```json
query_catalog({ "id": "saved_query:sales_count_by_period" })
```

```json
execute_saved_query({
  "name": "sales_count_by_period",
  "variables": {
    "start": "2026-05-04T00:00:00-07:00",
    "end": "2026-05-11T00:00:00-07:00",
    "status": ["paid", "completed"]
  }
})
```

Then it should discover the schema facts behind the answer:

```json
query_catalog({
  "search": "sales orders paid completed status",
  "where": {
    "kind": { "in": ["table", "column", "relationship"] }
  },
  "limit": 20
})
```

The model is looking for evidence such as:

```text
table: orders
column: orders.status
column: orders.paid_at
column: orders.created_at
relationship: orders -> payments
```

It should inspect exact catalog rows before claiming what the count means:

```json
query_catalog({ "id": "table:app.public.orders" })
query_catalog({ "id": "column:app.public.orders.status" })
query_catalog({ "id": "column:app.public.orders.paid_at" })
```

For the code side, first inspect the catalog's code guidance:

```json
query_catalog({ "id": "help:code" })
```

Then search for code/source-related catalog evidence:

```json
query_catalog({
  "search": "orders status paid completed code db_reference payment",
  "where": {
    "kind": { "in": ["table", "column", "relationship", "system_capability"] }
  },
  "limit": 20
})
```

If direct GraphQL access to `gj_code` is available and policy permits it, the
natural provenance query is:

```graphql
query {
  gj_code(
    search: "orders status paid completed"
    where: {
      kind: { eq: "db_reference" }
      table_name: { eq: "orders" }
      column_name: { eq: "status" }
    }
    order_by: { search_rank: desc }
    limit: 20
  ) {
    id
    kind
    path
    symbol_name
    ref_kind
    table_name
    column_name
    start_row
    start_col
    code_context
    search_rank
  }
}
```

Then inspect likely files or symbols:

```graphql
query {
  gj_code(
    where: {
      kind: { in: ["symbol", "reference"] }
      path: { eq: "services/payments/settlement.go" }
    }
    limit: 20
  ) {
    id
    kind
    name
    path
    start_row
    end_row
    code_context
  }
}
```

The final answer should be evidence-shaped:

```text
Paid sales last week: 1,284

Count source:
- saved_query:sales_count_by_period
- table: orders
- filter: paid_at >= 2026-05-04T00:00:00-07:00 and < 2026-05-11T00:00:00-07:00
- status in ["paid", "completed"]

Code provenance:
- orders.status is written in services/payments/settlement.go, symbol markOrderPaid
- orders.paid_at is set in the same transaction after payment capture succeeds
- refunds appear to update orders.status in services/refunds/refund.go, so the count excludes refunded/cancelled statuses
```

If the model only has MCP and no direct GraphQL access to `gj_code`, it should
not pretend to have inspected code. It can run the approved sales count, report
the catalog/code capability it found, and explain that proving code references
requires direct `gj_code` access or an approved workflow/saved query.

### Trace Data To Code

```text
intent
  -> query_catalog(search: "<table, column, or business concept>")
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
  -> query_catalog(search: "<workflow intent>")
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
  -> query_catalog(search: "<exact config/security instruction>")
  -> query_catalog(id: "<best config/help/capability row>")
  -> gj_security: inspect effective policy and high/critical findings
  -> gj_runtime: inspect recent config/auth/access failures and next_action
  -> gj_config: read redacted current state when permitted
  -> build the smallest update against source-mode fields
  -> gj_config: update only when policy allows it
  -> verify catalog_revision, gj_security, and gj_runtime
  -> refresh discovery before the next action
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

## Old MCP Discovery Surface In The Catalog World

The old MCP surface taught models by putting many discovery tool descriptions
directly into the prompt. Sources mode keeps the prompt small and moves that
knowledge into `mcpServerInstructions`, `graphql_help`, and `gj_catalog` help
rows.

| Old MCP surface | Catalog-world path |
| :--- | :--- |
| `get_catalog_entrypoints` | `graphql_help(for: "discovery")` or `gj_catalog(where: { kind: { eq: "entrypoint" } })` |
| `get_catalog_card` | `query_catalog(id: "...")` |
| `get_catalog_capabilities` | `query_catalog(where: { kind: { in: ["capability", "system_capability"] } })` |
| `get_query_syntax` | `graphql_help(for: "query")` and `query_catalog(id: "help:query")` |
| `get_mutation_syntax` | `graphql_help(for: "mutations")` and `query_catalog(id: "help:mutations")` |
| `get_discovery_schema` | `graphql_help(for: "catalog")` and `query_catalog(id: "help:catalog")` |
| `get_table_sample` | `graphql_help(for: "tables")`, `graphql_help(for: "columns")`, sample/profile catalog guidance, then permitted app-data queries or workflows |
| `get_workflow_guide` | `graphql_help(for: "workflows")` and workflow catalog rows |
| `get_schema_insights` | `graphql_help(for: "schema")` |
| `explore_relationships` / `find_path` | `graphql_help(for: "relationships")`, relationship rows, and `edges_json` |
| saved-query discovery tools | `graphql_help(for: "saved_queries")`, `saved_query` rows, then `execute_saved_query` |
| fragment discovery tools | `graphql_help(for: "fragments")` and `fragment` rows |
| `get_config_docs` | `graphql_help(for: "config")`, config catalog rows, and `gj_config` when permitted |
| `get_js_runtime_api` | `graphql_help(for: "workflow_runtime")` and workflow runtime catalog rows |
| `write_query` / `write_mutation` | `graphql_help(for: "query" | "mutations")`, catalog examples, then direct GraphQL or saved workflow/query |
| `fix_query_error` | `errors[].extensions.graphjin_repair` and `graphql_help(for: "errors")` |
| `execute_workflow` | `gj_workflow_execution(insert)` in GraphQL |

The principle is simple: old MCP tools carried knowledge in tool descriptions;
sources mode carries that knowledge in catalog rows, help rows, examples,
evidence, safety metadata, and the bootstrap prompt.
