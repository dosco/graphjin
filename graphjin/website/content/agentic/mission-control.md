---
title: "Mission Control"
description: "Review declared task trails, unseen watch events, and annotation drafts from the same governed map used by GraphJin agents."
nav_group: "agentic"
doc_kind: "guide"
weight: 48
---

Mission Control is the human window over GraphJin's durable agentic state. It
moved out of GraphJin into the OpenNeko admin app (`/admin/graphjin/activity`),
which shows the work that otherwise lives behind the `gj_task`,
`gj_watch_event`, and `gj_artifacts` GraphQL roots.

It reads GraphJin directly rather than introducing a second state store or
privileged API: people and agents read the same rows under the same caller
identity and role.

## Caller identity

Owner-scoped roots intentionally return no rows when no caller identity exists.
OpenNeko calls GraphJin with a short-lived token for the signed-in admin, so the
page shows that admin's tasks, watches, and annotations.

## Follow declared work

The Tasks view lists open, verifying, closed, verified, and failed tasks. Open a
task to inspect its newest-first immutable trail:

- caller journal entries;
- embedded-agent runs and their trace IDs;
- linked-watch creation;
- verification entries, including the declared expectation and observed
  saved-query result.

Task changes remain in [Agent chat](/agentic/server-agent/). A chat request that
sends a `task_id` warm-starts from the owner-scoped goal and recent trail, and
journals the run under the task. The ID remains correlation only and never
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

The agent panel on the admin workbench shows every response notice instead of
dropping it, such as `watch_events_unseen`, `task_open_unlinked`,
`task_context_loaded`, `task_verify_failed`, and `annotations_unshared`.
Unknown future notice kinds appear too, so new server-side reminders do not
silently disappear.

Tasks, watches, and artifacts are queried independently. If one feature is
disabled, Mission Control explains the missing capability in that section;
the remaining sections continue to work.
