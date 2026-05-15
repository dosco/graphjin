# Agentic GraphJin

GraphJin gives agents a governed GraphQL operating surface over an
organization's data, schema, code, config, workflows, and capabilities.

The ambition is simple: an agent should be able to enter a live software system
through one graph, discover what exists, understand how it connects, inspect the
code and workflows that operate it, and then act through the same audited
GraphJin language.

This document is the technical guide to that vision.

## The Shape Of The System

GraphJin is not a resolver framework. It is a compiler and execution layer that
turns GraphQL into database-native work. Agentic GraphJin keeps that philosophy:
system knowledge, application data, code intelligence, workflows, and config all
become graph-shaped data sources.

The agent-facing graph is organized around a few canonical roots:

| Root | Role |
| :--- | :--- |
| `gj_catalog` | The map of the system: schema, relationships, capabilities, syntax, workflows, config facts, and system guidance. |
| `gj_code` | Code intelligence over CodeSQL's durable indexes, including files, symbols, references, imports, database references, docs, parse state, change sets, and locks. |
| `gj_workflow` | Reusable workflow definitions and metadata. |
| `gj_workflow_execution` | Mutation-only workflow execution with an ephemeral result row; it does not store run history. |
| `gj_config` | Current GraphJin configuration, redacted facts, and guarded updates. |
| Application roots | The user's databases, APIs, filesystem objects, and other configured sources. |

The shape is intentionally small. Agents do better when they learn a few
powerful roots deeply instead of memorizing a pile of special-purpose entry
points.

## The Agent Loop

Every useful agent loop has the same rhythm:

```text
intent
  -> discover the relevant catalog items
  -> inspect evidence and examples
  -> join into code, workflows, or application data
  -> act through GraphJin
  -> observe the result
  -> refine from the graph
```

