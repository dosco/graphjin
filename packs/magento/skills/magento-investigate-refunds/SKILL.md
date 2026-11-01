---
name: magento-investigate-refunds
description: Investigate Magento refund-value or cancellation spikes, reconcile credit memos, and locate affected stores, orders, or SKUs. Use for refund-rate changes, cancellation anomalies, credit-memo questions, or the refund/cancellation watcher; use magento-investigate-order for a single known order.
license: Apache-2.0
metadata:
  openneko:
    tags: [magento, refunds, cancellations, credit-memo, revenue, investigation]
    category: commerce
    requires_toolsets: [graphjin]
    related_skills: [magento-investigate-order, magento-review-performance, magento-triage-fulfillment]
---

# Investigate Magento refunds and cancellations

Determine what changed and where it is concentrated without conflating
Magento lifecycle events or claiming unsupported causality. Treat
`magento_analytics` as read-only.

## Fix the comparison frame

Resolve store IDs, base currency, Magento timezone, and a half-open current
interval `[from, to)`. For a spike investigation, use the configured baseline
or an equal-length immediately preceding interval with identical scope. If no
baseline is available, describe the current level without calling it a spike.

Read [refund and cancellation semantics](references/refunds-and-cancellations.md)
before interpreting results.

## Measure the change

- Use `refund_rate` for refunded base value divided by invoiced base value.
- Use `cancellation_rate` for cancelled orders divided by orders placed.
- Use `net_invoiced_revenue` to show the commercial effect of credit memos.
- Use `sales_by_store` to locate store-level concentration.
- Use `watch_refund_cancellation` when explaining a watcher event.

Show numerator and denominator with each rate, particularly for sparse stores
or short windows. A high percentage based on one order is materially different
from the same percentage at scale.

## Trace the concentration

When the aggregate change is material, inspect credit memo headers/items and
cancelled orders using approved fields only. Break down by store, order ID, SKU,
credited base value, and quantity when the data supports it. Prefer parent
items where product structures would double-count.

Do not expose customer identity, addresses, contact data, payment or processor
details, credentials, tokens, or admin data. Do not use correlation with a SKU,
store, or status as proof of root cause. State which operational evidence would
be needed to confirm the cause.

## Report and hand off

Return the comparison frame, current and baseline values, absolute change,
concentrated stores/orders/SKUs, data limitations, and the highest-value next
check. Use `magento-investigate-order` for a selected order timeline.

If the user asks to issue a refund, approve a return, change a financial
setting, or exceed the store-credit cap, use `magento.financial_handoff` to
record the exact entity, evidence, and a draft for a human Magento Admin
operator. This action creates an audit artifact only and has no external
execution path. State that final completion must be confirmed in Magento.

Boundary: Never issue or reverse a refund, approve a return, edit a credit memo, or change financial configuration; create a `magento.financial_handoff` and leave execution to a human in Magento Admin.
