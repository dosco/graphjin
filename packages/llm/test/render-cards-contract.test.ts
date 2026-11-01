import { describe, expect, it } from "vitest";
import {
  normalizeRenderCardsInput,
  RENDER_CARDS_INPUT_SCHEMA,
  validateRenderCardsInput,
} from "../src/work/a2ui-contract";
import { RENDER_CARDS_DESCRIPTION } from "../src/work/render-catalog";
import { portableSchemaIssues } from "../src/tool-schema-portability";

const quarters = {
  title: "Revenue over the last four quarters",
  table: [
    "| Quarter | Revenue (USD) | Orders |",
    "| --- | --- | --- |",
    "| 2025 Q4 | 12,699,845.92 | 5571 |",
    "| 2026 Q1 | 11565433.28 | 6161 |",
  ].join("\n"),
  chart: "line",
  chartColumn: "Revenue (USD)",
  keyFigures: ["2026 Q1: $11.57M (down 9% on Q4)", "Orders: 6,161"],
  callout: "Q1 revenue fell while orders rose.",
  followUps: ["Break 2026 Q1 down by region"],
};

function components(input: unknown) {
  const result = validateRenderCardsInput(input, "answer-test");
  if (!result.success) throw new Error(JSON.stringify(result.issues));
  const [message] = result.messages as Array<{ version: string; createSurface: Record<string, unknown> }>;
  expect(message.version).toBe("v1.0");
  expect(message.createSurface).toMatchObject({
    surfaceId: "answer-test",
    catalogId: "urn:openneko:catalog:work:v2",
  });
  return message.createSurface.components as Array<Record<string, unknown>>;
}

function issues(input: unknown) {
  const result = validateRenderCardsInput(input);
  expect(result.success).toBe(false);
  return result.success ? [] : result.issues.map((issue) => issue.message);
}

