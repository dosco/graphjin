import { z } from "zod";
import type { AgentSurfaceMessage } from "../agent-backend";
import { randomUUID } from "node:crypto";
import {
  evaluateExpression,
  expressionNames,
  NAME_PATTERN,
  parseExpression,
} from "./answer-expression";
import { ANSWER_TOOL_MAX_HTML, answerToolReachesOut } from "./answer-tool-check";

export const A2UI_VERSION = "v1.0" as const;
export const A2UI_CATALOG_ID = "urn:openneko:catalog:work:v2" as const;
export const A2UI_RENDER_SERVER_NAME = "neko_ui" as const;
export const A2UI_RENDER_TOOL_NAME = "render_cards" as const;
/** Registered name for render_cards on the brokered neko MCP server. */
export const A2UI_RENDER_MCP_TOOL_NAME =
  "mcp__neko__ui_render_cards" as const;

/**
 * ACP reports tools from the multiplexed `neko` bridge as
 * `mcp_neko_<logical-server>_<tool>` in ACP notifications.
 */
export const A2UI_RENDER_ACP_TITLE =
  "mcp_neko_ui_render_cards" as const;

const a2uiComponentSchema = z
  .object({ id: z.string().min(1), component: z.string().min(1) })
  .passthrough();

function messageSchema(opts: { generated: boolean }) {
  const version = opts.generated
    ? z.literal(A2UI_VERSION)
    : z.enum(["v0.9", A2UI_VERSION]);
  const catalogId = opts.generated
    ? z.literal(A2UI_CATALOG_ID)
    : z.string().min(1);

  const createSurfaceSchema = z
    .object({
      version,
      createSurface: z
        .object({
          surfaceId: z.string().min(1),
          catalogId,
          surfaceProperties: z.record(z.string(), z.unknown()).optional(),
          sendDataModel: z.boolean().optional(),
          components: z.array(a2uiComponentSchema).min(1).optional(),
          dataModel: z.record(z.string(), z.unknown()).optional(),
        })
        .strict(),
    })
    .strict();
  const updateComponentsSchema = z
    .object({
      version,
      updateComponents: z
        .object({
          surfaceId: z.string().min(1),
          components: z.array(a2uiComponentSchema).min(1),
        })
        .strict(),
    })
    .strict();
  const updateDataModelSchema = z
    .object({
      version,
      updateDataModel: z
        .object({
          surfaceId: z.string().min(1),
          path: z.string().optional(),
          value: z.unknown().optional(),
        })
        .strict(),
    })
    .strict();
  const deleteSurfaceSchema = z
    .object({
      version,
      deleteSurface: z.object({ surfaceId: z.string().min(1) }).strict(),
    })
    .strict();

  return z.union([
    createSurfaceSchema,
    updateComponentsSchema,
    updateDataModelSchema,
    deleteSurfaceSchema,
  ]);
}

/** The sole schema for newly generated A2UI messages. */
export const generatedA2UIMessageSchema = messageSchema({ generated: true });

/** Reader compatibility for already-persisted v0.9 surfaces. */
const readableA2UIMessageSchema = messageSchema({ generated: false });

const text = (max: number) => z.string().trim().min(1).max(max);

/**
 * Every argument is a string, a list of strings, or an enum. Some agent
 * runtimes call deferred tools through a bridge whose arguments parameter is
 * an open object; small models fill flat text fields reliably there and nested
 * objects poorly. The host reads the text with plain string splitting.
 */
