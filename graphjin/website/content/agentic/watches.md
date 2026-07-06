---
title: "Watches"
description: "Standing questions stored in gj_watch, evaluated with the owner's permissions, delivering fired events to a durable gj_watch_event inbox, webhooks, or workflows."
nav_group: "agentic"
doc_kind: "guide"
weight: 47
---

A watch is a standing question: "tell me when a roast batch fails QC twice in a week." GraphJin stores the definition under `gj_watch`, evaluates it as a governed subscription **with the owner's stored identity and role**, and writes fired events to the owner's `gj_watch_event` inbox. Optional delivery fans out to webhooks or workflows, and a read-only agent enrichment can attach a short summary of what happened.

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

A watch needs a `name` and either a subscription `query` or a `saved_query_name`:

```graphql
mutation {
  gj_watch(
    insert: {
      name: "new_orders"
      query: "subscription new_orders { orders { id status } }"
    }
  ) {
    id
    status
  }
}
```

Pause and resume via `status` / `enabled` updates; remove with `gj_watch(delete)`. Optional fields: `variables_json`, `condition_js` (a predicate over the result), `delivery_json` (webhook/workflow targets), and `enrich_json` (read-only agent summary). User-supplied JSON fields are size-capped at `snapshot_max_bytes`.

{{< verified by="TestWatchControlPlaneInitializesScopesAndUpdatesEvents" file="serv/watches_test.go" line="24" >}}
{{< verified by="TestUpsertWatchRejectsOversizedDefinitionJSON" file="serv/watches_test.go" line="843" >}}

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

Mark reviewed events seen with `gj_watch_event(update: { seen: true }, where: ...)`. [Server-side agent](/agentic/server-agent/) responses carry a `watch_events_unseen` notice whenever the caller has unreviewed events — the cue to run exactly this loop. At runtime, `query_catalog(id: "help:watches")` returns the full contract.

## Durability

- **Restart-safe**: definitions, events, and the fire cursor (`last_data_hash`) are store rows; on boot the runner reloads every `enabled + active + approved` watch and resumes evaluation.
- **Downtime**: a state change that happened while the server was down still fires on the first re-evaluation after boot, because the persisted hash no longer matches. Only a transient change that also reverted during the outage is missed; there is no historical replay.
- **Retention**: events are kept `event_retention_hours` (default 168) and capped at `max_events_per_watch` (default 500); event snapshots are capped at `snapshot_max_bytes` (default 32KB); agent enrichment is capped at `enrichment_daily_cap` per watch per day (default 10).
- **Failure**: a broken watch flips to `status: "error"` with `last_error` and a growing `failure_count`; it is never auto-disabled or deleted — pause it explicitly.

{{< verified by="TestWatchRunnerPersistsEventsIdempotentlyAndNotices" file="serv/watches_test.go" line="226" >}}

## Delivery

Multiple replicas may evaluate the same watch, but fires and deliveries are deduplicated: event IDs are deterministic (`watch_id + data_hash`) so duplicate inserts collapse on the primary key, and webhook/workflow delivery is claimed atomically so exactly one replica performs it. Webhooks get 3 attempts with a 10s timeout, an HMAC-SHA256 signature header, and an `Idempotency-Key`; targets must match the `watches.webhook_allow` allowlist (empty means all webhooks are denied).

{{< verified by="TestWatchDeliveryWebhookAllowlistSignatureAndStatus" file="serv/watches_test.go" line="396" >}}
