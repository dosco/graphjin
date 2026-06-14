---
title: "Vision"
description: "GraphJin's thesis: one governed graph over an organization's data, APIs, files, workflows, and code."
nav_group: "story"
doc_kind: "concept"
weight: 10
---

## Organizations are data and code

GraphJin starts from a practical observation: an organization is made from data and code, but most tools force people and agents to inspect them separately.

Source note: this page is curated from the root `VISION.md` document and shaped for the website Story section.

Data holds customers, orders, inventory, payments, permissions, incidents, metrics, logs, and decisions. Code decides what gets created, validated, billed, escalated, shown, or trusted. The useful questions inside a company rarely stay in only one of those systems.

GraphJin's vision is a common graph that connects those systems through one governed interface. A model can move from a customer row to invoices, workflows, API operations, files, and source references without switching tools or guessing how the organization is wired.

## The missing graph

GraphJin is not an ORM and not a resolver framework. For SQL databases, it compiles GraphQL into optimized SQL. For MongoDB, it emits a JSON DSL that becomes aggregation pipelines. For OpenAPI, filesystem, CodeSQL, workflow, and GraphJin-owned system surfaces, it exposes table-like roots with the same policy model.

```graphql
query {
  gj_columns(where: { table_name: { eq: "invoices" } }) {
    column_name
    type
    code_db_refs {
      ref_kind
      file { path }
      symbol { name kind }
    }
  }
}
```

That query is small, but the idea is large: the model can connect schema truth to implementation evidence before it reasons or edits.

## Governed exploration

The goal is not to make agents safe by making them powerless. The goal is to let them explore inside boundaries humans can understand, review, and enforce.

GraphJin puts those boundaries in one place: sources, roles, saved queries, read-only rules, table permissions, column blocks, mutation limits, MCP settings, OAuth, config update policy, workflow policy, and source edit controls. Humans can review the config. Agents can query the catalog and security graph before acting.

## Why it matters

An agent entering an organization needs more than a prompt. It needs a map:

- what data exists and how it relates
- which source owns each table-like root
- which code reads or writes important fields
- which API operations and workflows are available
- which operations are safe, blocked, or require preview
- which query shape is valid before execution

GraphJin makes that map queryable through GraphQL and MCP. The result is fewer invented joins, fake fields, pasted schemas, and risky one-off integrations.

## Where the vision shows up

| Capability | What it contributes |
| --- | --- |
| Compiler core | Pushes filters, joins, pagination, JSON construction, aggregation, and mutations into the backend instead of resolver code. |
| `gj_catalog` | Gives humans and models a searchable map of tables, columns, relationships, syntax, examples, source capabilities, and evidence. |
| `gj_security` | Exposes effective policy, read-only state, findings, and recommendations before sensitive actions. |
| CodeSQL | Makes source trees queryable as indexed code facts and supports guarded preview/apply edits. |
| Workflows | Exposes reviewed operational procedures instead of letting agents improvise multi-step actions. |
| MCP | Teaches AI clients how to discover, validate, and execute through GraphJin's governed surface. |

The canonical long-form source for this page is `VISION.md`.