export const renderCardsArgsSchema = z.object({
  title: text(120),
  stage: z.enum(["draft", "final"]).optional()
    .describe('"draft" for an early card while you are still checking figures; "final" for the finished answer.'),
  layout: z.enum(["report", "compare"]).optional()
    .describe('"compare" shows each table row as a side-by-side panel; use it for 2 to 6 items compared on the same measures.'),
  table: text(20_000).optional()
    .describe("A pipe table: a header row, then one row per line, cells separated by |."),
  chart: z.enum(["line", "bar", "area", "donut"]).optional()
    .describe("Chart the table: the first column gives the labels, chartColumn the values."),
  chartColumn: text(60).optional()
    .describe("The table column to chart. Write its cells as plain numbers, such as 12699845.92."),
  keyFigures: z.array(text(160)).min(1).max(6).optional()
    .describe('Headline numbers, each written as "Label: value" with an optional "(note)".'),
  rowDetails: z.array(text(8000)).min(1).max(20).optional()
    .describe('Sub-tables that table rows open in place, each "Row name >> pipe table": the row\'s first-column name, then the sub-table\'s header row and rows, one per line.'),
  drillDown: text(200).optional()
    .describe("A follow-up question for one table row, chart point, map place, or diagram step. Write {label} where its name goes."),
  controls: z.array(text(200)).min(1).max(4).optional()
    .describe('Inputs the reader can move, each "name: Label (unit) = default (min to max, step n)", such as "price_change: Price change (%) = 5 (-20 to 20, step 1)", or a choice "name: Label = Option (number) | Option (number)", where the first option is the default and formulas use the chosen number.'),
  values: z.array(text(120)).max(24).optional()
    .describe('Fixed figures the formulas use, each "name = number", such as "road_revenue = 1496791.67".'),
  computed: z.array(text(300)).min(1).max(6).optional()
    .describe('Results that recompute as the controls move, each "Label (unit): formula" using control and value names, + - * / ^, min, max, round, abs.'),
  map: z.array(text(160)).min(1).max(80).optional()
    .describe('Places to plot, each "Place @ latitude, longitude: value".'),
  mapValue: text(60).optional().describe("What the map values measure, with the unit, such as Sales (USD)."),
  diagram: z.array(text(200)).min(1).max(40).optional()
    .describe('Steps of a process or the links between parts, each "From -> To" with an optional ": label".'),
  tool: text(ANSWER_TOOL_MAX_HTML).optional()
    .describe("A small interactive tool as an HTML fragment with inline <script>. Put every figure it needs in the fragment. It runs in a frame as narrow as 320px that already styles labels, inputs, selects, buttons, output and tables, so add little CSS."),
  callout: text(600).optional().describe("A status, risk, or recommended action."),
  mood: z.enum(["good", "watch", "act"]).optional().describe("The callout's tone."),
  followUps: z.array(text(200)).min(1).max(4).optional()
    .describe("Follow-up questions the operator can ask next."),
});

/** Fields a channel without the rich answer renderer leaves out of the tool schema. */
export const RICH_ANSWER_FIELDS = ["layout", "rowDetails", "drillDown", "controls", "values", "computed", "map", "mapValue", "diagram", "tool"] as const;

/** The card tool schema for channels that render only the core blocks. */
export const coreRenderCardsArgsSchema = renderCardsArgsSchema.omit(
  Object.fromEntries(RICH_ANSWER_FIELDS.map((field) => [field, true])) as { [K in (typeof RICH_ANSWER_FIELDS)[number]]: true },
);

/** JSON Schema is generated from the validator instead of maintained by hand. */
export const RENDER_CARDS_INPUT_SCHEMA = z.toJSONSchema(
  renderCardsArgsSchema,
) as Record<string, unknown>;

export const CORE_RENDER_CARDS_INPUT_SCHEMA = z.toJSONSchema(
  coreRenderCardsArgsSchema,
) as Record<string, unknown>;

export function renderCardsInputSchema(rich: boolean): Record<string, unknown> {
  return rich ? RENDER_CARDS_INPUT_SCHEMA : CORE_RENDER_CARDS_INPUT_SCHEMA;
}

export type RenderCardsArgs = z.infer<typeof renderCardsArgsSchema>;

export type RenderInputValidation =
  | { success: true; messages: AgentSurfaceMessage[] }
  | {
      success: false;
      issues: Array<{ path: string; code: string; message: string }>;
    };

type Issue = { path: string; code: string; message: string };
type Table = { columns: string[]; rows: string[][] };
type Built<T> = { value: T } | { issue: Issue };

const SECTIONS = ["table", "keyFigures", "callout", "followUps", "computed", "map", "diagram", "tool"] as const;

/** A plain number with optional thousands separators, such as 1,250 or -3.5. */
function plainNumber(cell: string): number | undefined {
  const compact = cell.trim().replaceAll(",", "");
  if (compact === "") return undefined;
  const value = Number(compact);
  return Number.isFinite(value) ? value : undefined;
}

function cells(line: string): string[] {
  let row = line.trim();
  if (row.startsWith("|")) row = row.slice(1);
  if (row.endsWith("|")) row = row.slice(0, -1);
  return row.split("|").map((cell) => cell.trim());
}