describe("render_cards contract", () => {
  it("builds key figures, a chart drawn from the table, the table, a callout and follow-ups", () => {
    const [root, figures, chart, table, callout, followUps] = components(quarters);
    expect(root).toEqual({
      id: "root",
      component: "Answer",
      title: "Revenue over the last four quarters",
      children: ["keyFigures", "chart", "table", "callout", "followUps"],
    });
    expect(figures).toEqual({
      id: "keyFigures",
      component: "KeyFigures",
      items: [
        { label: "2026 Q1", value: "$11.57M", sub: "down 9% on Q4" },
        { label: "Orders", value: "6,161" },
      ],
    });
    expect(chart).toEqual({
      id: "chart",
      component: "Chart",
      type: "line",
      title: "Revenue over the last four quarters",
      valueLabel: "Revenue (USD)",
      data: [
        { d: "2025 Q4", v: 12_699_845.92 },
        { d: "2026 Q1", v: 11_565_433.28 },
      ],
    });
    expect(table).toEqual({
      id: "table",
      component: "Table",
      columns: [
        { key: "c0", label: "Quarter" },
        { key: "c1", label: "Revenue (USD)", align: "right" },
        { key: "c2", label: "Orders", align: "right" },
      ],
      rows: [
        { c0: "2025 Q4", c1: "12,699,846", c2: "5,571" },
        { c0: "2026 Q1", c1: "11,565,433", c2: "6,161" },
      ],
    });
    expect(callout).toEqual({ id: "callout", component: "Callout", mood: "watch", text: "Q1 revenue fell while orders rose." });
    expect(followUps).toEqual({
      id: "followUps",
      component: "Choice",
      options: [{ label: "Break 2026 Q1 down by region", prompt: "Break 2026 Q1 down by region" }],
    });
  });

  it("charts the first column of plain numbers when chartColumn is absent", () => {
    const [, chart] = components({
      title: "Tickets",
      table: "Priority | Owner | Tickets\nHigh | Ana | 18\nLow | Raj | 102",
      chart: "donut",
    });
    expect(chart).toMatchObject({ type: "donut", valueLabel: "Tickets", data: [{ d: "High", v: 18 }, { d: "Low", v: 102 }] });
  });

  it("keeps formatted cells as written in columns that are not plain numbers", () => {
    const [, table] = components({ title: "Revenue", table: "Region | Revenue\nNorth | $1.2M\nSouth | $0.9M" });
    expect(table).toMatchObject({
      columns: [{ key: "c0", label: "Region" }, { key: "c1", label: "Revenue" }],
      rows: [{ c0: "North", c1: "$1.2M" }, { c0: "South", c1: "$0.9M" }],
    });
  });

  it("names the row and column when a charted cell is not a plain number", () => {
    expect(issues({ title: "Revenue", table: "Region | Revenue\nNorth | 1200000\nSouth | $0.9M", chart: "bar", chartColumn: "Revenue" }))
      .toEqual(["table row 2 needs a plain number in the Revenue column, such as 12699845.92."]);
  });

  it("lists the value columns when chartColumn names none of them", () => {
    expect(issues({ title: "Revenue", table: "Region | Revenue\nNorth | 1\nSouth | 2", chart: "bar", chartColumn: "Sales" }))
      .toEqual(["chartColumn must be one of the table's value columns: Revenue."]);
  });

  it("reads a literal backslash-n as a line break", () => {
    const [, chart] = components({ title: "Orders", table: "Day | Orders\\nMon | 3\\nTue | 5", chart: "bar" });
    expect(chart).toMatchObject({ component: "Chart", data: [{ d: "Mon", v: 3 }, { d: "Tue", v: 5 }] });
  });

  it("requires one cell per column and a table for a chart", () => {
    expect(issues({ title: "Orders", table: "A | B\n1 | 2\n3" })).toEqual(["table row 2 needs 2 cells, one per column."]);
    expect(issues({ title: "Orders", chart: "line", keyFigures: ["Orders: 3"] })).toEqual(["chart draws from table; add a table."]);
  });

  it("requires a donut to have 2 to 8 nonnegative parts", () => {
    const rows = Array.from({ length: 9 }, (_, index) => `p${index} | ${index + 1}`).join("\n");
    expect(issues({ title: "Mix", table: `Part | Share\n${rows}`, chart: "donut" }))
      .toEqual(["a donut needs 2 to 8 rows with nonnegative values."]);
  });

  it("requires key figures in the form Label: value", () => {
    expect(issues({ title: "Orders", keyFigures: ["214 orders"] })).toEqual(['keyFigures.0 needs the form "Label: value".']);
  });

  it("requires at least one section", () => {
    expect(issues({ title: "Orders" })).toEqual(["Fill at least one of table, keyFigures, callout, followUps, computed, map, diagram, tool."]);
  });

  it("recovers answers from the call shapes a tool bridge produces", () => {
    const answer = { title: "Orders", keyFigures: ["Orders: 214"] };
    expect(normalizeRenderCardsInput({ name: "render_cards", arguments: answer })).toEqual(answer);
    expect(normalizeRenderCardsInput({ arguments: JSON.stringify(answer) })).toEqual(answer);
    expect(normalizeRenderCardsInput({ cards: answer })).toEqual(answer);
    expect(normalizeRenderCardsInput({ ...answer, chart: null, table: false, callout: "", followUps: [] })).toEqual(answer);
    expect(normalizeRenderCardsInput({ title: "Orders", followUps: "By region\nBy channel" }))
      .toEqual({ title: "Orders", followUps: ["By region", "By channel"] });
  });

  it("rejects the previous message envelope", () => {
    expect(validateRenderCardsInput({ messages: [{ version: "v1.0", createSurface: "{}" }] }).success).toBe(false);
  });

  it("advertises a schema every provider can read in full", () => {
    expect(portableSchemaIssues(RENDER_CARDS_INPUT_SCHEMA)).toEqual([]);
  });

  it("keeps the tool description short and free of protocol vocabulary", () => {
    expect(RENDER_CARDS_DESCRIPTION.length).toBeLessThan(300);
    expect(RENDER_CARDS_DESCRIPTION).not.toMatch(/A2UI|createSurface|surfaceId|catalog/i);
  });
});
