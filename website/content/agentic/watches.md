---
title: "Watches"
description: "Cursor-backed standing questions with optional inline, tool-free AxFlow triage before durable inbox, webhook, workflow, or MCP delivery."
nav_group: "agentic"
doc_kind: "guide"
weight: 47
---

A watch is a standing question: "tell me when a roast batch fails QC twice in a week." GraphJin stores the definition under `gj_watch`, evaluates it as a governed cursor-backed subscription **with the owner's stored identity and role**, and writes fired events to the owner's `gj_watch_event` inbox. Optional delivery fans out to webhooks or workflows. An inline, tool-free AxFlow can triage an event before anything wakes.

Watches never elevate access: a watch can only ever see what its owner could already query, and both roots are owner-scoped — callers see only their own watches and events.

Start with [Choosing Watches, Flows, and Workflows](/agentic/watch-automation/) when deciding whether a request needs a plain watch, AI triage, an autonomous action, or both.

## Defaults and overrides

Watches persist through the [artifact store](/agentic/artifacts/). In `dev` and `agentic` modes both are enabled automatically, and the watch runner defaults to `all`; no application subscription starts until an active, approved watch exists.

Use configuration only to opt out or choose which replicas evaluate:

```yaml
watches:
  runner: "off" # definitions persist, nothing evaluates on this replica
```

`runner` is per-replica. Set `watches.enabled: false` to remove the watch roots entirely. If artifacts are explicitly disabled and watches are omitted, watches are disabled too; explicitly enabling watches without artifacts is a configuration error. The `recipe.config.enable_watches` catalog recipe walks an agent through these changes.

## Create, pause, delete

A watch needs a `name` and either a cursor-capable subscription `query` or a `saved_query_name`:

```graphql
mutation {
  gj_watch(
    insert: {
      name: "new_orders"
      query: "subscription new_orders { orders(first: 25, after: $cursor, order_by: { id: asc }) { id status } orders_cursor }"
    }
  ) {
    id
    lifecycle
    status
  }
}
```

Pause and resume via `status` / `enabled` updates; remove with `gj_watch(delete)`. Optional fields: `variables_json`, `delivery_json` (inbox digest, webhook, or workflow targets), `enrich_json` (inline AxFlow triage or legacy read-only agent enrichment), and `absence_json` (source-silence detection). `condition_js` remains stored for compatibility but is not executed by the current watch runner. User-supplied JSON fields are size-capped at `snapshot_max_bytes`.

## Absence watches

Subscriptions report changes, not silence. Add `absence_json` for requests such as "tell me if no shipment scan arrives for four hours":

```graphql
mutation {
  gj_watch(
    insert: {
      name: "shipment_scan_silence"
      query: "subscription shipment_scan_silence { shipment_scans(first: 25, after: $cursor) { id scanned_at } shipment_scans_cursor }"
      absence_json: { enabled: true, window: "4h", repeat: false }
    }
  ) {
    id
    absence_json
    action_hash
    action_approval
  }
}
```

The window is normalized between one minute and 30 days. When it elapses, GraphJin creates a synthetic event with `data_json.kind: "absence"` and the silence anchor, detection time, and fire count. A fresh process anchors the window at runtime startup, so restart or failover does not mass-fire overdue watches. Real source data re-arms a non-repeating episode. Use `repeat: true` carefully: it intentionally produces another event every window while silence continues.

## Inline AxFlow triage

Flows are watch-owned runtime configuration, not reusable artifacts. Use the explicit built-in or store Mermaid directly in `enrich_json`:

```graphql
mutation {
  gj_watch(
    insert: {
      name: "coffee_roast_triage"
      query: "subscription coffee_roast_triage { roast_batches(first: 25, after: $cursor) { id phase temperature } roast_batches_cursor }"
      enrich_json: { enabled: true, kind: "flow", flow: "default_watch_triage" }
    }
  ) { id flow_hash flow_approval status enabled enrich_json }
}
```

Inline Mermaid must compile through AxFlow and return exactly `verdict` (`notify`, `digest`, or `discard`), `severity` (`info`, `warn`, or `critical`), and `summary` (at most 280 characters). Flows use GraphJin's server-side `agent` provider/model/credential settings; they run without tools, GraphJin bindings, or workflow callables. A new or changed flow is stored paused with approval pending. Preview it against explicit samples or retained events, then optionally approve and resume in the same mutation:

```graphql
mutation {
  gj_watch(
    where: { id: { eq: "watch:..." } }
    update: {
      flow_review_json: {
        decision: "approve"
        expected_flow_hash: "..."
        samples_json: [{ batch_id: "batch-7", temperature: 421 }]
      }
    }
  ) {
    id
    flow_hash
    flow_approval
    flow_preview_json
    status
    enabled
  }
}
```

