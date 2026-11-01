# Shipped Magento metric definitions

Use these definitions exactly when the user names a pack metric:

- **Net invoiced revenue:** invoiced base value minus refunded base value.
- **Orders placed:** distinct orders created in the period, including orders
  later cancelled.
- **Average order value:** net invoiced revenue divided by distinct orders in
  the period whose invoiced base value is positive; null for a zero
  denominator.
- **Refund rate:** refunded base value divided by invoiced base value. It is a
  value rate, not a count of refunded orders.
- **Cancellation rate:** orders whose Magento state is `canceled` divided by
  orders placed.
- **Fulfillment backlog:** parent-item invoiced quantity less shipped and
  refunded quantity. The shipped aggregate intentionally lacks a terminal-order
  filter because that relationship filter is unreliable in the supported
  GraphJin/MariaDB path; do not describe it as an exact count of open orders.
- **Fulfillment age:** median and oldest age of paid, non-terminal orders in the
  selected lookback window.
- **Sales by store:** net invoiced base value grouped by store.
- **Top products:** invoiced item value and quantity by parent-item SKU. Refunds
  are not subtracted from this ranking.
- **Stock status:** source-level quantity approximation, not Magento salable
  quantity.
- **Cron health:** failed jobs plus running jobs older than the configured
  limit.
- **Indexer health:** non-valid or stale indexer state.
- **Data freshness:** age of newest order and successful cron evidence, plus the
  metric snapshot observation time.

Do not silently reinterpret null as zero. State numerator, denominator, and
small-sample caveats for rates when they affect the conclusion.
