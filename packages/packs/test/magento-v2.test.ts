import { describe, expect, it } from "vitest";
import {
  classifyMagentoChange,
  evaluateMagentoCaps,
  isMagentoHandoffOnlyOperation,
  magentoExecutionMode,
} from "../src/magento-v2.js";

describe("Magento V2 classifier", () => {
  it("requires approval for an unreviewed writable attribute", () => {
    const result = classifyMagentoChange({
      domain: "catalog",
      entityType: "product",
      operationId: "magentoUpdateProduct",
      defaultClass: 2,
      body: { product: { name: "Known", future_magento_attribute: "unknown" } },
    });
    expect(result.riskClass).toBe(1);
    expect(result.reasons).toContain(
      "Unreviewed attribute future_magento_attribute requires administrator approval",
    );
  });

  it("keeps reviewed reversible content eligible to run automatically within limits", () => {
    const result = classifyMagentoChange({
      domain: "content",
      entityType: "cms",
      operationId: "magentoUpdateCmsPage",
      defaultClass: 2,
      body: { page: { title: "Shipping", content: "Updated content", is_active: true } },
    });
    expect(result.riskClass).toBe(2);
    expect(result.attributes.every((attribute) => attribute.reviewed)).toBe(true);
  });

  it("recognizes handoff-only operation names before attribute routing", () => {
    expect(isMagentoHandoffOnlyOperation("magentoOnlineRefund")).toBe(true);
    expect(isMagentoHandoffOnlyOperation("magentoApproveReturn")).toBe(true);
    expect(isMagentoHandoffOnlyOperation("magentoUpdateTaxConfig")).toBe(true);
    expect(classifyMagentoChange({
      domain: "orders",
      entityType: "order",
      operationId: "magentoOnlineRefund",
      defaultClass: 1,
      body: {},
    }).riskClass).toBe(0);
  });

  it("maps internal risk values to operator-facing execution modes", () => {
    expect(magentoExecutionMode(0)).toBe("handoff_only");
    expect(magentoExecutionMode(1)).toBe("approval_required");
    expect(magentoExecutionMode(2)).toBe("controlled_automation_eligible");
  });
});

describe("Magento V2 caps", () => {
  it("requires approval for a price move beyond the automatic-execution limit", () => {
    const result = evaluateMagentoCaps({
      domain: "catalog",
      riskClass: 2,
      rows: [{
        beforeImage: { price: 100 },
        afterImage: { product: { price: 125 } },
      }],
      caps: { maxPriceDeltaPercent: 10 },
      now: new Date("2026-08-28T00:00:00.000Z"),
    });
    expect(result.allowed).toBe(true);
    expect(result.riskClass).toBe(1);
    expect(result.escalations[0]).toMatch(/25\.00%/);
  });

  it("rejects a near-free promotion and missing expiry", () => {
    const result = evaluateMagentoCaps({
      domain: "promotions",
      riskClass: 1,
      rows: [{ afterImage: { rule: { discount_amount: 100 } } }],
      projectedExposure: 100,
    });
    expect(result.allowed).toBe(false);
    expect(result.riskClass).toBe(0);
    expect(result.violations).toEqual(expect.arrayContaining([
      expect.stringMatching(/free or near-free/i),
      "Promotion expiry is required",
    ]));
  });

  it("stops an automatic action at the daily cap or entity cooldown", () => {
    const result = evaluateMagentoCaps({
      domain: "inventory",
      riskClass: 2,
      rows: [{ afterImage: { sourceItems: [{ sku: "SKU-1", quantity: 4 }] } }],
      autoExecution: true,
      autoActionsToday: 5,
      cooldownBreaches: ["SKU-1:default"],
      caps: { maxDailyAutoActions: 5 },
    });
    expect(result.allowed).toBe(false);
    expect(result.violations).toEqual(expect.arrayContaining([
      expect.stringMatching(/daily automatic-action cap/i),
      "Cooldown is still active for SKU-1:default",
    ]));
  });

  it("does not coerce a missing exposure to zero", () => {
    expect(evaluateMagentoCaps({
      domain: "catalog",
      riskClass: 2,
      rows: [{ afterImage: { product: { name: "A" } } }],
      projectedExposure: null,
    }).projectedExposure).toBeNull();
  });
});

describe("Promotion operation caps", () => {
  it.each(["magentoDeleteSalesRule", "magentoGenerateCoupons"])("does not require rule dates on %s", (operationId) => {
    expect(evaluateMagentoCaps({ domain: "promotions", operationId, riskClass: 1,
      rows: [{ afterImage: { couponSpec: { quantity: 2 } } }],
    }).allowed).toBe(true);
  });
  it("still enforces coupon limits without requiring expiry", () => {
    expect(evaluateMagentoCaps({ domain: "promotions", operationId: "magentoGenerateCoupons", riskClass: 1,
      rows: [{ afterImage: { couponSpec: { quantity: 1000000 } } }],
    }).violations).toEqual([expect.stringContaining("Coupon count exceeds")]);
  });
  it("uses existing expiry for partial rule updates but rejects removal", () => {
    const base = { domain: "promotions" as const, operationId: "magentoUpdateSalesRule", riskClass: 1 as const };
    expect(evaluateMagentoCaps({ ...base, rows: [{ beforeImage: { to_date: "2026-10-01" }, afterImage: { rule: { description: "Updated" } } }] }).allowed).toBe(true);
    expect(evaluateMagentoCaps({ ...base, rows: [{ beforeImage: { to_date: "2026-10-01" }, afterImage: { rule: { to_date: "" } } }] }).violations).toContain("Promotion expiry is required");
  });
});
