import { describe, expect, it } from "vitest";
import { structuredTurnSummary } from "../src/work/structured-turn-summary";

describe("structuredTurnSummary", () => {
  it("keeps validated headline figures in the next turn's transcript", () => {
    expect(structuredTurnSummary({ vitals: [{
      label: "Affected orders",
      value: "12",
      sub: "open",
      basis: "observed",
      source: "GraphJin",
    }] })).toContain("Affected orders: 12 (open; observed; source: GraphJin)");
  });

  it("saves nothing when the turn left no findings", () => {
    expect(structuredTurnSummary(null)).toBe("");
    expect(structuredTurnSummary(null, [], ["", "  "])).toBe("");
  });

  it("keeps the assistant's interim text beside its cards", () => {
    const summary = structuredTurnSummary(null, [{
      version: "v1.0",
      createSurface: {
        surfaceId: "tools",
        components: [{ id: "root", component: "Answer", title: "Tool number to order relationship" }],
      },
    }], ["Tool numbers and orders are one-to-many.", "Tool numbers and orders are one-to-many."]);
    expect(summary).toBe([
      "Structured result from my previous turn:",
      "- Tool numbers and orders are one-to-many.",
      "- Tool number to order relationship",
    ].join("\n"));
  });

  it("preserves visible card findings and bounded table data for a follow-up", () => {
    const summary = structuredTurnSummary(null, [{
      version: "v1.0",
      createSurface: {
        surfaceId: "orders",
        components: [
          { id: "root", component: "Answer", title: "Late order analysis" },
          { id: "finding", component: "Callout", title: "Main finding", text: "Twelve orders are late." },
          { id: "rows", component: "Table", columns: [{ key: "order", label: "Order" }, { key: "days", label: "Days late" }], rows: [{ order: "SO-100", days: 4 }] },
        ],
      },
    }]);
    expect(summary).toContain("Late order analysis");
    expect(summary).toContain("Twelve orders are late.");
    expect(summary).toContain("order: SO-100, days: 4");
    expect(summary).not.toContain("surfaceId");
  });

  it("keeps chart points and data-model-backed table rows in the next turn", () => {
    const summary = structuredTurnSummary(null, [{
      version: "v1.0",
      createSurface: {
        surfaceId: "trend",
        dataModel: {
          series: [{ d: "Sep 7", v: 42 }, { d: "Sep 14", v: 47 }],
          orders: [{ order: "SO-100", days: 4 }],
          privateNote: "not displayed",
        },
        components: [
          { id: "root", component: "Answer", title: "Weekly orders", children: ["figures", "chart", "table"] },
          { id: "figures", component: "KeyFigures", items: [{ label: "Open orders", value: 12, source: "GraphJin" }] },
          { id: "chart", component: "Chart", title: "Orders by week", valueLabel: "orders", data: { path: "/series" } },
          { id: "table", component: "Table", columns: [{ key: "order", label: "Order" }, { key: "days", label: "Days late" }], rows: { path: "/orders" } },
        ],
      },
    }]);
    expect(summary).toContain("Sep 7: 42 orders");
    expect(summary).toContain("Sep 14: 47 orders");
    expect(summary).toContain("Open orders: 12 (GraphJin)");
    expect(summary).toContain("order: SO-100, days: 4");
    expect(summary).not.toContain("privateNote");
    expect(summary).not.toContain("not displayed");
  });
});