Use `decision: "preview"` to store evidence without approving, `approve` only after inspecting a successful preview for the current hash, and `reject` to keep the watch paused. `notify` leaves the event pending and unseen, so its per-watch MCP resource wakes. `digest` records `digest_queued` without waking immediately; after `delivery_json.digest.window`, GraphJin drains the queued members into one unseen `data_json.kind: "digest"` event without another model call. The default window is one hour when no digest block is present. `discard` records `suppressed` and marks the event processed without waking. Invalid output, timeout, missing server credentials, the daily enrichment cap, or any model error sends the raw pending notification but never executes a workflow or webhook. Omitting a flow makes no model call. The event's `enrichment_json` and compact MCP entry include the triage verdict, severity, and summary.

Configure a digest window while retaining inbox delivery:

```graphql
delivery_json: { kind: "inbox", digest: { window: "1h" } }
```

Digest controls noise from one watch. It is different from a rollup watch, which correlates events across watch IDs.

{{< verified by="TestWatchFlowReviewApprovesCurrentInlineFlow" file="serv/watch_flow_test.go" line="104" >}}

Normal watches are durable by default. Use an explicit lease only when the user asks for a TTL:

```graphql
mutation {
  gj_watch(
    insert: {
      name: "next_30m_orders"
      lifecycle: "ephemeral"
      lease_expires_at: "<future RFC3339 timestamp>"
      query: "subscription next_30m_orders { orders(first: 25, after: $cursor, order_by: { id: asc }) { id status } orders_cursor }"
    }
  ) {
    id
    lifecycle
    lease_expires_at
    status
  }
}
```

Expired ephemeral watches become `status: "expired"` and `enabled: false`. Their events remain until normal event retention removes them. Durable watches are never deleted just because an MCP client unsubscribes.

{{< verified by="TestWatchControlPlaneInitializesScopesAndUpdatesEvents" file="serv/watches_test.go" line="24" >}}
{{< verified by="TestEphemeralWatchLeaseLifecycleAndExpiry" file="serv/watches_test.go" line="1065" >}}
{{< verified by="TestUpsertWatchRejectsOversizedDefinitionJSON" file="serv/watches_test.go" line="1517" >}}

## The inbox loop

```graphql
query {
  gj_watch_event(
    where: { seen: { eq: false } }
    order_by: { created_at: desc }
    limit: 20
  ) {
    id
    watch_id
    data_json
    created_at
  }
}
```

Mark reviewed events seen with `gj_watch_event(update: { seen: true }, where: ...)`. To defer one without acknowledging it, set `snoozed_until` to a future RFC3339 timestamp:

```graphql
mutation {
  gj_watch_event(
    where: { id: { eq: "we:..." } }
    update: { snoozed_until: "2026-07-25T17:00:00Z" }
  ) {
    id
    seen
    snoozed_until
  }
}
```

Use `snoozed_until: null` to clear the snooze. A snooze-only update does not change `seen`; future-snoozed events leave unseen notices and resources, then passively re-enter when the timestamp lapses. Snoozes cannot outlive event retention.

In a multi-conversation MCP client, give each watch a unique name, retain the returned watch ID, filter this query to that ID, and never acknowledge another conversation's event. [Server-side agent](/agentic/server-agent/) responses carry a `watch_events_unseen` notice with the relevant `watch_ids`. At runtime, `query_catalog(id: "help:watches")` returns the full contract.

MCP clients can RFC 6570-expand `graphjin://watch-events/unseen/{watch_id}` for each retained watch, percent-encoding reserved characters in the ID, and subscribe to that concrete resource. GraphJin then sends `notifications/resources/updated` only to sessions subscribed to that watch; the notification URI identifies the watch, while a resource read returns its compact event metadata. The aggregate `graphjin://watch-events/unseen` resource remains available for clients without per-URI subscription support, but those clients must filter entries to their conversation's retained watch IDs before reading or acknowledging events. Full event payloads still come from `gj_watch_event`.

{{< verified by="TestWatchMCPPerWatchRoutingSameOwnerSessions" file="serv/watches_test.go" line="1571" >}}

## Rollup watches

A **rollup watch** is a durable watch whose subscription root is `gj_watch_event`. Use it for cross-watch correlation:

```graphql
mutation {
  gj_watch(
    insert: {
      name: "ops_rollup"
      query: "subscription($watch_ids: [String!], $gj_watch_event_cursor: Cursor) { gj_watch_event(where: { watch_id: { in: $watch_ids } }, first: 25, after: $gj_watch_event_cursor) { id watch_id data_json created_at } gj_watch_event_cursor }"
      variables_json: {
        watch_ids: ["watch:shipping", "watch:inventory"]
      }
    }
  ) {
    id
    status
    evidence_json
  }
}
```

