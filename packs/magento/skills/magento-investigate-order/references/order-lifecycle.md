# Magento order lifecycle and reconciliation

Sales documents use an entity header plus item rows:

- `sales_order` / `sales_order_item`: commercial intent and current order
  state.
- `sales_invoice` / `sales_invoice_item`: invoiced value and quantity.
- `sales_shipment` / `sales_shipment_item`: fulfillment evidence.
- `sales_creditmemo` / `sales_creditmemo_item`: credited or refunded value and
  quantity.

Join each document header to its order, and each document item through the
relationship overlay's `order_id` or `parent_id`. Prefer parent order items when
configurable or bundle children would otherwise double-count value or quantity.

An order status is a workflow label; it is not proof that a payment, invoice,
shipment, or refund document exists. Cancellation is an order state and is not
the same as a credit memo. A credit memo proves Magento recorded a credit, but
does not by itself prove the payment processor settled money externally.

For each relevant line, reconcile ordered, cancelled, invoiced, shipped, and
refunded quantities. Keep base-currency values separate from display-currency
values. Use the configured Magento timezone when presenting timestamps.
