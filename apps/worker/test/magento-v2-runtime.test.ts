import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  executeGraphjinMutation,
  readMagentoEntity,
  isTerminalMagentoBulkStatus,
  magentoGraphjinMutationField,
} from "../src/packs/magento-v2-runtime.js";

const here = dirname(fileURLToPath(import.meta.url));
const runtimeSource = resolve(here, "../src/packs/magento-v2-runtime.ts");
const serviceSource = resolve(here, "../src/packs/service.ts");
const adminSource = resolve(here, "../src/packs/magento-admin.ts");

describe("Magento V2 GraphJin mutation aliases", () => {
  it("calls the expose_as field rather than the namespaced artifact identity", () => {
    expect(
      magentoGraphjinMutationField("magento_operator_v2_magento_update_product"),
    ).toBe("magento_update_product");
  });

  it("rejects roots that do not carry the reviewed V2 namespace", () => {
    expect(() => magentoGraphjinMutationField("magento_update_product")).toThrow(
      "invalid Magento V2 mutation root",
    );
  });

  it("waits while Magento reports an async operation as open", () => {
    expect(isTerminalMagentoBulkStatus(4)).toBe(false);
    expect([1, 2, 3, 5].every(isTerminalMagentoBulkStatus)).toBe(true);
  });

  it("keeps numeric risk labels out of operator errors, receipts, and admin responses", async () => {
    const source = [
      await readFile(runtimeSource, "utf8"),
      await readFile(serviceSource, "utf8"),
      await readFile(adminSource, "utf8"),
    ].join("\n");

    expect(source).not.toMatch(/\bclass\s*[012]\b/i);
    expect(source).toContain("complete it in Magento Admin");
    expect(source).toContain("requires approval from a human administrator");
    expect(source).toContain("executionMode");
    expect(source).toContain("automationEligible");
    expect(source).toContain("handoffOnly");
  });
});


vi.mock("@neko/llm/graphjin", async (importOriginal) => ({
  ...await importOriginal<typeof import("@neko/llm/graphjin")>(),
  graphjinQuery: vi.fn(),
  mintGraphjinToken: vi.fn(() => "test-token"),
}));

const runtime = {
  baseUrl: "https://magento.example", storeCode: "default",
  graphjinEndpoint: "https://graphjin.example", integrationToken: "test-token",
};
const operation = {
  operationId: "saveSourceItems", mutationRoot: "magento_operator_v2_magento_save_source_items",
  readPath: "/V1/inventory/source-items", bodyKey: "sourceItems",
  defaultClass: 1 as const, entityType: "source_item", reversible: true,
};
const item = { sku: "sku-with-two-sources", source_code: "warehouse", quantity: 21, status: 1 };
const legacyQuery = { "searchCriteria[filter_groups][0][filters][0][value]": "wrong-sku" };

afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks(); });

describe("Magento inventory read and write boundaries", () => {
  it("selects the body's exact SKU and source, independently of legacy read filters", async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ items: [item] }));
    vi.stubGlobal("fetch", fetchMock);
    expect(await readMagentoEntity({ runtime, operation, path: {}, query: legacyQuery,
      body: { sourceItems: [item] } })).toEqual(item);
    const url = new URL(String(fetchMock.mock.calls[0][0]));
    for (const [index, field] of ["sku", "source_code"].entries()) {
      const prefix = `searchCriteria[filter_groups][${index}][filters][0]`;
      expect(url.searchParams.get(`${prefix}[field]`)).toBe(field);
      expect(url.searchParams.get(`${prefix}[value]`)).toBe(item[field as "sku" | "source_code"]);
      expect(url.searchParams.get(`${prefix}[condition_type]`)).toBe("eq");
    }
  });

  it("treats an empty inventory collection as absent for delete reconciliation", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ items: [] })));
    expect(await readMagentoEntity({ runtime, operation, path: {}, query: {},
      body: { sourceItems: [item] } })).toBeNull();
  });

  it.each([
    { items: [item, item] },
    { items: [{ ...item, source_code: "default" }] },
    { items: [{ ...item, sku: "other-sku" }] },
    {},
  ])("rejects an ambiguous, unrelated, or malformed provider response: %j", async (response) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(response)));
    await expect(readMagentoEntity({ runtime, operation, path: {}, query: {},
      body: { sourceItems: [item] } })).rejects.toThrow("Magento source-item read");
  });

  it.each([[[]], [[item, item]], [[{ sku: item.sku }]]])("rejects rows without exactly one complete inventory identity: %j", async (items) => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await expect(readMagentoEntity({ runtime, operation, path: {}, query: {},
      body: { sourceItems: items } })).rejects.toThrow("exactly one source item");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it.each(["save", "delete"])("never forwards read filters to the %s mutation", async (verb) => {
    const { graphjinQuery } = await import("@neko/llm/graphjin");
    const field = `magento_${verb}_source_items`;
    vi.mocked(graphjinQuery).mockResolvedValue({ data: { [field]: { ok: true, status_code: 200 } } });
    const input = {
      runtime, operation: { ...operation, mutationRoot: `magento_operator_v2_${field}` },
      request: { orgId: "test-org" } as Parameters<typeof executeGraphjinMutation>[0]["request"],
      role: "magento_ops_executor" as const, path: {}, query: legacyQuery,
      body: { sourceItems: [item] },
    };
    await executeGraphjinMutation(input);
    expect(vi.mocked(graphjinQuery).mock.calls[0][0].variables).toEqual({
      call: { path: { storeCode: "default" }, body: input.body },
    });
  });
});

