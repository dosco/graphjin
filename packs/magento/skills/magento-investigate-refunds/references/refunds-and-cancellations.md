# Refund and cancellation semantics

Magento records different events in different places:

- `sales_invoice` is invoiced commerce value.
- `sales_creditmemo` is credited/refunded value recorded in Magento.
- `sales_creditmemo_item` ties credited quantities and value to order items.
- `sales_order.state = canceled` is an order-state outcome.

A cancellation is not a refund. An order may be cancelled before capture and
therefore have no credit memo. An order may have a partial credit memo without
being cancelled. A credit memo proves Magento recorded the credit; it does not
prove the external payment processor completed settlement.

The pack's refund rate is value-based:

```text
sum(sales_creditmemo.base_grand_total)
------------------------------------------------
sum(sales_invoice.base_grand_total)
```

The cancellation rate is count-based:

```text
orders in state canceled / orders placed
```

Use document timestamps in the configured Magento timezone and base-currency
columns. Keep current and baseline windows identical, and show a null rate when
the denominator is zero.
