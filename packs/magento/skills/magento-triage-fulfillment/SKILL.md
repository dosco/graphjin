---
name: magento-triage-fulfillment
description: Triage Magento paid or invoiced orders that remain unshipped, quantify backlog and aging, and prepare a prioritized fulfillment exception list. Use for shipping SLA breaches, fulfillment backlog, partially shipped orders, or watcher findings; use magento-investigate-order for one already-identified order.
license: Apache-2.0
metadata:
  openneko:
    tags: [magento, fulfillment, shipping, backlog, sla, operations]
    category: commerce
    requires_toolsets: [graphjin]
    related_skills: [magento-investigate-order, magento-check-inventory, magento-review-performance]
---

# Triage Magento fulfillment

Identify orders that need fulfillment attention, rank them using observable
lifecycle evidence, and prepare a governed order action when explicitly asked.

## Define the queue

Resolve the store scope, Magento timezone, lookback window, and the user's SLA
threshold. If no SLA is supplied, report ages without labeling any order
overdue. Use order IDs and operational timestamps only; do not retrieve customer
identity, address, contact, payment, or credential data.

Read [fulfillment rules](references/fulfillment-rules.md) before reconciling
quantities.

## Measure, then investigate

1. Use `fulfillment_backlog` for the installed aggregate quantity definition.
2. Use `fulfillment_age` for median and oldest age among paid non-terminal
   orders in the configured lookback.
3. Use `watch_aged_fulfillment` when evaluating the watcher threshold.
4. For the exception list, query candidate orders and parent items, then
   reconcile invoiced, shipped, refunded, and cancelled quantities. Do not
   assume the aggregate backlog and age queries have identical populations.

Exclude cancelled, closed, and complete orders from the actionable queue after
checking their actual documents. For a partially shipped order, report the
remaining quantity by SKU. Do not treat order age alone as proof of a warehouse
failure; payment review, stock allocation, holds, and external fulfillment can
require checks outside the analytics allowlist.

## Prioritize exceptions

Rank with transparent evidence, normally:

1. age beyond the confirmed SLA;
2. paid/invoiced quantity still outstanding;
3. partial shipment or lifecycle inconsistency; and
4. repeated concentration by SKU or store.

Do not invent customer value, shipping promise dates, or priority fields that
the source does not expose. Label any heuristic as a heuristic.

## Deliver the handoff

Return the reporting frame, aggregate backlog and age, a bounded exception list
with order ID/store/age/quantities, the reason each item was selected, and the
next check. Use `magento-investigate-order` for a deeper single-order timeline
and `magento-check-inventory` for a suspected stock constraint.

For an explicitly requested operational change, draft `magento.manage_orders`
with the resolved numeric order ID, named store scope, stable idempotency key,
and the smallest supported operation. Shipping, invoicing, cancellation, and
order edits require a human administrator. Hold, unhold, and a
private internal comment remain reconciled change-sets. Never retry an
ambiguous result.

Boundary: Never refund, approve a return, alter stock, or call Magento through SQL, raw GraphQL, or raw REST; fulfillment writes only use an approved `magento.manage_orders` change-set.