function isDivider(line: string): boolean {
  return [...line].every((char) => "|-: ".includes(char));
}

/** Split text into lines. Some models write the two characters \ and n for a line break. */
function lines(source: string): string[] {
  return source.replaceAll("\\n", "\n").split("\n");
}

function readTable(source: string): Built<Table> {
  const rowsText = lines(source).filter((line) => line.trim() !== "" && !isDivider(line));
  if (rowsText.length < 2) {
    return { issue: { path: "table", code: "table_shape", message: "table needs a header row and at least one data row." } };
  }
  const columns = cells(rowsText[0]!);
  const rows = rowsText.slice(1).map(cells);
  const short = rows.findIndex((row) => row.length !== columns.length);
  if (short >= 0) {
    return { issue: { path: "table", code: "table_row_width", message: `table row ${short + 1} needs ${columns.length} cells, one per column.` } };
  }
  return { value: { columns, rows } };
}

function numericColumn(table: Table, index: number): boolean {
  return index > 0 && table.rows.every((row) => plainNumber(row[index]!) !== undefined);
}

function chartFromTable(args: RenderCardsArgs, table: Table): Built<Record<string, unknown>> {
  const requested = args.chartColumn
    ? table.columns.findIndex((name) => name.toLowerCase() === args.chartColumn!.trim().toLowerCase())
    : -1;
  if (args.chartColumn && requested < 1) {
    return { issue: { path: "chartColumn", code: "chart_column", message: `chartColumn must be one of the table's value columns: ${table.columns.slice(1).join(", ")}.` } };
  }
  const column = requested >= 1 ? requested : table.columns.findIndex((_, index) => numericColumn(table, index));
  if (column < 1) {
    return { issue: { path: "chartColumn", code: "chart_column", message: "chart needs a table column of plain numbers, such as 12699845.92; name it in chartColumn." } };
  }
  const bad = table.rows.findIndex((row) => plainNumber(row[column]!) === undefined);
  if (bad >= 0) {
    return { issue: { path: "table", code: "chart_value", message: `table row ${bad + 1} needs a plain number in the ${table.columns[column]} column, such as 12699845.92.` } };
  }
  const data = table.rows.map((row) => ({ d: row[0]!, v: plainNumber(row[column]!)! }));
  if (data.length < 2 || data.length > 60) {
    return { issue: { path: "chart", code: "chart_points", message: "chart needs 2 to 60 table rows." } };
  }
  if (args.chart === "donut" && (data.length > 8 || data.some((point) => point.v < 0))) {
    return { issue: { path: "chart", code: "donut_parts", message: "a donut needs 2 to 8 rows with nonnegative values." } };
  }
  return {
    value: { id: "chart", component: "Chart", type: args.chart, title: args.title, valueLabel: table.columns[column], data },
  };
}

/** One format per column: whole numbers above 10,000, otherwise up to two decimals. */
function columnFormat(table: Table, index: number): Intl.NumberFormat {
  const values = table.rows.map((row) => plainNumber(row[index]!)!);
  const decimals = values.some((value) => !Number.isInteger(value)) && Math.max(...values.map(Math.abs)) < 10_000 ? 2 : 0;
  return new Intl.NumberFormat("en-US", { minimumFractionDigits: decimals, maximumFractionDigits: decimals });
}

function tableRows(table: Table) {
  const numeric = table.columns.map((_, index) => numericColumn(table, index));
  const formats = table.columns.map((_, index) => numeric[index] ? columnFormat(table, index) : null);
  return {
    columns: table.columns.map((label, index) => ({ key: `c${index}`, label, ...(numeric[index] ? { align: "right" } : {}) })),
    rows: table.rows.map((row) => Object.fromEntries(row.map((cell, index) => [
      `c${index}`,
      formats[index] ? formats[index]!.format(plainNumber(cell)!) : cell,
    ]))),
  };
}