const orderOperation = {
  ...operation, operationId: "magentoUpdateOrder", entityType: "order",
  readPath: "/V1/orders/{id}", bodyKey: "entity",
  mutationRoot: "magento_operator_v2_magento_update_order",
  numericPathParams: ["id"],
};

describe("Magento order repository updates", () => {
  it("uses the provider's order save route and keeps the order ID in the body", async () => {
    const { parse } = await import("yaml");
    const spec = parse(await readFile(resolve(here, "../../../packs/magento/graphjin/specs/magento-operator-v2.yaml"), "utf8"));
    expect(spec.paths["/rest/{storeCode}/V1/orders"].post.operationId).toBe("magentoUpdateOrder");
    expect(spec.paths["/rest/{storeCode}/V1/orders/{id}"].put).toBeUndefined();
    const { graphjinQuery } = await import("@neko/llm/graphjin");
    vi.mocked(graphjinQuery).mockResolvedValue({ data: { magento_update_order: { ok: true, status_code: 200 } } });
    const body = { entity: { entity_id: 63, customer_note: "Updated note" } };
    await executeGraphjinMutation({ runtime, operation: orderOperation, path: { id: 63 }, body,
      role: "magento_sensitive_executor",
      request: { orgId: "test-org" } as Parameters<typeof executeGraphjinMutation>[0]["request"],
    });
    expect(vi.mocked(graphjinQuery).mock.calls[0][0].variables).toEqual({
      call: { path: { storeCode: "default" }, body },
    });
  });

  it.each([undefined, 64, 0])("rejects missing or mismatched body identity %j before reading or writing", async (entity_id) => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const input = { runtime, operation: orderOperation, path: { id: 63 }, query: {},
      body: { entity: { entity_id } }, role: "magento_sensitive_executor" as const,
      request: { orgId: "test-org" } as Parameters<typeof executeGraphjinMutation>[0]["request"],
    };
    await expect(readMagentoEntity(input)).rejects.toThrow("entity.entity_id to match");
    await expect(executeGraphjinMutation(input)).rejects.toThrow("entity.entity_id to match");
    const { graphjinQuery } = await import("@neko/llm/graphjin");
    expect(fetchMock).not.toHaveBeenCalled();
    expect(graphjinQuery).not.toHaveBeenCalled();
  });

  it("rejects an absent order instead of allowing repository save to create one", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 404 })));
    await expect(readMagentoEntity({ runtime, operation: orderOperation, path: { id: 63 },
      query: {}, body: { entity: { entity_id: 63 } },
    })).rejects.toThrow("requires an existing order");
  });
});

describe("Magento provider-specific reconciliation", () => {
  it("checks category parent and sibling position", async () => {
    const { reconciliationConfirmed: confirm } = await import("../src/packs/magento-v2-runtime.js");
    const op = { ...operation, operationId: "magentoMoveCategory", entityType: "category" };
    expect(confirm(op, { parent_id: 2, position: 1 }, { parentId: 2, afterId: 0, position: 1 })).toBe(true);
    expect(confirm(op, { parent_id: 3, position: 1 }, { parentId: 2, position: 1 })).toBe(false);
    expect(confirm(op, { parent_id: 2, position: 2 }, { parentId: 2, position: 1 })).toBe(false);
  });
  it("normalizes only Magento's coupon enum and customer timestamp", async () => {
    const { reconciliationConfirmed: confirm } = await import("../src/packs/magento-v2-runtime.js");
    expect(confirm({ ...operation, entityType: "sales_rule" }, { coupon_type: "SPECIFIC_COUPON", discount_amount: 5 }, { coupon_type: 2, discount_amount: 5 })).toBe(true);
    expect(confirm({ ...operation, entityType: "sales_rule" }, { coupon_type: "NO_COUPON" }, { coupon_type: 2 })).toBe(false);
    expect(confirm({ ...operation, entityType: "customer" }, { firstname: "New", updated_at: "later" }, { firstname: "New", updated_at: "earlier" })).toBe(true);
    expect(confirm({ ...operation, entityType: "customer" }, { firstname: "Wrong" }, { firstname: "New" })).toBe(false);
  });
  it.each(["magentoCreateInvoice", "magentoCreateShipment"])("verifies the created %s document's order and item quantities", async (operationId) => {
    const { reconciliationConfirmed: confirm } = await import("../src/packs/magento-v2-runtime.js");
    const op = { ...operation, operationId, entityType: "fulfillment" };
    const expected = { order_id: 63, items: [{ order_item_id: 189, qty: 1 }], notify: false };
    expect(confirm(op, { entity_id: 8, ...expected }, expected)).toBe(true);
    expect(confirm(op, { ...expected, order_id: 64 }, expected)).toBe(false);
    expect(confirm(op, { order_id: 63, items: [{ order_item_id: 189, qty: 2 }] }, expected)).toBe(false);
    expect(confirm(op, { order_id: 63, items: [] }, expected)).toBe(false);
  });
});

describe("Magento cooldown SQL binding", () => {
  it.each([["one"], ["one", "quote'and,comma"]].map((refs) => [refs]))("binds each entity reference as a scalar: %j", async (refs) => {
    const { magentoCooldownQuery } = await import("../src/packs/magento-v2-runtime.js");
    const query = magentoCooldownQuery("org", refs, 3600).toQuery({
      escapeName: (name) => `"${name}"`, escapeParam: (index) => `$${index + 1}`,
      escapeString: (value) => `'${value.replaceAll("'", "''")}'`,
    });
    expect(query.params).toEqual(["org", ...refs, 3600]);
    expect(query.sql).toContain("r.entity_ref IN (");
    expect(query.sql).not.toContain("quote'and,comma");
  });
});
