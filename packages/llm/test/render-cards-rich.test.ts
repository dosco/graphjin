import { describe, expect, it } from "vitest";
import { buildCardsSection } from "../src/work/prompt";
import {
  CORE_RENDER_CARDS_INPUT_SCHEMA,
  RENDER_CARDS_INPUT_SCHEMA,
  validateRenderCardsInput,
} from "../src/work/a2ui-contract";

const CATEGORIES = "Category | Units sold | Revenue (USD)\nMountain Bikes | 1740 | 2323674.45\nRoad Bikes | 1822 | 1848406.70\nTouring Bikes | 1845 | 2089567.66";

function surface(input: Record<string, unknown>) {
  const result = validateRenderCardsInput(input, "answer-test");
  if (!result.success) throw new Error(JSON.stringify(result.issues));
  const [message] = result.messages as Array<{ createSurface: { components: Array<Record<string, unknown>> } }>;
  return Object.fromEntries(message.createSurface.components.map((component) => [component.id, component]));
}

function issues(input: Record<string, unknown>) {
  const result = validateRenderCardsInput(input);
  expect(result.success).toBe(false);
  return result.success ? [] : result.issues.map((issue) => issue.message);
}

describe("render_cards rich answers", () => {
  it("lays table rows out side by side and carries the drill-down question", () => {
    const parts = surface({ title: "Bike categories", layout: "compare", table: CATEGORIES, drillDown: "Break down {label} by month" });
    expect(parts.table).toMatchObject({
      component: "Compare",
      drill: "Break down {label} by month",
      rows: [
        { c0: "Mountain Bikes", c1: "1,740", c2: "2,323,674" },
        { c0: "Road Bikes", c1: "1,822", c2: "1,848,407" },
        { c0: "Touring Bikes", c1: "1,845", c2: "2,089,568" },
      ],
    });
  });

  it("asks for 2 to 6 rows in a comparison", () => {
    const one = "Category | Revenue (USD)\nMountain Bikes | 2323674";
    expect(issues({ title: "One", layout: "compare", table: one })).toContain("compare needs 2 to 6 table rows; use the report layout for more.");
  });

  it("builds what-if controls whose results recompute from names", () => {
    const parts = surface({
      title: "Road price change",
      controls: ["price_change: Road bike price change (%) = 5 (-20 to 20, step 1)"],
      values: ["road_rev = 1,496,791.67", "other_rev = 4532859.65"],
      computed: ["Total revenue (USD): road_rev * (1 + price_change / 100) + other_rev"],
    });
    expect(parts.whatIf).toEqual({
      id: "whatIf",
      component: "WhatIf",
      controls: [{ name: "price_change", label: "Road bike price change", unit: "%", value: 5, min: -20, max: 20, step: 1 }],
      values: { road_rev: 1_496_791.67, other_rev: 4_532_859.65 },
      outputs: [{ label: "Total revenue", unit: "USD", expression: "road_rev * (1 + price_change / 100) + other_rev" }],
    });
  });

  it("builds a choice control whose chosen number feeds the formulas", () => {
    const parts = surface({
      title: "Prepreg cover",
      controls: ["panels: Panels = 20 (1 to 40, step 1)", "alt: Alternative = No swap (0) | Panasonic Megtron 6 (12) | Isola Astra MT77 (0)"],
      values: ["per_panel = 3", "stock = 29"],
      computed: ["Still short: max(0, panels * per_panel - stock - alt)"],
    });
    expect((parts.whatIf as { controls: unknown[] }).controls[1]).toEqual({
      name: "alt", label: "Alternative", value: 0, min: 0, max: 12, step: 1,
      options: [{ label: "No swap", value: 0 }, { label: "Panasonic Megtron 6", value: 12 }, { label: "Isola Astra MT77", value: 0 }],
    });
    expect(issues({ title: "x", controls: ["alt: Alternative = No swap | Megtron 6 (12)"], computed: ["Out: alt"] }))
      .toEqual(['controls.0 needs each choice in the form "Option (number)", separated by |.']);
  });

  it("opens a row's own sub-table from rowDetails", () => {
    const parts = surface({
      title: "Quote materials",
      table: "Material | Needed | In stock | Status\nPrepreg 1086 | 60 | 29 | Short by 31\nCore 0.006 | 60 | 199 | Available",
      rowDetails: ["Prepreg 1086 >> Alternative | Approval | In stock\nMegtron 6 | Preferred | 12\nAstra MT77 | Preferred | 0"],
    });
    expect((parts.table as { details: Record<string, unknown> }).details).toEqual({
      "Prepreg 1086": {
        columns: [{ key: "c0", label: "Alternative" }, { key: "c1", label: "Approval" }, { key: "c2", label: "In stock", align: "right" }],
        rows: [{ c0: "Megtron 6", c1: "Preferred", c2: "12" }, { c0: "Astra MT77", c1: "Preferred", c2: "0" }],
      },
    });
    expect(issues({ title: "x", table: "A | B\nx | 1", rowDetails: ["y >> C | D\n1 | 2"] }))
      .toEqual(['rowDetails.0 needs "Row name >> pipe table", where the row name is a first-column cell of table.']);
  });

  it("names the problem in a what-if the browser cannot compute", () => {
    expect(issues({ title: "x", controls: ["p: P = 5 (0 to 10)"], computed: ["Out: p * missing"] }))
      .toEqual(["computed.0 uses missing; name each one in controls or values."]);
    expect(issues({ title: "x", controls: ["p: P = 50 (0 to 10)"], computed: ["Out: p"] }))
      .toEqual(["controls.0 needs its default between min and max."]);
    expect(issues({ title: "x", controls: ["p: P = 5 (0 to 10)"], computed: ["Out: alert(p)"] }))
      .toEqual(["computed.0: unknown function alert."]);
    expect(issues({ title: "x", controls: ["p: P = 5 (0 to 10)"] }))
      .toContain("controls and computed go together: the controls move, the computed results follow.");
  });

  it("plots places with coordinates on a map", () => {
    const parts = surface({
      title: "Sales by territory",
      map: ["Southwest @ 34.05, -112.07: 1121473", "France @ 46.6, 2.35: 519,114"],
      mapValue: "Sales (USD)",
      drillDown: "Show top customers in {label}",
    });
    expect(parts.map).toEqual({
      id: "map",
      component: "AnswerMap",
      title: "Sales by territory",
      valueLabel: "Sales (USD)",
      drill: "Show top customers in {label}",
      points: [
        { label: "Southwest", lat: 34.05, lon: -112.07, v: 1_121_473 },
        { label: "France", lat: 46.6, lon: 2.35, v: 519_114 },
      ],
    });
    expect(issues({ title: "x", map: ["Nowhere @ 95, 10: 5"] })[0]).toContain("latitude -90 to 90");
  });

  it("draws a diagram from arrows and keeps each step once", () => {
    const parts = surface({
      title: "Order flow",
      diagram: ["Customer -> Sales order: places", "Sales order -> Shipment", "sales order -> Invoice: bills", "Returns"],
    });
    expect(parts.diagram).toEqual({
      id: "diagram",
      component: "Diagram",
      title: "Order flow",
      nodes: [
        { id: "Customer", label: "Customer" },
        { id: "Sales order", label: "Sales order" },
        { id: "Shipment", label: "Shipment" },
        { id: "Invoice", label: "Invoice" },
        { id: "Returns", label: "Returns" },
      ],
      edges: [
        { from: "Customer", to: "Sales order", label: "places" },
        { from: "Sales order", to: "Shipment" },
        { from: "Sales order", to: "Invoice", label: "bills" },
      ],
    });
  });

  it("accepts an offline tool and refuses one that loads from the network", () => {
    const html = "<input id=a value=2><output id=o></output><script>o.textContent = a.value * 2</script>";
    expect(surface({ title: "Doubler", tool: html }).tool).toEqual({ id: "tool", component: "AnswerTool", title: "Doubler", html });
    const refusal = "tool runs offline inside the answer; put every figure it needs in the fragment, and load, open, or navigate to nothing outside it.";
    expect(issues({ title: "x", tool: '<script src="https://cdn.example.com/x.js"></script>' })).toEqual([refusal]);
    expect(issues({ title: "x", tool: "<script>fetch('/api/x')</script>" })).toEqual([refusal]);
    expect(issues({ title: "x", tool: "<script>new WebSocket('ws://x')</script>" })).toEqual([refusal]);
    expect(issues({ title: "x", tool: "<script>location.href = '/leak?d=' + data</script>" })).toEqual([refusal]);
  });

  it("marks a draft answer", () => {
    const parts = surface({ title: "Early figures", stage: "draft", keyFigures: ["Revenue: $6.26M"] });
    expect(parts.root).toMatchObject({ component: "Answer", stage: "draft" });
    expect(surface({ title: "Done", stage: "final", keyFigures: ["Revenue: $6.26M"] }).root).not.toHaveProperty("stage");
  });

  it("needs {label} in a drill-down question", () => {
    expect(issues({ title: "x", table: CATEGORIES, drillDown: "Tell me more" }))
      .toEqual(["drillDown needs {label} where the item's name goes."]);
  });

  it("teaches only examples the contract accepts", () => {
    const examples = [...buildCardsSection(true, true).matchAll(/^\{.*\}$/gm)].map((match) => JSON.parse(match[0]));
    expect(examples.length).toBeGreaterThanOrEqual(7);
    for (const example of examples) {
      expect(validateRenderCardsInput(example, "x", { rich: true }).success, example.title).toBe(true);
    }
  });

  it("offers the rich fields only in the full schema", () => {
    const full = Object.keys((RENDER_CARDS_INPUT_SCHEMA as { properties: object }).properties);
    const core = Object.keys((CORE_RENDER_CARDS_INPUT_SCHEMA as { properties: object }).properties);
    expect(full).toEqual(expect.arrayContaining(["layout", "drillDown", "controls", "computed", "map", "diagram", "tool", "stage"]));
    expect(core).toEqual(["title", "stage", "table", "chart", "chartColumn", "keyFigures", "callout", "mood", "followUps"]);
  });
});
