# Shopify solution pack

This pack reads Shopify orders and sets an order's internal note and tags
through the Shopify Admin REST API (version 2026-01), as a GraphJin API source.
It replaces the `@open-neko/plugin-shopify` plugin: no plugin code runs inside
OpenNeko.

## Configure

1. In Shopify admin, open Settings, Apps and sales channels, Develop apps.
   Create an app with the `read_orders` and `write_orders` Admin API scopes,
   install it, and copy the Admin API access token (`shpat_…`).
2. Open Admin, Settings, Packs and install Shopify.
3. Set `shopify.store_url` to the store's myshopify.com address, such as
   `https://acme.myshopify.com`, and paste the token. GraphJin seals it in its
   keystore. The install check lists open orders with it.
4. Review and apply the configuration.
5. Enable **Shopify governed order changes**. It installs disabled and asks an
   administrator to approve each change.

## Coverage

| Plugin action | Pack equivalent |
|---|---|
| `list_shopify_orders` | `shopify_list_orders` read and the `shopify_open_orders` saved query |
| `get_shopify_order` | `shopify_get_order` read |
| `update_shopify_order_note` | `shopify.order` action, operation `update_order_note` |

The pack adds `shopify_count_orders`. The update accepts only `note` and `tags`.

The plugin appended to a note and merged tags by reading the order first, in
code. The action now sends the final note and tags; the skill reads the order
and composes them, and the approval shows the result.

The store address is an input, so the pack cannot pin one vendor host. Enter
the myshopify.com address, not a custom storefront domain.