function rowDetails(args: RenderCardsArgs, table: Table, issues: Issue[]): Record<string, ReturnType<typeof tableRows>> | null {
  if (!args.rowDetails) return null;
  const names = new Set(table.rows.map((row) => row[0]!.trim()));
  const details: Record<string, ReturnType<typeof tableRows>> = {};
  args.rowDetails.forEach((entry, index) => {
    const path = `rowDetails.${index}`;
    const split = entry.indexOf(">>");
    const name = split > 0 ? entry.slice(0, split).trim() : "";
    if (!name || !names.has(name)) {
      issues.push({ path, code: "row_details", message: `${path} needs "Row name >> pipe table", where the row name is a first-column cell of table.` });
      return;
    }
    const sub = readTable(entry.slice(split + 2));
    if ("issue" in sub) issues.push({ ...sub.issue, path, message: `${path}: ${sub.issue.message.replace(/^table /, "the sub-table ")}` });
    else details[name] = tableRows(sub.value);
  });
  return Object.keys(details).length ? details : null;
}

function withDrill(component: Record<string, unknown>, drill: string | undefined): Record<string, unknown> {
  return drill ? { ...component, drill } : component;
}

function keyFigure(line: string, index: number): Built<Record<string, string>> {
  const colon = line.indexOf(":");
  let value = colon > 0 ? line.slice(colon + 1).trim() : "";
  let sub: string | undefined;
  const open = value.lastIndexOf("(");
  if (open > 0 && value.endsWith(")")) {
    sub = value.slice(open + 1, -1).trim();
    value = value.slice(0, open).trim();
  }
  if (!value) {
    return { issue: { path: `keyFigures.${index}`, code: "key_figure", message: `keyFigures.${index} needs the form "Label: value".` } };
  }
  return { value: { label: line.slice(0, colon).trim(), value, ...(sub ? { sub } : {}) } };
}

/** "Label (unit)" → label and unit. */
function labelUnit(source: string): { label: string; unit?: string } {
  const match = /^(.*?)\s*\(([^()]{1,16})\)\s*$/.exec(source.trim());
  return match ? { label: match[1]!.trim(), unit: match[2]!.trim() } : { label: source.trim() };
}

type ControlOption = { label: string; value: number };
type Control = { name: string; label: string; value: number; min: number; max: number; step: number; unit?: string; options?: ControlOption[] };

