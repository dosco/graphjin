---
name: shopify-orders
description: Read Shopify orders and request approved changes to an order's internal note or tags through the Shopify pack. Use when the operator asks about recent orders, one order's detail, order counts, or wants to flag an order with a note or tag.
license: Apache-2.0
metadata:
  hermes:
    tags: [shopify, commerce, orders]
    category: commerce
    requires_toolsets: [graphjin, pack-actions]
---

# Shopify orders

The pack reaches one store, the one its access token belongs to, through the
Shopify Admin REST API version 2026-01.

## Read orders

Use GraphJin reads. Write arguments as literals.

| GraphJin query | Use it to |
|---|---|
| `shopify_list_orders(status: "open", financial_status: "paid", created_at_min: "2026-10-01T00:00:00Z") { orders { id name email financial_status fulfillment_status total_price currency created_at tags note } }` | List recent orders; filter by `status`, `financial_status`, `fulfillment_status`, `created_at_min`/`created_at_max`, `updated_at_min`, or `name` |
| `shopify_get_order(order_id: 1234567890) { order { id name note tags customer { email first_name last_name } line_items { title sku quantity price } refunds fulfillments shipping_address } }` | Read one order in full |
| `shopify_count_orders(status: "any", created_at_min: "2026-10-01T00:00:00Z") { count }` | Count orders |

- `status` defaults to `open` in Shopify. Use `any` to include closed and
  cancelled orders.
- A list returns Shopify's default page of 50 orders, newest first. For older
  orders, narrow the dates, or page with `since_id` set to the last id you saw.
- The saved query `shopify_open_orders` lists open orders.
- `order_id` is the numeric id, not the `#1001` name. To find an order by its
  name, list with `name: "1001"`.

## Change a note or tags

Request the `shopify.order` action with `operation: update_order_note`. The
note is internal: Shopify shows it in admin only, never to the customer.

```json
{
  "intent": "Flag order #1001 for review: the shipping address failed verification.",
  "payload": {
    "operation": "update_order_note",
    "path": { "order_id": 1234567890 },
    "body": { "order": { "id": 1234567890, "note": "Address failed verification. Hold for review.", "tags": "vip, review" } }
  }
}
```

- `body.order` takes `id` (the same id as `path.order_id`) and `note`, `tags`,
  or both. No other order field can change through this pack.
- Both fields replace what is there. **To append**, read the order first,
  then send the old note plus the new line, or the old tags plus the new ones.
  Show the old and new values in the intent.
- `tags` is one comma-separated string.

Every change waits for an administrator's approval.
