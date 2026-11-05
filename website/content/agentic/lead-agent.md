---
title: "Lead Agent"
description: "The lead agent holds the conversation, your skills and your memories, and asks a stateless data worker the questions. Every part of it is readable and writable through GraphQL."
nav_group: "agentic"
doc_kind: "guide"
weight: 6
---

GraphJin's [server-side agent](/agentic/server-agent/) is two agents. The
**worker** answers one self-contained data question: it discovers the catalog,
runs a governed query, and returns a database-computed, grounded answer. It
uses fixed built-in skills, keeps no state, and is meant to run on a small, fast
model. The **lead** holds the conversation. It reads the thread's history, the
agent profile's instructions, and the caller's memories and skills, then sends
the worker self-contained questions through its `ask_graphjin` function.

None of the conversation reaches a worker. The lead resolves "that customer" or
"last month" first, and then asks a question that makes sense on its own.
Questions it sends together run in parallel. The lead never computes aggregates
itself; it asks a worker, and the worker computes them in the database.

Everything the lead keeps is a row in the graph: skills and memories in
`gj_artifacts`, conversations in `gj_thread`, `gj_thread_message`, and
`gj_thread_run`. A GUI that lists, edits, and shares them needs only
`/api/v1/graphql`. REST is left for the actions that are not row writes:
starting a turn, following or cancelling a run, and finding an agent's home
thread.

{{< verified by="TestLeadKeepsConversationStateOutOfWorkers" file="agent/lead_test.go" >}}
{{< verified by="TestLeadFansOutQuestionsInParallel" file="agent/lead_test.go" >}}

## Turn the lead on or off

`agent.lead.enabled` decides which agent the outside world talks to.

| Setting | What `POST /api/v1/agent` and `ask_graphjin_agent` run |
|---------|--------------------------------------------------------|
| `agent.lead.enabled: true` | The lead, for every call: console chat, other thread turns, single questions, and watch routines. The worker is reachable only through the lead's questions. |
| `agent.lead.enabled: false` | The worker, directly, for single self-contained questions. Conversations, memories, skills, and agent profiles are unavailable. |

In `dev` and `agentic` modes the default follows `agent.enabled`, so the lead is
on whenever the agent is. Production defaults it to `false`. Use the
`GJ_AGENT_LEAD_ENABLED` environment variable for environment-only deployments.

With the lead off, GraphJin refuses any request that carries a conversation. A
REST request with `history` or a `thread_id` returns `400` with the error
`conversations (thread_id, history) require agent.lead.enabled`, and the MCP
tool returns the same message as a tool error. Writing a skill fails with
`skills are disabled: set agent.lead.enabled`. Setting `threads.enabled` or
`memory.enabled` to `true` explicitly while the lead is off fails configuration
validation at startup.

{{< verified by="TestAgentRequestsRunTheLeadWhenEnabled" file="serv/lead_runner_test.go" >}}

### Pick the lead's model

Unset, the lead runs on the worker's model. A typical split is a capable lead
over a fast worker:

```yaml
agent:
  provider: google-gemini
  model: gemini-3.5-flash
  api_key_env: GOOGLE_API_KEY
  lead:
    enabled: true
    model: gemini-3.5-pro
    max_parallel: 4     # questions running at once (max 16)
    max_questions: 8    # questions per turn
    timeout_seconds: 120
```

`agent.lead.provider`, `api_key_env`, `base_url`, `reasoning`, and `max_steps`
are also available. `api_key_env` is required when the lead's provider differs
from the worker's. The [Config Reference](/reference/config-reference/) lists
every option and its environment variable.

## Skills

A skill is a named, reusable instruction you write for the lead: "how we run
the weekly churn review." It is a `gj_artifacts` row with `kind: "skill"`.