In GraphQL:

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
    type
    search_rank
    details_json
    evidence_json
    examples_json
    safety_json
    edges_json
  }
}
```

The catalog gives the agent grounded nouns. The rest of the graph gives it
verbs and context.

## Catalog Items

`gj_catalog` is the single discovery root. Every row is a catalog item. The
primary selector is `kind`.

Core catalog item kinds:

| Kind | Meaning |
| :--- | :--- |
| `database` | A configured application database or source boundary. |
| `table` | A table-like object, including database/schema/table identity and row-shape hints. |
| `column` | A field on a table-like object, including type, nullability, indexing, examples, and safety facts. |
| `relationship` | A traversable edge between graph objects, including join evidence and nearby graph structure. |
| `workflow` | A reusable workflow that can be discovered, inspected, and run. |
| `directive` | GraphJin language features such as `@object`, `@through`, `@running`, and `@rank`. |
| `operator_set` | Filter, expression, and comparison operators available to GraphJin queries. |
| `query_pattern` | Query idioms such as grouped summaries, recursive traversal, analytics rows, and expression aggregates. |
| `mutation_pattern` | Mutation idioms such as insert, update, upsert, delete, and nested writes. |
| `config` | Redacted configuration facts, policy shape, source state, and capability flags. |
| `entrypoint` | A curated starting point for broad or ambiguous tasks. |
| `capability` | A feature the runtime exposes, with safety and input/output shape. |
| `system_capability` | GraphJin-owned system behavior and constraints. |

Table and column discovery are just catalog queries:

```graphql
query {
  tables: gj_catalog(where: { kind: { eq: "table" } }) {
    id
    name
    database_name
    schema_name
    table_name
    summary
  }

  columns: gj_catalog(
    where: { kind: { eq: "column" }, table_name: { eq: "users" } }
    order_by: { column_name: asc }
  ) {
    id
    name
    column_name
    type
    summary
    safety_json
  }
}
```

Relationships are catalog items too:

```graphql
query {
  gj_catalog(
    search: "orders customers"
    where: { kind: { eq: "relationship" } }
    order_by: { search_rank: desc }
  ) {
    id
    name
    summary
    database_name
    table_name
    column_name
    evidence_json
    edges_json
  }
}
```

The goal is that a model learns one pattern and keeps reusing it:

```graphql
gj_catalog(where: { kind: { eq: "table" } }) { ... }
gj_catalog(where: { kind: { eq: "column" } }) { ... }
gj_catalog(where: { kind: { eq: "relationship" } }) { ... }
gj_catalog(where: { kind: { eq: "workflow" } }) { ... }
gj_catalog(where: { kind: { eq: "capability" } }) { ... }
```

## nanoDB

nanoDB is the compact, pure-Go system database behind GraphJin-owned graph data.
It exists so catalog and workflow surfaces behave like ordinary GraphJin tables:
schema-discovered, joinable, filterable, searchable, and refreshable as atomic
snapshots.

nanoDB is built for:

- `gj_catalog`
- `gj_workflow`
- `gj_config`
- future compact GraphJin-owned system surfaces

Its job is not to replace user databases or CodeSQL. Its job is to make the
system graph first-class.

The important capabilities are:

- typed tables and columns
- primary keys and foreign keys
- named relationships
- secondary indexes
- full-text indexes and `search_rank`
- scalar and JSON columns
- indexed `in` lookups
- one-to-one, one-to-many, reverse, and self joins
- `where`, `search`, `order_by`, `limit`, and `offset`
- atomic snapshot refreshes

Conceptually, nanoDB gives GraphJin a small internal database that speaks the
same graph language as every other source.

## CodeSQL And `gj_code`

Code intelligence has a different scale profile from catalog data. A large
repository can have millions of syntax nodes, references, captures, docs, and
text chunks. That belongs in CodeSQL's durable indexed storage.

The public graph projection is `gj_code`.

```graphql
query {
  gj_code(
    search: "LoadUser email"
    where: { kind: { in: ["symbol", "reference", "db_reference"] } }
    order_by: { search_rank: desc }
    limit: 20
  ) {
    id
    kind
    name
    path
    language
    symbol_kind
    ref_kind
    db_object_id
    table_key
    column_key
    start_row
    end_row
    search_rank
  }
}
```

`gj_code.kind` values include:

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

The same root supports guarded code edits:

```graphql
mutation {
  gj_code(insert: {
    kind: "change_set"
    action: "preview"
    title: "Rename handler"
    edits: [
      {
        op: "replace"
        path: "handler.go"
        start_byte: 10
        end_byte: 20
        content: "newName"
        expected_hash: "..."
      }
    ]
  }) {
    id
    kind
    status
    diff
    errors_json
  }
}
```

Long-running work can reserve files or ranges through `gj_code` rows with
`kind: "lock"`. The model remains the same: code is queried and changed through
one GraphJin root, selected by `kind`.

## Workflows

Workflows are both catalog items and graph tables.

Discovery starts with `gj_catalog`:

```graphql
query {
  gj_catalog(search: "daily report", where: { kind: { eq: "workflow" } }) {
    id
    name
    title
    summary
    workflow_id
    examples_json
    safety_json
  }
}
```

The read surface is `gj_workflow`:

```graphql
query {
  gj_workflow(where: { enabled: { eq: true } }, order_by: { name: asc }) {
    name
    title
    summary
    catalog_item_id
    gj_catalog {
      id
      kind
      summary
      examples_json
      safety_json
    }
  }
}
```

Execution is `gj_workflow_execution`. This root is mutation-only and returns an ephemeral result row; it does not store workflow run history:

```graphql
mutation {
  gj_workflow_execution(insert: {
    workflow_name: "daily_report"
    variables: { account_id: 42 }
  }) {
    status
    result_json
    error
  }
}
```

Workflow source files remain the durable source of truth. nanoDB provides the
read/index surface that lets workflows participate naturally in GraphJin joins.

## Cross-Graph Joins

The real unlock is not that these roots exist. The unlock is that they join.

GraphJin can relate catalog items, code facts, workflows, and application data
through the same relationship machinery it already uses for databases and
remote sources.

Core relationship shapes:

```text
gj_workflow.catalog_item_id -> gj_catalog.id
gj_catalog.workflow_id -> gj_workflow.name
gj_catalog.code_refs_id -> code:gj_code.db_object_id
gj_code.catalog_item_id -> graphjin:gj_catalog.id
gj_code.table_catalog_item_id -> graphjin:gj_catalog.id
gj_code.column_catalog_item_id -> graphjin:gj_catalog.id
gj_code.file_id -> gj_code.id
gj_code.symbol_id -> gj_code.id
gj_code.parent_id -> gj_code.id
gj_code.target_symbol_id -> gj_code.id
```

Catalog to code:

```graphql
query {
  gj_catalog(
    where: {
      kind: { eq: "column" }
      table_name: { eq: "users" }
      column_name: { eq: "email" }
    }
    limit: 1
  ) {
    id
    name
    type
    gj_code {
      kind
      ref_kind
      path
      start_row
      symbol_id
    }
  }
}
```

Code to catalog:

```graphql
query {
  gj_code(where: { kind: { eq: "db_reference" }, table_name: { eq: "users" } }) {
    path
    ref_kind
    gj_catalog {
      id
      kind
      name
      summary
    }
  }
}
```

Workflow to catalog:

```graphql
query {
  gj_workflow(where: { name: { eq: "daily_report" } }) {
    name
    catalog_item_id
    gj_catalog {
      title
      summary
      safety_json
    }
  }
}
```

This is the agentic graph: the model can move from a business concept to a
table, from a table to the code that touches it, from code to workflows, and
from workflows back to the data they operate on.

## Query Patterns

### Find The Right Data

```graphql
query {
  gj_catalog(
    search: "customer account subscription"
    where: { kind: { eq: "table" } }
    order_by: { search_rank: desc }
    limit: 10
  ) {
    id
    table_name
    summary
    examples_json
  }
}
```

The agent starts with meaning, not memory. Search narrows candidates; `kind`
keeps the shape predictable.

### Inspect Columns And Safety

```graphql
query {
  gj_catalog(where: { kind: { eq: "column" }, table_name: { eq: "orders" } }) {
    column_name
    type
    summary
    evidence_json
    safety_json
  }
}
```

Column catalog items carry the details that keep filters, joins, and output
selection grounded.

### Learn GraphJin Syntax In Context

```graphql
query {
  gj_catalog(
    search: "running totals aggregate upsert nested insert"
    where: { kind: { in: ["directive", "operator_set", "query_pattern", "mutation_pattern"] } }
    order_by: { search_rank: desc }
  ) {
    kind
    name
    summary
    examples_json
  }
}
```

GraphJin is intentionally its own dialect. The catalog lets the runtime teach
that dialect through the same graph the agent uses for data discovery.

### Connect Data And Code

```graphql
query {
  gj_catalog(
    where: { kind: { eq: "table" }, table_name: { eq: "orders" } }
    limit: 1
  ) {
    id
    title
    gj_code(where: { kind: { in: ["db_reference", "symbol", "reference"] } }) {
      kind
      name
      path
      start_row
    }
  }
}
```

This makes questions like "where is this table used?" or "which handlers touch
this column?" part of ordinary GraphQL.

### Run A Workflow From Discovery

```graphql
query {
  gj_catalog(search: "reconcile billing", where: { kind: { eq: "workflow" } }) {
    name
    workflow_id
    summary
    input_schema_json
    safety_json
  }
}
```

```graphql
mutation {
  gj_workflow_execution(insert: {
    workflow_name: "billing_reconcile"
    variables: { account_id: 42 }
  }) {
    status
    result_json
    error
  }
}
```

Discovery and action stay close, but distinct.

## Agent Design Principles

### One Graph Over Many Systems

An enterprise is never one database. It is databases, services, files, code,
workflows, APIs, configs, and policies. GraphJin's job is to make those systems
feel like one graph without hiding their boundaries.

Agents should preserve those boundaries in their reasoning:

- application data comes from application roots
- system knowledge comes from `gj_catalog`
- code knowledge comes from `gj_code`
- reusable operations come from `gj_workflow`
- execution comes from `gj_workflow_execution`
- configuration comes from `gj_config`

### Evidence Before Action

Agents are strongest when they build a chain of evidence. A table name, column
type, relationship path, workflow input shape, mutation pattern, or code edit
should be grounded in graph data.

Evidence usually lives in:

- `summary`
- `details_json`
- `examples_json`
- `evidence_json`
- `safety_json`
- `edges_json`
- relationship fields such as `gj_code` and `gj_catalog`

### Query By Kind

The `kind` field is the model's steering wheel. It lets the same root expose a
wide system without multiplying roots:

```graphql
gj_catalog(where: { kind: { eq: "table" } }) { ... }
gj_catalog(where: { kind: { eq: "relationship" } }) { ... }
gj_code(where: { kind: { eq: "symbol" } }) { ... }
gj_code(where: { kind: { eq: "db_reference" } }) { ... }
```

This is closer to how GraphJin's GraphQL dialect already wants to be used:
single roots, strong filters, composable joins.

### Scale Where The Data Lives

nanoDB keeps compact system data nimble and joinable. CodeSQL keeps large code
indexes durable and searchable. User databases keep user data in the systems
that already own it.

The graph stitches these together. It does not flatten the company into one
giant in-memory structure.

### Prefer Durable Operations

Broad data questions often belong in workflows. Code edits belong in previewed
change sets. Config changes belong in guarded config mutations. The agentic
surface should make useful work repeatable, inspectable, and auditable.

## Enterprise Agent Patterns

### Analytics Agent

An analytics agent answers questions such as "Which product categories are
driving revenue this quarter?" or "Show moving average sales by region."

It discovers fact and dimension tables through `gj_catalog`, inspects
relationship items before nesting, uses query-pattern items for grouped
summaries, and moves broad or recurring analysis into workflows.

### Support Copilot

A support copilot investigates customer issues. It discovers customer, account,
order, invoice, ticket, or subscription tables; inspects policy/config items;
validates identifiers and statuses; and uses saved queries or workflows for
repeatable support paths.

### Operations Agent

An operations agent answers questions about inventory, incidents, fulfillment,
billing, and back-office workflow state. It uses catalog items to find
operational entities, checks sample/profile evidence for status values, and
uses workflows for multi-step operational summaries.

### Security And Policy Agent

A security or policy agent explains what roles can see, mutate, or execute. It
starts with config, capability, table, and column items, then connects those
facts to application roots, workflows, and code references.

### Code-Aware Data Agent

A code-aware data agent answers questions like "Which code paths write this
column?" or "Which workflow depends on this table?" It starts in `gj_catalog`,
joins into `gj_code`, follows symbols and references, and can preview code
changes through `gj_code` mutations.

### Workflow Automation Agent

A workflow automation agent creates reusable business logic for repeated tasks.
It discovers schema, syntax, runtime capabilities, and existing workflows first,
then saves or runs workflows through GraphJin.

## Minimal Agent Prompt

```text
You are connected to GraphJin. Treat GraphJin GraphQL as the operating surface
and gj_catalog as the source of truth.

For every data task:

1. Query gj_catalog with search for ranked discovery and where.kind for exact
   selectors over schema, relationships, language, config, workflow, policy,
   capability, and system-capability items.
2. Inspect details_json, evidence_json, examples_json, safety_json, and
   edges_json before choosing tables, columns, relationships, operators,
   workflows, actions, or code paths.
3. Use gj_workflow and gj_workflow_execution for reusable or broad work.
4. Use gj_code for code intelligence and code-edit preview/apply flows.
5. Use gj_config for config reads and guarded config updates.

Build every answer from the live graph: catalog item, evidence, join, action,
observation, refinement.
```

## What Success Looks Like

The system succeeds when an agent can answer:

- What data exists?
- Which fields are safe and useful?
- How do these entities relate?
- Which workflow already does this?
- Which code paths touch this table or column?
- What GraphJin syntax should I use here?
- What is the smallest safe action I can take next?

And it can answer those questions through one composable graph.

That is the point of Agentic GraphJin: a living graph that lets agents
understand and operate real software systems with evidence.
