---
title: "Declared Tasks"
description: "Persist an explicit owner-scoped goal, warm-start later agent runs, and retain a provenance-labeled trail without weakening GraphJin's guards."
nav_group: "agentic"
doc_kind: "guide"
weight: 46
---

A GraphJin task is explicit durable intent: one owner-scoped `gj_task` goal
plus immutable `gj_task_entry` trail rows. It is not inferred memory, a hidden
session, or an authorization token. Use it when work should survive the
conversation that declared it—especially when a linked [watch](/agentic/watches/)
may fire later with no conversation history available.

## Create and journal

Create a task explicitly and retain the returned ID:

```graphql
mutation {
  gj_task(insert: {
    goal: "Investigate delayed production orders"
    snapshot_json: { region: "west" }
  }) {
    id
    goal
    status
  }
}
```

Append a caller note through the immutable entry root:

```graphql
mutation {
  gj_task_entry(insert: {
    task_id: "task:..."
    body: "Use the carrier SLA as the threshold."
    detail_json: { source: "ops review" }
  }) {
    id
    origin
    created_at
  }
}
```

Callers can set only `task_id`, `body`, and `detail_json` on an entry. GraphJin
sets its ID, `origin`, status, trace/watch IDs, identity, and timestamps.
Missing, closed, and foreign tasks share `task_not_found_or_closed`, so the API
does not disclose whether another owner has a matching ID.

{{< verified by="TestTaskControlPlaneLifecycleScopeAndTrail" file="serv/tasks_test.go" line="48" >}}
{{< verified by="TestTaskProjectionGraphQLTraversal" file="serv/tasks_flow_test.go" line="16" >}}

## Warm-start the built-in agent

Pass `task_id` to `ask_graphjin_agent` or `POST /api/v1/agent`. GraphJin prepends
the declared goal and up to five recent trail entries to the request's
untrusted history, then writes one `agent_run` entry with the answer, used
skills, action summary, catalog detail IDs, refusal code, usage, duration, and
trace ID. Replaying the same trace is idempotent.

Task history remains a hint. The current run must still inspect catalog detail,
validate filters, and establish mutation evidence. `task_id` never grants
access and never satisfies a protocol guard.

{{< verified by="TestTaskAgentHTTPFlowWarmStartsAndJournals" file="serv/tasks_flow_test.go" line="63" >}}
{{< verified by="TestRunRejectsSavedQueryExecutionBeforeDetailLookup" file="agent/agent_test.go" line="375" >}}

## Link a watch

Set `task_id` when creating `gj_watch` to preserve why the standing question
exists. GraphJin verifies the same-owner task is open and appends one
idempotent `watch_created` trail entry. Task deletion clears the link without
deleting the watch. Watches over `gj_task` or `gj_task_entry` are rejected in
v1 to prevent feedback loops.

{{< verified by="TestTaskWatchLinkJournalsAndDeleteUnlinks" file="serv/tasks_test.go" line="170" >}}
{{< verified by="TestWatchRejectsTaskRootSubscriptions" file="serv/tasks_test.go" line="226" >}}

## Close, retain, and configure

Close a finished task with an outcome; reopening is allowed:

```graphql
mutation {
  gj_task(
    where: { id: { eq: "task:..." } }
    update: { status: "closed", outcome: "Carrier mapping corrected." }
  ) {
    id
    status
    outcome
    closed_at
  }
}
```

Prefer close over delete so the trail remains auditable. A real owner delete
cascades entries and unlinks watches. Parsed `dev` and `agentic` configs enable
tasks with the [artifact store](/agentic/artifacts/); tune `tasks.max_per_owner`,
`tasks.max_entries_per_task`, `tasks.entry_retention_hours`, and
`tasks.snapshot_max_bytes` when needed. There are no task-specific REST wrappers
or `graphjin://task` resource; lifecycle and journals use GraphQL, while only
`ask_graphjin_agent` adds a new `task_id` argument.

At runtime, inspect `query_catalog(id: "help:tasks")` for the local contract.