| Field | Rule |
|-------|------|
| `name` | Required. Unique per owner, case-insensitive. At most 64 bytes. |
| `description` | Required. One line that says when to use the skill. At most 200 bytes. |
| `content` | Required. The instructions. At most 8 KB. |
| `tier` | `observed` (personal, the default) or `approved` (shared with the owner's account). |

Each owner can keep up to 50 skills. The lead sees every visible skill's name
and description. When a request matches one, it loads that skill's
instructions with `discover()` and follows them. The skills it used are
recorded on the run (`gj_thread_run.used_skills`) and in the response's
`skills` field.

```graphql
mutation {
  gj_artifacts(insert: {
    kind: "skill"
    name: "churn review"
    description: "How we report churn"
    content: "Report churn as a count and the MRR lost over the last 30 days."
  }) { id name tier }
}
```

```graphql
query {
  gj_artifacts(where: { kind: { eq: "skill" } }) {
    id name description content tier owner_ref
  }
}
```

Share a skill with your account by setting its tier. Editing a shared skill's
description or content takes it back to personal until you share it again, so a
teammate never runs instructions nobody approved:

```graphql
mutation {
  gj_artifacts(where: { id: { eq: "skill:..." } }, update: { tier: "approved" }) { id tier }
}
```

```graphql
mutation {
  gj_artifacts(where: { id: { eq: "skill:..." } }, delete: true) { id }
}
```

Only the owner (or an admin) can edit or delete a skill. Skills are separate
from the worker's fixed built-in skills, which come from the caller's
capability profile and never change.

{{< verified by="TestSkillsAreGraphQLArtifacts" file="serv/skills_test.go" >}}
{{< verified by="TestLeadLoadsOnlyCatalogSkills" file="agent/lead_test.go" >}}

## Memories

A memory is one saved preference or fact, a `gj_artifacts` row with
`kind: "memory"`: a `content` of at most 4 KB and the same personal or team
`tier` as a skill. Each owner can keep up to 200 (`memory.max_per_owner`).

Every lead turn starts with the caller's most relevant personal and team
memories, up to `memory.recall_limit` (default 5, and at most 2 KB in total).
The lead can call `recall()` to fetch more as it works. Recall ranks with the
[semantic catalog search](/configure/discovery-semantic-search/) embedder when
that is enabled, and falls back to shared-word matching otherwise. Memories are
standing preferences, never evidence: an answer still has to be grounded in
what a worker returned.

The lead saves new memories with its `remember` tool. It uses that tool only
when the user asks it to remember something or states a standing preference
("from now on, report revenue in euros"), and never for query results, secrets,
or one-off requests. `remember` is a `gj_artifacts` insert made as the caller,
so a saved memory shows up in the same list a GUI reads:

```graphql
query {
  gj_artifacts(where: { kind: { eq: "memory" } }) { id content tier owner_ref }
}
```

```graphql
mutation {
  gj_artifacts(insert: { kind: "memory", content: "Report revenue in euros." }) { id tier }
}
```

A read-only lead, including every watch routine, has no `remember` tool. The
memories a turn used are recorded on the run (`gj_thread_run.used_memories`)
and in the response's `memories` field.

{{< verified by="TestMemoryScopesAndPublishing" file="serv/memory_test.go" >}}
{{< verified by="TestLeadRemembersOnlyOnWritableTurns" file="agent/lead_test.go" >}}

## Threads

A thread is a persistent conversation. Threads, their messages, and their runs
are three GraphQL roots, enabled with `threads.enabled`. The rows are the
caller's own; an admin can read every owner's.

| Root | Fields | Writes |
|------|--------|--------|
| `gj_thread` | `id title profile status channel active_run_id unread owner_ref last_message_at last_seen_at created_at updated_at` | `insert` of `title` and `profile`; `update` of `title`, `profile`, `status` (`open` or `archived`), or `last_seen_at`; `delete: true` |
| `gj_thread_message` | `id thread_id seq role content status run_id catalog_ids response created_at` | Read-only. Turns write messages. |
| `gj_thread_run` | `id thread_id status tokens_in tokens_out llm_calls used_skills used_memories error created_at updated_at finished_at` | Read-only. Turns write runs. |

These roots have no nested relationships, so filter messages and runs by
`thread_id`. They are served live from the thread store rather than the system
NanoDB mirror, so query them in their own request, not alongside `gj_artifacts`
or other mirrored roots.

### A GUI's thread queries

List the open threads, newest first:

```graphql
query {
  gj_thread(where: { status: { eq: "open" } }, order_by: { last_message_at: desc }) {
    id title unread last_message_at active_run_id
  }
}
```

Read one conversation in order:

```graphql
query {
  gj_thread_message(where: { thread_id: { eq: "th_..." } }, order_by: { seq: asc }) {
    seq role content status run_id created_at
  }
}
```

See what each turn cost and what shaped it:

```graphql
query {
  gj_thread_run(where: { thread_id: { eq: "th_..." } }) {
    id status tokens_in tokens_out llm_calls used_skills used_memories error finished_at
  }
}
```

Create, rename, archive, mark read, and delete:

```graphql
mutation { gj_thread(insert: { title: "Churn review" }) { id title unread } }
```

```graphql
mutation {
  gj_thread(where: { id: { eq: "th_..." } }, update: { title: "Churn, Q3" }) { id title }
}
```

```graphql
mutation {
  gj_thread(where: { id: { eq: "th_..." } }, update: { status: "archived" }) { id status }
}
```

```graphql
mutation {
  gj_thread(where: { id: { eq: "th_..." } }, update: { last_seen_at: "now" }) { id unread }
}
```

```graphql
mutation {
  gj_thread(where: { id: { eq: "th_..." } }, delete: true) { id deleted }
}
```

A thread reports `unread: true` when an assistant message landed after the
owner last looked. Updating `last_seen_at` with any value marks it read as of
the server's clock; the owner's own messages mark it read too. Deleting a
thread removes its messages and runs. `threads.max_per_owner` (default 500)
caps open threads; archived threads do not count.

Messages and runs cannot be written through GraphQL. A turn writes them, and a
turn starts at `POST /api/v1/agent`.

{{< verified by="TestThreadsAreGraphQLRoots" file="serv/threads_graphql_test.go" >}}
{{< verified by="TestThreadTurnRunsTheLeadOverStatelessWorkers" file="serv/lead_runner_test.go" >}}

## Agent profiles

An agent profile is a named set of standing instructions, a `gj_artifacts` row
with `kind: "agent_profile"`: a `name`, the instructions as `content` (at most
2 KB), and an optional `icon`. Profiles follow the same tiers as skills and
memories, and changing a shared profile's instructions makes it personal again.
A thread whose `profile` names a profile the caller can see runs with those
instructions. Profiles shape the lead's guidance only; they grant no access.

```graphql
mutation {
  gj_artifacts(insert: { kind: "agent_profile", name: "Ops", content: "Focus on late shipments.", icon: "truck" }) {
    id name tier
  }
}
```

Every user gets one home thread per agent they can see, found or created on
first use through REST (below). Watches that post to an agent post to that
thread.

{{< verified by="TestAgentProfilesThroughGraphQL" file="serv/agent_profiles_test.go" >}}

## REST actions

Four REST actions remain. Each is an action, not a row write.

| Method and path | Purpose |
|-----------------|---------|
| `POST /api/v1/agent` | Start a turn. Send `instruction`, plus a `thread_id` to continue a thread. The thread's stored history replaces any `history` in the body. Add `Accept: text/event-stream` to stream the turn. |
| `GET /api/v1/agent/runs/{id}/events` | Replay and follow a run as server-sent events. Resume with `Last-Event-ID` or `?after=N`. |
| `POST /api/v1/agent/runs/{id}/cancel` | Cancel a run, whichever replica is executing it. |
| `GET /api/v1/agent/profiles/{id}/thread` | The caller's home thread for an agent profile, found or created. |

```bash
curl -sS http://localhost:8080/api/v1/agent \
  -H 'content-type: application/json' \
  --data '{"instruction":"Which customers churned last month?","thread_id":"th_..."}'
```

A run's id is on `gj_thread.active_run_id` while it runs, and on each message's
`run_id`. A run keeps going if the client disconnects, so a GUI can reattach
through the events endpoint. If the replica running it stops, a lease sweep on
any replica marks the run `failed`; GraphJin never re-executes it, because it
may already have written data.

Run events carry `delegate` (the question's 1-based index) on every worker step
and `question` on the lead's own start and finish events, so a client can group
a turn's trail by question. The response's `data.delegations` lists each
question with its status, answer, and data, and `usage` counts the lead and all
of its workers.

The removed REST CRUD routes for threads, memories, and profiles (for example
`GET /api/v1/agent/threads` and `/api/v1/agent/memories`) are now the GraphQL
roots above.

## Grounding and changes

The lead's reply is grounded the same way a worker's is. An identifier that no
delegated question returned triggers one restatement, then an
`ungrounded_answer_fields` refusal. A question or memory repeated within one
turn is answered from the first result.

The lead delegates changes, such as creating a watch or a task or writing data,
as explicit instructions, and only after the user asked for or confirmed the
change. The worker's guards, roles, and row-level security still decide whether
the change runs. A routine's lead and workers are read-only.

{{< verified by="TestLeadGroundsItsAnswerInWorkerResults" file="agent/lead_test.go" >}}
{{< verified by="TestLeadAnswersARepeatedQuestionOnce" file="agent/lead_test.go" >}}
