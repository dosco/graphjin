# Fulfillment eligibility and quantities

An order is not fulfilled merely because it is paid or invoiced. Shipment
documents are the fulfillment evidence.

At parent-item level, the pack's backlog quantity is:

```text
qty_invoiced - qty_shipped - qty_refunded
```

The shipped `fulfillment_backlog` aggregate intentionally does not filter by
terminal order state because that relationship filter is unreliable through
the supported MariaDB path. Use the saved query unchanged for the metric, but
join candidate items to their order and remove cancelled, closed, and complete
orders from the human action queue.

Use invoice and shipment item documents to verify any surprising order-item
quantity. Configurable and bundle children can double-count; prefer parent
items unless the investigation explicitly needs component detail.

Show age in the configured Magento timezone and preserve the query timestamp.
An SLA breach requires an operator-confirmed threshold; a long age without a
threshold is an old order, not automatically a breach.