The filter must be conjunctive and non-self with `watch_id` `eq` or `in`. `or` and `not` are rejected, a watch cannot reference itself, the dependency DAG rejects cycles, and saved-query dependency drift pauses the watch for review. Relevant query, variable, absence, digest, flow, or autonomous-delivery changes can invalidate the pinned action hash and require re-approval. Use digest for same-watch noise; use a rollup watch for correlation across watches.

## Durability

- **Restart-safe**: definitions, events, and subscription cursor checkpoints (`last_cursor_json`) are store rows; on boot the runner reloads every `enabled + active + approved` watch and resumes evaluation from the persisted cursor.
- **Downtime**: durable watches require cursor-capable subscriptions. The subscription cursor defines what is new after restart; `last_data_hash` remains only as a defensive idempotency guard so repeated payloads do not create duplicate inbox events.
- **Retention**: events are kept `event_retention_hours` (default 168) and capped at `max_events_per_watch` (default 500); event snapshots are capped at `snapshot_max_bytes` (default 32KB); flow and legacy agent enrichment share `enrichment_daily_cap` per watch per day (default 10).
- **Failure**: transient subscription or processing errors keep the watch active, increment `failure_count`, record `evidence_json.retry`, and exponentially back off up to five minutes. At `retry_max_failures` (default 5) the watch moves to terminal `status: "error"`; it is never auto-deleted. Deterministic definition errors are terminal immediately, while safety drift pauses for review.

{{< verified by="TestWatchRunnerPersistsEventsIdempotentlyAndNotices" file="serv/watches_test.go" line="251" >}}

## REST management and cleanup

GraphQL remains the source of truth, but REST wrappers mirror the owner/admin permissions for operators:

```text
GET    /api/v1/watches
POST   /api/v1/watches
PATCH  /api/v1/watches/{id}
DELETE /api/v1/watches/{id}
GET    /api/v1/watch-events/unseen
POST   /api/v1/watch-events/{id}/seen
POST   /api/v1/watches/cleanup-preview
POST   /api/v1/watches/cleanup-apply
```

Cleanup preview groups candidates by `expired_ephemeral`, `disabled_stale`, `errored_stale`, `orphaned_events`, `retention_events`, and `orphaned_saved_queries`. Cleanup apply requires the dry-run token plus explicit IDs (`watch_ids`, `event_ids`, `artifact_ids`) or allowed reason filters. Broad durable watch deletion is refused unless the caller selects watch IDs explicitly.

Creating a watch in dev mode registers its named subscription as a `saved_query` artifact for catalog discovery. Deleting the watch removes that artifact again unless another watch still references the same query name, and `orphaned_saved_queries` sweeps up subscription saved queries that no longer match any watch (rows left behind by older releases included). Saved queries with `query` or `mutation` operations are never cleanup candidates.

{{< verified by="TestWatchCleanupPreviewAndApplyScopesDurableDeletion" file="serv/watches_test.go" line="1142" >}}
{{< verified by="TestWatchRESTWrappersCRUDAndUnseenEvents" file="serv/watches_test.go" line="1210" >}}
{{< verified by="TestDeleteWatchRemovesRegisteredSavedQueryArtifact" file="serv/watches_test.go" line="965" >}}
{{< verified by="TestWatchCleanupSweepsOrphanedSavedQueryArtifacts" file="serv/watches_test.go" line="1023" >}}

## Delivery

Multiple replicas may evaluate the same watch, but fires and deliveries are deduplicated: event IDs are deterministic from `watch_id + data_hash`, plus the canonical flow hash when a flow is configured, so identical data reuses a verdict while a deliberate flow revision is evaluated again. Webhook/workflow delivery is claimed atomically so exactly one replica performs it. Webhooks get 3 attempts with a 10s timeout, an HMAC-SHA256 signature header, and an `Idempotency-Key`; targets must match the `watches.webhook_allow` allowlist (empty means all webhooks are denied).

Workflow and webhook delivery are autonomous actions. Creating or changing one pauses the watch with `action_approval: "pending"` and returns an `action_hash` that pins the query, variables, flow, delivery, and resolved workflow source. After the user confirms that exact proposal, approve it through the same root:

```graphql
mutation {
  gj_watch(
    where: { id: { eq: "watch:..." } }
    update: {
      action_review_json: {
        decision: "approve"
        expected_action_hash: "..."
      }
    }
  ) {
    id
    action_hash
    action_approval
    status
    enabled
  }
}
```

An agent must stop for confirmation after proposing the action; it cannot create or change the delivery and approve it in the same run. A relevant definition or workflow-source change invalidates approval and pauses the watch again.

{{< verified by="TestWatchDeliveryWebhookAllowlistSignatureAndStatus" file="serv/watches_test.go" line="396" >}}
