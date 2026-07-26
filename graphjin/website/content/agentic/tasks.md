---
title: "Declared Tasks"
description: "Persist an owner-scoped goal, keep it visible across agent runs, and optionally require a declared saved-query check before close."
nav_group: "agentic"
doc_kind: "guide"
weight: 46
---

A GraphJin task is explicit durable intent: one owner-scoped `gj_task` goal
plus immutable `gj_task_entry` trail rows. It is not inferred memory, a hidden
session, or an authorization token. Use it when work should survive the
conversation that declared it—especially when a linked [watch](/agentic/watches/)
may fire later with no conversation history available.

A task can also carry a declared verification. On close, GraphJin runs a saved
query as the task owner and records what the data actually said. This separates
an unverified claim (`status: "closed"`, empty `verify_status`) from a proved
outcome (`status: "closed"`, `verify_status: "verified"`).

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

## Keep the task visible

GraphJin reinforces declared intent without guessing it. Agent responses use
three bounded, owner-scoped notices:

- `task_open_unlinked` lists open or verifying tasks when the current run has no
  `task_id`, so a client can deliberately continue one or start unrelated work.
- `task_context_loaded` confirms the goal, recent trail count, and catalog hints
  that were loaded for a task-bound run.
- `task_verify_failed` reports tasks whose declared check failed and stayed open.

After task creation, the MCP `next` guidance offers concrete follow-ups: append
a trail note, continue the task with `ask_graphjin_agent`, or close it with a
saved-query verification. IDs and notices are correlation aids, never grants.

{{< verified by="TestTaskOpenUnlinkedNoticeIsBoundedAndOwnerScoped" file="serv/tasks_notices_test.go" line="12" >}}
{{< verified by="TestTaskContextLoadedNoticeReportsWarmStart" file="serv/tasks_notices_test.go" line="84" >}}
{{< verified by="TestTaskCreationNextGuidance" file="serv/tasks_notices_test.go" line="123" >}}

## Warm-start the built-in agent

Pass `task_id` to `ask_graphjin_agent` or `POST /api/v1/agent`. GraphJin prepends
the declared goal and up to five recent trail entries to the request's
untrusted history, then writes one `agent_run` entry with the answer, used
skills, action summary, catalog detail IDs, refusal code, usage, duration, and
trace ID. Replaying the same trace is idempotent.

Open and verifying tasks can warm-start the agent. Task history remains a hint:
the current run must still inspect catalog detail, validate filters, and
establish mutation evidence. `task_id` never grants access and never satisfies
a protocol guard.

{{< verified by="TestTaskAgentHTTPFlowWarmStartsAndJournals" file="serv/tasks_flow_test.go" line="63" >}}
{{< verified by="TestRunRejectsSavedQueryExecutionBeforeDetailLookup" file="agent/agent_test.go" line="375" >}}

## Link a watch

Set `task_id` when creating `gj_watch` to preserve why the standing question
exists. GraphJin verifies the same-owner task is open or verifying and appends one
idempotent `watch_created` trail entry. Task deletion clears the link without
deleting the watch. Watches over `gj_task` or `gj_task_entry` are rejected in
v1 to prevent feedback loops.

{{< verified by="TestTaskWatchLinkJournalsAndDeleteUnlinks" file="serv/tasks_test.go" line="170" >}}
{{< verified by="TestWatchRejectsTaskRootSubscriptions" file="serv/tasks_test.go" line="226" >}}

## Close with proof

Declare a saved query, variables, and one small expectation in `verify_json`:

```graphql
mutation {
  gj_task(
    where: { id: { eq: "task:..." } }
    update: {
      status: "closed"
      outcome: "No delayed orders remain."
      verify_json: {
        saved_query_name: "late_orders_count"
        variables: { region: "west" }
        expect: { path: "orders", op: "empty" }
      }
    }
  ) {
    id
    status
    outcome
    verify_status
    verify_attempts
    closed_at
  }
}
```

Without `recheck`, GraphJin runs the check inside the close request. A pass
closes the task with `verify_status: "verified"`. A failed condition, query
error, or invalid result leaves the task open with `verify_status: "failed"`,
clears the claimed outcome and `closed_at`, writes a `verification` trail entry,
and returns the row successfully so the caller can see that close did not stick.

Add `recheck: "2h"` for one delayed check. The close first returns
`status: "verifying"`, `verify_status: "pending"`, and `verify_after`. A durable
sweep later closes a passing task or reopens a failing one. Reopening while it
is verifying cancels that pending attempt. Windows are normalized between one
minute and 30 days.

| Action | First state | Final state |
| --- | --- | --- |
| Close without a check | `closed` | `closed` (claimed) |
| Close with an immediate check | request stays open | `closed` when verified; otherwise `open` |
| Close with `recheck` | `verifying` | `closed` when verified; otherwise `open` |
| Explicitly reopen while verifying | `open` | `open` with verification cancelled |
| Explicitly reopen a closed task | `open` | `open` |

The expectation operators are deliberately small: `empty`, `not_empty`,
`count_le`, `count_ge`, `eq`, `neq`, `le`, and `ge`. Paths are dotted field
names with optional numeric array indices; wildcards are rejected. When `path`
is omitted, the response must have exactly one root field and that field must
be an array. Numeric ordering is typed rather than string-based.

Verification accepts saved queries only—never inline GraphQL or
`condition_js`. The query must resolve for the owner and must be a read query.
GraphJin executes it with the stored owner identity and hardened role, so a
background check cannot gain more access than the person who declared the task.
The normalized spec is hashed into a deterministic trail entry with the
expectation, bounded observed value, attempt, and duration. Retries converge on
one entry, and changing the spec cannot inherit an earlier verdict.

{{< verified by="TestTaskImmediateVerificationPassAndFail" file="serv/tasks_verify_test.go" line="57" >}}
{{< verified by="TestTaskDelayedVerificationSweepAndReopenCancellation" file="serv/tasks_verify_test.go" line="134" >}}
{{< verified by="TestTaskVerificationClaimIsSingleWinnerAcrossReplicas" file="serv/tasks_verify_test.go" line="230" >}}
{{< verified by="TestTaskVerifyJSONSemanticRejects" file="serv/tasks_verify_test.go" line="347" >}}

## Close by claim, retain, and configure

Verification is optional. Close without `verify_json` when the outcome is a
human claim or has no data-level predicate; reopening is allowed:

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
