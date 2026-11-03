import { describe, expect, it } from "vitest";

import { conditionLabel, metricEventSummary, numericPaths } from "./MetricWatches";

describe("metric watches", () => {
  it("offers every number in a query result as a metric path", () => {
    const paths = numericPaths({ orders: [{ count_id: 3, sum_total: "12.50", status: "open" }] });
    expect(paths.map((p) => p.path)).toEqual(["orders.0.count_id", "orders.0.sum_total"]);
  });

  it("offers grouped rows as a per-group metric", () => {
    const paths = numericPaths({ orders: [{ region: "east", avg_hours: 30 }, { region: "west", avg_hours: 50 }] });
    expect(paths).toContainEqual({ path: "orders", key: "region", value: "avg_hours", sample: "2 groups" });
  });

  it("summarizes fired events in one line", () => {
    const metric = { path: "customer_tickets.0.count_id", op: "gt", threshold: 50 };
    expect(conditionLabel(metric)).toBe("count_id > 50");
    expect(metricEventSummary({ kind: "metric", metric, value: 57, previous: 44 })).toBe("57 (was 44) · count_id > 50");
    expect(
      metricEventSummary({
        kind: "metric",
        metric: { path: "orders", key: "region", value: "avg_hours", op: "gt", threshold: 48 },
        crossings: [{ key: "east", value: 60 }],
      })
    ).toBe("east 60 · avg_hours per region > 48");
    expect(metricEventSummary({ kind: "schedule" })).toBe("");
  });
});
