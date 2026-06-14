---
title: "Agentic GraphJin"
description: "How GraphJin works as a governed operating graph for AI agents."
nav_group: "story"
doc_kind: "concept"
weight: 20
---

## The model-facing loop

Agentic GraphJin is GraphJin running in sources mode for users who work through AI clients. The model should learn the live system from the graph itself, not from memory or pasted schema.

Source note: this page is curated from the root `AGENTIC.md` document and keeps the technical `agentic/` docs in place.

```text
user instruction
  -> search gj_catalog for that intent
  -> inspect evidence and examples
  -> check gj_security and gj_runtime before risky actions
  -> join into application data, gj_code, workflows, or config
  -> validate or preview
  -> act through the governed GraphJin surface
  -> observe and refresh discovery
```

The first reliable move is usually an intent search:

```graphql
query {
  gj_catalog(
    search: "users email references"
    where: { kind: { in: ["column", "relationship", "query_pattern"] } }
    order_by: { search_rank: desc }
    limit: 10
  ) {
    id
    kind
    name
    summary
    evidence_json
    examples_json
    suggested_next_json
  }
}
```

## Surfaces and ownership

| Surface | Owner | Agent use |
| --- | --- | --- |
| Application roots | Databases, MongoDB, OpenAPI, filesystems, CodeSQL, and other configured sources | Business facts and source-owned state. |
| `gj_catalog` | GraphJin catalog | Discovery, syntax, examples, relationships, capabilities, and evidence. |
| `gj_security` | GraphJin security report | Effective policy and findings before writes or control-plane actions. |
| `gj_runtime` | GraphJin runtime status | Recent bounded decision support for stale schema, degraded sources, and reload/discovery state. |
| `gj_code` | CodeSQL | Files, symbols, references, database references, docs, parse state, and guarded change sets. |
| `gj_workflow` | Workflow source | Reviewed procedures, input contracts, hashes, and lifecycle facts. |
| `gj_workflow_execution` | Workflow runtime | Mutation-only execution that returns an ephemeral result row. |
| `gj_config` | GraphJin config | Redacted current config and guarded preview/apply updates when policy allows. |

## MCP is the bootstrap, not the whole graph

MCP helps a fresh model enter the graph correctly. It should discover first, validate before execution, and prefer saved queries or policy-controlled GraphQL actions.

```bash
graphjin mcp add codex
graphjin mcp add claude

codex mcp add graphjin --url https://graphjin.example.com/api/v1/mcp
claude mcp add --transport http graphjin https://graphjin.example.com/api/v1/mcp
```

Once connected, the model should use `query_catalog`, `graphql_help`, `validate_where_clause`, and saved-query execution to learn the safe path. Direct GraphQL remains the richest surface for composed reads across application and system roots.

## Preflight before action

Before writes, config changes, workflow execution, schema changes, file access, or source edits, agents should inspect security and runtime context:

```graphql
query {
  summary: gj_security(id: "summary") {
    mode
    summary
    summary_json
  }

  recent: gj_runtime(
    where: { kind: { in: ["status", "source", "event"] } }
    order_by: { created_at: desc }
    limit: 20
  ) {
    kind
    source
    status
    severity
    summary
    next_action
  }
}
```

## Source edits use preview/apply

CodeSQL edits are guarded: the agent reads file paths and hashes, proposes exact ranges and `old_text`, previews the change, then applies only if the preview is valid and policy allows it. Hash mismatches, overlapping locks, missing old text, and disallowed paths become structured errors.

That loop keeps agent work grounded in current source truth while giving humans an auditable preview boundary.

The canonical long-form source for this page is `AGENTIC.md`.
