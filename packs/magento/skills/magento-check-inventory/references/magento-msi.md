# Magento Multi-Source Inventory

Magento MSI separates several concepts:

- `inventory_source_item.quantity` is physical quantity recorded for one SKU
  at one source.
- `inventory_source_item.status` enables or disables that source item.
- `inventory_reservation.quantity` records reservation deltas associated with
  sales events.
- Stocks map sales channels to sources, and indexed configuration determines
  the quantity Magento considers salable.
- Legacy `cataloginventory_stock_item` settings can still influence behavior,
  including manage-stock and backorder rules.

The pack's shipped stock metric intentionally reports source-level quantity.
Always label it as an approximation, not salable quantity. A correct salable
calculation requires the installation's stock/source assignments, reservation
semantics, configuration, and supported Magento version to pass a specific
preflight.

For a replenishment proposal, show SKU, source, observed quantity/status,
threshold, and observation time. Keep any reservation evidence aggregated and
do not expose reservation metadata unless the operator explicitly needs a
non-personal event reference.
