---
name: magento-check-inventory
description: Investigate Magento low stock, stockouts, source quantities, reservations, and suspected MSI availability discrepancies, then prepare a replenishment handoff. Use for SKU or source availability questions, stock-threshold watcher findings, and inventory constraints; do not use for sales-only product rankings.
license: Apache-2.0
metadata:
  openneko:
    tags: [magento, inventory, msi, stock, reservations, replenishment]
    category: commerce
    requires_toolsets: [graphjin]
    related_skills: [magento-triage-fulfillment, magento-investigate-order, magento-review-performance]
---

# Check Magento inventory

Explain source-level stock evidence, prepare a reviewed correction, and use
the governed inventory action only when the user asks to change source items.

When someone asks to change stock, first verify the exact SKU, source code,
current quantity/status, intended quantity/status, store scope, and the MSI
caveats below. Draft `magento.manage_inventory` with `source_items_save` or
`source_items_delete`, a stable idempotency key, and one bounded row per source
item. Show the before image and proposed diff. Do not claim success until the
receipt is `applied`; surface `reconcile_required` without retrying.

Read [Magento MSI](references/magento-msi.md) before using the words
"available" or "salable."

## Establish the inventory question

Resolve the requested SKU(s), source code(s), configured low-stock threshold,
and observation time. When responding to a watcher, use the threshold that
actually triggered it. Do not infer a threshold from a low quantity.

## Gather evidence

1. Use `stock_status` for the installed source-quantity approximation.
2. Use `watch_stock_threshold` when validating a watcher count.
3. Query `inventory_source_item` for SKU, source, quantity, and enabled status.
4. Inspect aggregate reservations or legacy stock configuration only when the
   question requires it and the installation exposes the needed stock mapping.
5. Correlate with fulfillment exceptions only to identify potential impact;
   do not claim stock caused an order delay without order-level evidence.

Avoid reservation metadata or other fields that could reveal order/customer
context when aggregate quantity is sufficient. Never retrieve customer,
address, payment, credential, token, password, or admin data.

## Classify carefully

- `status = 0` means the source item is disabled/out of stock at that source.
- A low physical source quantity is not automatically low salable quantity.
- Reservations, stock-to-source assignment, backorders, manage-stock settings,
  and Magento version-specific index logic can change storefront availability.
- Do not calculate a definitive salable quantity by simply adding reservations
  unless the installation's stock mapping and formula have been validated.

## Deliver the result or proposal

Return a bounded list containing SKU, source, observed quantity/status,
threshold, observation time, and the MSI approximation warning. Separate
confirmed source-level stockouts from suspected salability discrepancies.
When no change was requested, provide a replenishment/configuration check. When
a change was requested, provide the governed change-set or its execution
receipt and approval state.

Boundary: Never change reservations, backorders, global stock configuration, or inventory through SQL, raw GraphQL, raw REST, `curl`, or a terminal; only `magento.manage_inventory` may change approved source items.
