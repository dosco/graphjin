/**
 * Magento data that is available to authenticated, read-only GraphJin callers.
 *
 * Customer and order PII is operational store data. It is intentionally
 * available here; authorization comes from the OpenNeko actor and the
 * dedicated SELECT-only Magento connection. Only credentials, access tokens,
 * and raw payment payloads are denied unconditionally.
 */
export const MAGENTO_READ_TABLES = [
  "sales_order",
  "sales_order_item",
  "sales_order_address",
  "sales_order_status_history",
  "sales_order_payment",
  "sales_invoice",
  "sales_invoice_item",
  "sales_creditmemo",
  "sales_creditmemo_item",
  "sales_shipment",
  "sales_shipment_item",
  "customer_entity",
  "customer_address_entity",
  "eav_attribute",
  "catalog_product_entity",
  "catalog_product_entity_decimal",
  "catalog_product_entity_int",
  "catalog_product_entity_text",
  "catalog_product_entity_varchar",
  "catalog_category_product",
  "inventory_source_item",
  "inventory_reservation",
  "cataloginventory_stock_item",
  "store",
  "store_group",
  "store_website",
  "cron_schedule",
  "indexer_state",
] as const;

export type MagentoReadTable = (typeof MAGENTO_READ_TABLES)[number];

/** Fields that must never cross the Magento database source boundary. */
export const MAGENTO_SECRET_COLUMN_BLOCKLIST: Partial<
  Record<MagentoReadTable, readonly string[]>
> = {
  // Used as a bearer-like secret by Magento's guest order lookup flow.
  sales_order: ["protect_code"],
  customer_entity: [
    "password_hash",
    "rp_token",
    "rp_token_created_at",
    "confirmation",
  ],
  // Keep useful payment status, method, amount, last-four, and transaction
  // references available while denying credentials and opaque gateway blobs.
  sales_order_payment: [
    "additional_data",
    "additional_information",
    "cc_number_enc",
    "cc_secure_verify",
    "cc_debug_request_body",
    "cc_debug_response_body",
    "cc_debug_response_serialized",
    "echeck_routing_number",
  ],
};

export function magentoGraphjinTables(
  prefix: string,
  available: readonly string[],
): Record<string, unknown>[] {
  return MAGENTO_READ_TABLES.filter((name) => available.includes(name)).map((name) => {
    const blockedColumns = MAGENTO_SECRET_COLUMN_BLOCKLIST[name];
    return {
      name,
      table: `${prefix}${name}`,
      source: "magento_analytics",
      // GraphJin 3.18.42 normalizes sources before it parses newly supplied
      // table entries in the same config patch. Keep the derived database
      // explicit so reload validation routes the table to the new source.
      database: "magento_analytics",
      read_only: true,
      ...(blockedColumns ? { blocklist: [...blockedColumns] } : {}),
    };
  });
}
