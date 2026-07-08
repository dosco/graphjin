---
title: "Watches"
description: "Cursor-backed standing questions stored in gj_watch, evaluated with the owner's permissions, delivering fired events to a durable gj_watch_event inbox, webhooks, workflows, or MCP resource notices."
nav_group: "agentic"
doc_kind: "guide"
weight: 47
---

A watch is a standing question: "tell me when a roast batch fails QC twice in a week." GraphJin stores the definition under `gj_watch`, evaluates it as a governed cursor-backed subscription **with the owner's stored identity and role**, and writes fired events to the owner's `gj_watch_event` inbox. Optional delivery fans out to webhooks or workflows, and a read-only agent enrichment can attach a short summary of what happened.

Watches never elevate access: a watch can only ever see what its owner could already query, and both roots are owner-scoped — callers see only their own watches and events.

## Enable

Watches persist through the [artifact store](/agentic/artifacts/), so both blocks are required:

```yaml
artifacts:
  enabled: true
  source: app
watches:
  enabled: true
  runner: "all" # default "off": definitions persist, nothing evaluates
```

`runner` is per-replica: set `"all"` on the replicas that should evaluate. The `recipe.config.enable_watches` catalog recipe walks an agent through this change; when watches are disabled, the roots are not advertised to callers at all.

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

Pause and resume via `status` / `enabled` updates; remove with `gj_watch(delete)`. Optional fields: `variables_json`, `condition_js` (a predicate over the result), `delivery_json` (webhook/workflow targets), and `enrich_json` (read-only agent summary). User-supplied JSON fields are size-capped at `snapshot_max_bytes`.

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

Mark reviewed events seen with `gj_watch_event(update: { seen: true }, where: ...)`. [Server-side agent](/agentic/server-agent/) responses carry a `watch_events_unseen` notice whenever the caller has unreviewed events - the cue to run exactly this loop. At runtime, `query_catalog(id: "help:watches")` returns the full contract.

MCP clients can also subscribe to `graphjin://watch-events/unseen`. GraphJin sends `notifications/resources/updated` when that caller has matching unseen events, and a resource read returns compact metadata only: event IDs, watch IDs, timestamps, hashes, truncation flags, and delivery status. Full event payloads still come from `gj_watch_event`.

{{< verified by="TestWatchMCPUnseenResourceAndSubscriptionNotification" file="serv/watches_test.go" line="1337" >}}

## Durability

- **Restart-safe**: definitions, events, and subscription cursor checkpoints (`last_cursor_json`) are store rows; on boot the runner reloads every `enabled + active + approved` watch and resumes evaluation from the persisted cursor.
- **Downtime**: durable watches require cursor-capable subscriptions. The subscription cursor defines what is new after restart; `last_data_hash` remains only as a defensive idempotency guard so repeated payloads do not create duplicate inbox events.
- **Retention**: events are kept `event_retention_hours` (default 168) and capped at `max_events_per_watch` (default 500); event snapshots are capped at `snapshot_max_bytes` (default 32KB); agent enrichment is capped at `enrichment_daily_cap` per watch per day (default 10).
- **Failure**: a broken watch flips to `status: "error"` with `last_error` and a growing `failure_count`; it is never auto-deleted. Existing non-cursor watches move to error until updated to a cursor-backed subscription.

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

Cleanup preview groups candidates by `expired_ephemeral`, `disabled_stale`, `errored_stale`, `orphaned_events`, and `retention_events`. Cleanup apply requires the dry-run token plus explicit IDs or allowed reason filters. Broad durable watch deletion is refused unless the caller selects watch IDs explicitly.

{{< verified by="TestWatchCleanupPreviewAndApplyScopesDurableDeletion" file="serv/watches_test.go" line="1142" >}}
{{< verified by="TestWatchRESTWrappersCRUDAndUnseenEvents" file="serv/watches_test.go" line="1210" >}}

## Delivery

Multiple replicas may evaluate the same watch, but fires and deliveries are deduplicated: event IDs are deterministic (`watch_id + data_hash`) so duplicate inserts collapse on the primary key, and webhook/workflow delivery is claimed atomically so exactly one replica performs it. Webhooks get 3 attempts with a 10s timeout, an HMAC-SHA256 signature header, and an `Idempotency-Key`; targets must match the `watches.webhook_allow` allowlist (empty means all webhooks are denied).

{{< verified by="TestWatchDeliveryWebhookAllowlistSignatureAndStatus" file="serv/watches_test.go" line="396" >}}
