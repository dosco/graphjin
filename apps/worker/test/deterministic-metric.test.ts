import { describe, expect, it } from "vitest";
import { buildSavedQueryVariables, mapSavedQueryMetric } from "../src/jobs/deterministic-metric.js";

function definition(result: Record<string, unknown>, extra: Record<string, unknown> = {}) {
  return {
    title: "Net revenue",
    description: "Reviewed metric",
    calculationNote: "Invoice less refund.",
    chartHint: "metric",
    unit: "currency",
    directionGood: "up",
    execution: {
      mode: "saved_query",
      source: "magento_analytics",
      query: "net_invoiced_revenue",
      document: "query { sales_invoice { sum_base_grand_total } }",
      result,
      freshnessSeconds: 3600,
      runtime: { storeIds: [2, 3], currency: "USD", windowDays: 7, ...extra },
    },
  };
}

describe("deterministic saved-query metrics", () => {
  it("builds bounded variables without an LLM", () => {
    const now = new Date("2026-08-20T12:00:00.000Z");
    expect(buildSavedQueryVariables(definition({ kind: "scalar", path: "orders.0.count" }), now)).toMatchObject({
      from: "2026-08-13T12:00:00.000Z",
      to: "2026-08-20T12:00:00.000Z",
      storeIds: [2, 3],
      threshold: 5,
    });
  });

  it("evaluates only the allowlisted arithmetic expression", () => {
    const mapped = mapSavedQueryMetric({
      definition: definition({ kind: "scalar", expression: { subtract: ["invoice.0.total", "refund.0.total"] } }),
      data: { invoice: [{ total: "120.50" }], refund: [{ total: 20.5 }] },
      baseline: 90,
      now: new Date("2026-08-20T12:00:00.000Z"),
    });
    expect(mapped.value).toBe(100);
    expect(mapped.result.headlineMetric).toBe("$100.00");
    expect(mapped.result.chartData).toEqual([{ d: "Current", v: 100, t: 90 }]);
    expect(mapped.result.mood).toBe("good");
  });

  it("returns null for a zero denominator", () => {
    const mapped = mapSavedQueryMetric({
      definition: definition({ kind: "scalar", expression: { divide: ["a.0.value", "b.0.value"] } }),
      data: { a: [{ value: 10 }], b: [{ value: 0 }] },
    });
    expect(mapped.value).toBeNull();
    expect(mapped.result.headlineMetric).toBe("Not yet available");
    expect(mapped.result.mood).toBe("watch");
  });

  it("supports nested arithmetic used by average order value", () => {
    const mapped = mapSavedQueryMetric({
      definition: definition({
        kind: "scalar",
        expression: {
          divide: [
            { subtract: ["invoice.0.total", "refund.0.total"] },
            "orders.0.count",
          ],
        },
        zeroDenominator: null,
      }),
      data: {
        invoice: [{ total: 125 }],
        refund: [{ total: 25 }],
        orders: [{ count: 4 }],
      },
    });
    expect(mapped.value).toBe(25);
    expect(mapped.result.headlineMetric).toBe("$25.00");
  });

  it("does not report missing freshness timestamps as zero seconds old", () => {
    const mapped = mapSavedQueryMetric({
      definition: definition({ kind: "timestamps", paths: ["orders.0.latest"] }),
      data: { orders: [] },
      now: new Date("2026-08-20T12:00:00Z"),
    });
    expect(mapped.value).toBeNull();
    expect(mapped.result.headlineMetric).toBe("Not yet available");
  });

  it("maps keyed store totals and timestamp-age summaries", () => {
    const stores = mapSavedQueryMetric({
      definition: definition({
        kind: "keyed_subtract",
        left: { rowsPath: "invoice", keyPath: "store_id", valuePath: "total" },
        right: { rowsPath: "refund", keyPath: "store_id", valuePath: "total" },
      }),
      data: { invoice: [{ store_id: 1, total: 100 }, { store_id: 2, total: 50 }], refund: [{ store_id: 1, total: 25 }] },
    });
    expect(stores.value).toBe(125);
    expect(stores.result.chartData).toEqual([{ d: "1", v: 75 }, { d: "2", v: 50 }]);

    const ages = mapSavedQueryMetric({
      definition: definition({ kind: "timestamp_age_summary", rowsPath: "orders", timestampPath: "created_at" }),
      data: { orders: [{ created_at: "2026-08-18T12:00:00Z" }, { created_at: "2026-08-16T12:00:00Z" }] },
      now: new Date("2026-08-20T12:00:00Z"),
    });
    expect(ages.value).toBe(3 * 86_400);
    expect(ages.valueJson).toEqual({ median: 3 * 86_400, max: 4 * 86_400, count: 2 });
  });

  it("rejects unreviewed expression operators", () => {
    expect(() => mapSavedQueryMetric({
      definition: definition({ kind: "scalar", expression: { eval: ["a", "b"] } }),
      data: { a: 1, b: 2 },
    })).toThrow(/operator eval is not allowed/);
  });
});