function choiceControl(line: string, issue: (message: string) => Built<Control>): Built<Control> | null {
  const match = /^\s*([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.+?)\s*=\s*(.+\|.+)$/.exec(line);
  if (!match) return null;
  const options: ControlOption[] = [];
  for (const part of match[3]!.split("|")) {
    const option = /^\s*(.+?)\s*\(\s*(-?[\d.,]+)\s*\)\s*$/.exec(part);
    const value = option ? plainNumber(option[2]!) : undefined;
    if (!option || value === undefined) return issue('needs each choice in the form "Option (number)", separated by |.');
    options.push({ label: option[1]!, value });
  }
  if (options.length > 12) return issue("has more than 12 choices.");
  const values = options.map((option) => option.value);
  return { value: { name: match[1]!.toLowerCase(), ...labelUnit(match[2]!), value: values[0]!, min: Math.min(...values), max: Math.max(...values), step: 1, options } };
}

function control(line: string, index: number): Built<Control> {
  const issue = (message: string): Built<Control> => ({ issue: { path: `controls.${index}`, code: "control", message: `controls.${index} ${message}` } });
  const choice = choiceControl(line, issue);
  if (choice) return choice;
  const match = /^\s*([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.+?)\s*=\s*(-?[\d.,]+)\s*\(\s*(-?[\d.,]+)\s*to\s*(-?[\d.,]+)\s*(?:,\s*step\s*([\d.,]+))?\s*\)\s*$/i.exec(line);
  if (!match) return issue('needs the form "name: Label (unit) = default (min to max, step n)".');
  const [value, min, max] = [match[3]!, match[4]!, match[5]!].map((cell) => plainNumber(cell)!);
  if (!(min! < max!)) return issue("needs min below max.");
  if (value! < min! || value! > max!) return issue("needs its default between min and max.");
  const step = match[6] ? plainNumber(match[6]) : undefined;
  if (step !== undefined && !(step > 0)) return issue("needs a step above 0.");
  return { value: { name: match[1]!.toLowerCase(), ...labelUnit(match[2]!), value: value!, min: min!, max: max!, step: step ?? Number(((max! - min!) / 100).toPrecision(2)) } };
}

function namedValue(line: string, index: number): Built<[string, number]> {
  const match = /^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(-?[\d.,]+)\s*$/.exec(line);
  const value = match ? plainNumber(match[2]!) : undefined;
  if (!match || value === undefined) {
    return { issue: { path: `values.${index}`, code: "value", message: `values.${index} needs the form "name = number".` } };
  }
  return { value: [match[1]!.toLowerCase(), value] };
}

function whatIf(args: RenderCardsArgs, issues: Issue[]): Record<string, unknown> | null {
  if (!args.controls && !args.computed) return null;
  if (!args.controls || !args.computed) {
    issues.push({ path: args.controls ? "computed" : "controls", code: "what_if", message: "controls and computed go together: the controls move, the computed results follow." });
    return null;
  }
  const controls: Control[] = [];
  const scope: Record<string, number> = {};
  const declared = new Set<string>();
  args.controls.forEach((line, index) => {
    const name = /^\s*([A-Za-z_][A-Za-z0-9_]*)\s*:/.exec(line)?.[1]?.toLowerCase();
    if (name) declared.add(name);
    const built = control(line, index);
    if ("issue" in built) issues.push(built.issue);
    else { controls.push(built.value); scope[built.value.name] = built.value.value; }
  });
  const values: Record<string, number> = {};
  (args.values ?? []).forEach((line, index) => {
    const name = /^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=/.exec(line)?.[1]?.toLowerCase();
    if (name) declared.add(name);
    const built = namedValue(line, index);
    if ("issue" in built) issues.push(built.issue);
    else { values[built.value[0]] = built.value[1]; scope[built.value[0]] = built.value[1]; }
  });
  if (declared.size !== args.controls.length + (args.values?.length ?? 0)) {
    issues.push({ path: "values", code: "value", message: "each control and value needs its own name." });
  }
  const names = declared;
  const valid = controls.length === args.controls.length;
  const outputs = args.computed.flatMap((line, index) => {
    const colon = line.indexOf(":");
    const path = `computed.${index}`;
    if (colon < 1) {
      issues.push({ path, code: "computed", message: `${path} needs the form "Label (unit): formula".` });
      return [];
    }
    const expression = line.slice(colon + 1).trim();
    try {
      const parsed = parseExpression(expression);
      const unknown = [...expressionNames(parsed)].filter((name) => !names.has(name));
      if (unknown.length > 0) {
        issues.push({ path, code: "computed", message: `${path} uses ${unknown.join(", ")}; name each one in controls or values.` });
        return [];
      }
      if (valid && !Number.isFinite(evaluateExpression(parsed, scope))) {
        issues.push({ path, code: "computed", message: `${path} gives no finite number at the default control values.` });
        return [];
      }
    } catch (error) {
      issues.push({ path, code: "computed", message: `${path}: ${(error as Error).message}.` });
      return [];
    }
    return [{ ...labelUnit(line.slice(0, colon)), expression }];
  });
  return { id: "whatIf", component: "WhatIf", controls, values, outputs };
}

function mapComponent(args: RenderCardsArgs, issues: Issue[]): Record<string, unknown> | null {
  if (!args.map) return null;
  const points = args.map.flatMap((line, index) => {
    const match = /^\s*(.+?)\s*@\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*:\s*(-?[\d.,]+)\s*$/.exec(line);
    const v = match ? plainNumber(match[4]!) : undefined;
    const [lat, lon] = match ? [Number(match[2]), Number(match[3])] : [Number.NaN, Number.NaN];
    if (!match || v === undefined || Math.abs(lat) > 90 || Math.abs(lon) > 180) {
      issues.push({ path: `map.${index}`, code: "map_point", message: `map.${index} needs the form "Place @ latitude, longitude: value" with latitude -90 to 90 and longitude -180 to 180.` });
      return [];
    }
    return [{ label: match[1]!, lat, lon, v }];
  });
  if (points.some((point) => point.v < 0)) {
    issues.push({ path: "map", code: "map_point", message: "map values need to be 0 or more." });
  }
  return withDrill({ id: "map", component: "AnswerMap", title: args.title, valueLabel: args.mapValue ?? "Value", points }, args.drillDown);
}

function diagramComponent(args: RenderCardsArgs, issues: Issue[]): Record<string, unknown> | null {
  if (!args.diagram) return null;
  const nodes = new Map<string, { id: string; label: string }>();
  const node = (label: string) => {
    const key = label.toLowerCase();
    if (!nodes.has(key)) nodes.set(key, { id: label, label });
    return nodes.get(key)!.id;
  };
  const edges = args.diagram.flatMap((line, index) => {
    const arrow = line.indexOf("->");
    if (arrow < 0) {
      const label = line.trim();
      if (label) node(label);
      return [];
    }
    const from = line.slice(0, arrow).trim();
    const rest = line.slice(arrow + 2);
    const colon = rest.indexOf(":");
    const to = (colon >= 0 ? rest.slice(0, colon) : rest).trim();
    const label = colon >= 0 ? rest.slice(colon + 1).trim() : "";
    if (!from || !to) {
      issues.push({ path: `diagram.${index}`, code: "diagram", message: `diagram.${index} needs the form "From -> To" with an optional ": label".` });
      return [];
    }
    return [{ from: node(from), to: node(to), ...(label ? { label } : {}) }];
  });
  if (nodes.size > 30) issues.push({ path: "diagram", code: "diagram", message: "a diagram holds up to 30 steps." });
  return withDrill({ id: "diagram", component: "Diagram", title: args.title, nodes: [...nodes.values()], edges }, args.drillDown);
}

function toolComponent(args: RenderCardsArgs, issues: Issue[]): Record<string, unknown> | null {
  if (!args.tool) return null;
  if (answerToolReachesOut(args.tool)) {
    issues.push({ path: "tool", code: "tool_network", message: "tool runs offline inside the answer; put every figure it needs in the fragment, and load, open, or navigate to nothing outside it." });
    return null;
  }
  return { id: "tool", component: "AnswerTool", title: args.title, html: args.tool };
}

function answerComponents(args: RenderCardsArgs): { components: Array<Record<string, unknown>>; issues: Issue[] } {
  const components: Array<Record<string, unknown>> = [];
  const issues: Issue[] = [];
  if (SECTIONS.every((section) => args[section] === undefined)) {
    issues.push({ path: "", code: "empty_answer", message: `Fill at least one of ${SECTIONS.join(", ")}.` });
  }
  if (args.drillDown && !args.drillDown.includes("{label}")) {
    issues.push({ path: "drillDown", code: "drill_down", message: "drillDown needs {label} where the item's name goes." });
  }
  if (args.keyFigures) {
    const figures = args.keyFigures.map(keyFigure);
    for (const figure of figures) if ("issue" in figure) issues.push(figure.issue);
    components.push({ id: "keyFigures", component: "KeyFigures", items: figures.flatMap((figure) => "value" in figure ? [figure.value] : []) });
  }
  for (const build of [mapComponent, diagramComponent]) {
    const component = build(args, issues);
    if (component) components.push(component);
  }
  const table = args.table ? readTable(args.table) : undefined;
  if (table && "issue" in table) issues.push(table.issue);
  if (args.chart && !table) {
    issues.push({ path: "chart", code: "chart_table", message: "chart draws from table; add a table." });
  }
  if (args.layout === "compare" && !table) {
    issues.push({ path: "layout", code: "compare_table", message: "compare shows table rows side by side; add a table." });
  }
  if (args.chart && table && "value" in table) {
    const chart = chartFromTable(args, table.value);
    if ("issue" in chart) issues.push(chart.issue);
    else components.push(withDrill(chart.value, args.drillDown));
  }
  if (table && "value" in table) {
    const compare = args.layout === "compare";
    if (compare && (table.value.rows.length < 2 || table.value.rows.length > 6)) {
      issues.push({ path: "layout", code: "compare_rows", message: "compare needs 2 to 6 table rows; use the report layout for more." });
    }
    const details = rowDetails(args, table.value, issues);
    components.push(withDrill({ id: "table", component: compare ? "Compare" : "Table", ...tableRows(table.value), ...(details ? { details } : {}) }, args.drillDown));
  } else if (args.rowDetails) {
    issues.push({ path: "rowDetails", code: "row_details", message: "rowDetails open under table rows; add a table." });
  }
  const adjust = whatIf(args, issues);
  if (adjust) components.push(adjust);
  const tool = toolComponent(args, issues);
  if (tool) components.push(tool);
  if (args.callout) components.push({ id: "callout", component: "Callout", mood: args.mood ?? "watch", text: args.callout });
  if (args.followUps) {
    components.push({ id: "followUps", component: "Choice", options: args.followUps.map((prompt) => ({ label: prompt, prompt })) });
  }
  return { components, issues };
}

function answerSurface(
  args: Pick<RenderCardsArgs, "title" | "stage">,
  sections: Array<Record<string, unknown>>,
  surfaceId: string,
): AgentSurfaceMessage[] {
  return [{
    version: A2UI_VERSION,
    createSurface: {
      surfaceId,
      catalogId: A2UI_CATALOG_ID,
      dataModel: {},
      components: [
        {
          id: "root",
          component: "Answer",
          title: args.title,
          ...(args.stage === "draft" ? { stage: "draft" } : {}),
          children: sections.map((section) => section.id),
        },
        ...sections,
      ],
    },
  } as AgentSurfaceMessage];
}

const ANSWER_FIELDS = new Set(Object.keys(renderCardsArgsSchema.shape));
const RICH_FIELD_SET = new Set<string>(RICH_ANSWER_FIELDS);
const LIST_FIELDS = new Set(["keyFigures", "followUps", "controls", "values", "computed", "map", "diagram"]);

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function parseJsonObject(value: unknown): unknown {
  if (typeof value !== "string") return value;
  try {
    const parsed: unknown = JSON.parse(value);
    return isRecord(parsed) ? parsed : value;
  } catch {
    return value;
  }
}

/**
 * Some agent runtimes call deferred tools through a bridge whose arguments
 * parameter is an open object. Models then nest the call again, send it as
 * JSON text, write a list as lines, or fill unused fields with null, false or
 * "". Recover the answer object; validation still decides what is accepted.
 */
export function normalizeRenderCardsInput(value: unknown, opts: { rich?: boolean } = {}): unknown {
  const rich = opts.rich ?? true;
  let answer = parseJsonObject(value);
  for (let depth = 0; depth < 3 && isRecord(answer) && !("title" in answer); depth += 1) {
    const nested = parseJsonObject("arguments" in answer
      ? answer.arguments
      : Object.keys(answer).length === 1 ? Object.values(answer)[0] : undefined);
    if (!isRecord(nested)) break;
    answer = nested;
  }
  if (!isRecord(answer)) return answer;
  const normalized: Record<string, unknown> = {};
  for (const [field, raw] of Object.entries(answer)) {
    if (!ANSWER_FIELDS.has(field) || (!rich && RICH_FIELD_SET.has(field)) || raw === null || raw === false || raw === "" ||
      (Array.isArray(raw) && raw.length === 0)) continue;
    normalized[field] = LIST_FIELDS.has(field) && typeof raw === "string"
      ? lines(raw).map((line) => line.trim()).filter(Boolean)
      : raw;
  }
  return normalized;
}

/** Validate the complete tool argument object and build its surface. */
export function validateRenderCardsInput(
  value: unknown,
  surfaceId = `answer-${randomUUID()}`,
  opts: { rich?: boolean } = {},
): RenderInputValidation {
  const parsed = renderCardsArgsSchema.safeParse(normalizeRenderCardsInput(value, opts));
  if (!parsed.success) {
    return {
      success: false,
      issues: parsed.error.issues.map((issue) => ({
        path: issue.path.join("."),
        code: issue.code,
        message: `${issue.path.join(".") || "input"}: ${issue.message}`,
      })),
    };
  }
  const { components, issues } = answerComponents(parsed.data);
  if (issues.length > 0) return { success: false, issues };
  return { success: true, messages: answerSurface(parsed.data, components, surfaceId) };
}

/** Validate parsed reader input while retaining v0.9 history compatibility. */
export function coerceReadableSurfaceMessages(
  value: unknown,
): AgentSurfaceMessage[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((message) => {
    const parsed = readableA2UIMessageSchema.safeParse(message);
    return parsed.success ? [parsed.data as AgentSurfaceMessage] : [];
  });
}

/** Validate parsed model output. New render calls are v1.0 only. */
export function coerceGeneratedSurfaceMessages(
  value: unknown,
): AgentSurfaceMessage[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((message) => {
    const parsed = generatedA2UIMessageSchema.safeParse(message);
    return parsed.success ? [parsed.data as AgentSurfaceMessage] : [];
  });
}
