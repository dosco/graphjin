---
name: magento-review-performance
description: Review Magento sales and operating performance using the pack's governed metrics. Use for daily or weekly briefings, revenue and order trends, store or product comparisons, and questions such as "how is the store doing?"; use a diagnostic Magento skill for a specific order, refund, fulfillment, inventory, cron, or indexer incident.
license: Apache-2.0
metadata:
  openneko:
    tags: [magento, commerce, sales, revenue, metrics, briefing]
    category: commerce
    requires_toolsets: [graphjin]
    related_skills: [magento-investigate-order, magento-triage-fulfillment, magento-investigate-refunds, magento-check-inventory, magento-diagnose-platform-health]
---

# Review Magento performance

Build an evidence-backed store briefing from the installed Magento metrics.
Treat `magento_analytics` as read-only and preserve the pack's checked-in
metric definitions instead of silently inventing alternative formulas.

## Establish the reporting frame

Before interpreting values, resolve and state:

- the Magento store scope or selected store IDs;
- the configured base currency;
- the Magento timezone; and
- a half-open reporting interval `[from, to)`.

Use the user's requested comparison period. If none is requested, report the
current period without manufacturing a trend. For a comparison, use an
equal-length immediately preceding period with the same store scope. Read
[scope and time](references/scope-and-time.md) when resolving store, currency,
or local-day boundaries.

## Gather evidence

Prefer the installed saved queries and their metric snapshots:

- commercial result: `net_invoiced_revenue`, `orders_placed`, and
  `average_order_value`;
- leakage: `refund_rate` and `cancellation_rate`;
- mix: `sales_by_store` and `top_products`;
- operating context for a full briefing: `fulfillment_backlog`,
  `fulfillment_age`, `stock_status`, `cron_health`, `indexer_health`, and
  `data_freshness`.

Read [metric definitions](references/metric-definitions.md) before calculating
or explaining a shipped metric. When a snapshot is stale, refresh or query the
same saved-query definition and disclose the observation time. Do not replace a
missing or null metric with zero.

## Interpret without overclaiming

- Separate order placement, invoicing, credit memos, cancellation, and
  shipment. They measure different lifecycle events.
- Treat a change as a fact only when both periods use identical scope and
  definitions. Show absolute values alongside percentages when the sample is
  small.
- Describe a store or SKU as associated with a result, not as its cause, unless
  direct evidence establishes causality.
- Do not report conversion rate without a connected traffic/session source.
- Do not report gross margin without a validated and sufficiently complete cost
  source.
- Do not query or expose customer, address, payment, credential, OAuth, admin,
  or password data.

## Deliver the briefing

Lead with the reporting frame and a compact scorecard. Then identify the few
material drivers, name the single highest-priority issue supported by the
evidence, and recommend the next diagnostic task. Route detailed follow-up to
the related Magento skill rather than expanding this reporting skill into an
incident investigation.
