---
title: "Mission Control"
description: "Review declared task trails, unseen watch events, and annotation drafts from the same governed map used by GraphJin agents."
nav_group: "agentic"
doc_kind: "guide"
weight: 48
---

Mission Control is the human window over GraphJin's durable agentic state. Open
`/mission` in the built-in console to see the work that otherwise lives behind
the `gj_task`, `gj_watch_event`, and `gj_artifacts` GraphQL roots.

It is available with the console in development and agentic modes. The page
polls GraphJin rather than introducing a second state store or privileged API:
people and agents read the same rows under the same caller identity and role.

## Set the development identity

Owner-scoped roots intentionally return no rows when no caller identity exists.
Use the identity control in the console header to set a development user, role,
and account. The console persists those values in browser local storage and
sends them as `X-User-ID`, `X-User-Role`, and `X-Account-ID`.

These headers are for `auth.development` only. With real authentication,
same-origin session cookies continue to flow as they do for every other console
page. Mission Control makes the missing-development-identity state explicit, so
an unauthenticated empty scope cannot look like an empty workload.

## Follow declared work

The Tasks view lists open, verifying, closed, verified, and failed tasks. Open a
task to inspect its newest-first immutable trail:

- caller journal entries;
- embedded-agent runs and their trace IDs;
- linked-watch creation;
- verification entries, including the declared expectation and observed
  saved-query result.

Task changes remain in [Agent chat](/agentic/server-agent/) for v1. Continue a
task from its drawer to pin the `task_id` above the composer. Every later chat
request sends that ID, warm-starts from the owner-scoped goal and recent trail,
and journals the run under the task. The ID remains correlation only and never
grants access or satisfies an evidence guard.

## Review the watch inbox

The Watch Inbox joins unseen events to their watch names, shows delivery status,
and exposes the bounded event payload, receipt, enrichment, and evidence in a
drawer. Marking an event seen uses the ordinary owner-scoped
`gj_watch_event(update: { seen: true })` mutation.

The watch list below the inbox shows lifecycle status, consecutive failures,
last fire time, and any linked task. Watch creation and editing remain in Agent
chat and GraphQL for v1.

## Publish reviewed graph memory

The Annotations view is the review queue for owner-only `observed` notes. Before
approval, Mission Control quotes the complete note and explains that publishing
makes it visible to agents in the account. GraphJin stamps the real approving
caller and approval time server-side.

Approved notes stay visible with their author, approver, target address, and
source task. Admins can demote a note back to `observed`. Annotation content
remains untrusted organizational data: approval cannot grant access, create
catalog evidence, or override source metadata.

## Act on agent notices

The Agent page renders every response notice instead of dropping it:

- `watch_events_unseen` opens the watch inbox;
- `task_open_unlinked` offers task-pin actions;
- `task_context_loaded` confirms the retained goal and trail count;
- `task_verify_failed` opens the failed task trail;
- `annotations_unshared` opens the annotation review queue.

Unknown future notice kinds still appear as generic notice cards, so new
server-side reminders do not silently disappear from the console.

Tasks, watches, and artifacts are queried independently. If one feature is
disabled, Mission Control hides only that tab and explains the missing
capability; the remaining views continue to work.
