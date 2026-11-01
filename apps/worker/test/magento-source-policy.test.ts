import { describe, expect, it } from "vitest";
import {
  MAGENTO_READ_TABLES,
  magentoGraphjinTables,
} from "../src/packs/magento-source-policy.js";

describe("Magento GraphJin source policy", () => {
  const tables = magentoGraphjinTables("m2_", MAGENTO_READ_TABLES);
  const table = (name: string) => tables.find((value) => value.name === name);

  it("exposes operational customer, address, order-history, and payment tables", () => {
    expect(tables.map((value) => value.name)).toEqual(
      expect.arrayContaining([
        "customer_entity",
        "customer_address_entity",
        "sales_order_address",
        "sales_order_status_history",
        "sales_order_payment",
      ]),
    );
    expect(table("customer_address_entity")).not.toHaveProperty("blocklist");
    expect(table("sales_order_address")).not.toHaveProperty("blocklist");
  });

  it("does not block customer PII or ordinary operational fields", () => {
    expect(table("sales_order")?.blocklist).toEqual(["protect_code"]);
    expect(table("sales_order")?.blocklist).not.toEqual(
      expect.arrayContaining([
        "customer_email",
        "customer_firstname",
        "customer_lastname",
        "customer_id",
        "remote_ip",
      ]),
    );
    expect(table("sales_order_item")).not.toHaveProperty("blocklist");
    expect(table("sales_shipment")).not.toHaveProperty("blocklist");
  });

  it("still blocks credentials, access tokens, and raw payment payloads", () => {
    expect(table("customer_entity")?.blocklist).toEqual(
      expect.arrayContaining(["password_hash", "rp_token", "confirmation"]),
    );
    expect(table("customer_entity")?.blocklist).not.toContain("email");
    expect(table("sales_order_payment")?.blocklist).toEqual(
      expect.arrayContaining([
        "cc_number_enc",
        "additional_information",
        "cc_debug_request_body",
        "echeck_routing_number",
      ]),
    );
    expect(table("sales_order_payment")?.blocklist).not.toEqual(
      expect.arrayContaining(["method", "cc_last_4", "last_trans_id"]),
    );
  });

  it("renders physical table prefixes without changing GraphJin identities", () => {
    expect(table("customer_entity")).toMatchObject({
      name: "customer_entity",
      table: "m2_customer_entity",
      source: "magento_analytics",
      database: "magento_analytics",
      read_only: true,
    });
  });

  it("allows read-only product text attributes such as descriptions and dimensions", () => {
    expect(table("eav_attribute")).toMatchObject({
      table: "m2_eav_attribute",
      source: "magento_analytics",
      read_only: true,
    });
    expect(table("catalog_product_entity_text")).toMatchObject({
      table: "m2_catalog_product_entity_text",
      source: "magento_analytics",
      read_only: true,
    });
  });
});
