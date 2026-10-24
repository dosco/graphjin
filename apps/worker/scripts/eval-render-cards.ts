/**
 * Card-selection eval across model providers.
 *
 * Sends ten fixed questions, each with its query result, to every provider
 * that has a key, and records whether the model calls render_cards, whether
 * the call is valid, and whether the chosen blocks fit the data.
 *
 *   GEMINI_API_KEY=… GEMINI_MODELS=gemini-3.8-flash,gemini-pro-latest \
 *   ANTHROPIC_API_KEY=… ANTHROPIC_MODELS=claude-sonnet-5-5 \
 *   OPENAI_API_KEY=… OPENAI_MODELS=gpt-5.2 \
 *   pnpm --filter @neko/worker eval:cards [--org <orgId>] [--out results.json]
 *
 * --bridge offers the tool the way the retired Hermes tool search did: a tool_call bridge
 * whose arguments parameter is an open object, with the schema as text.
 *
 * --rich offers the web schema with comparisons, what-if controls, maps,
 * diagrams, tools and drill-down, and adds the cases that need them.
 *
 * --org adds the organization's saved primary provider and model; --models
 * runs other comma-separated models with that saved key.
 */
import { writeFile } from "node:fs/promises";
import { resolvePrimaryProviderConfig } from "@neko/llm";
import {
  buildCardsSection,
  normalizeRenderCardsInput,
  RENDER_CARDS_DESCRIPTION,
  renderCardsInputSchema,
  validateRenderCardsInput,
} from "@neko/llm/work";

type Kind = "keyFigures" | "chart" | "table" | "callout" | "followUps" | "compare" | "whatIf" | "map" | "diagram" | "tool" | "drillDown";
type Case = {
  id: string;
  question: string;
  result: unknown;
  /** Each inner list is one acceptable condition; every condition must hold. */
  expect: Array<{ kinds?: Kind[]; chartTypes?: string[]; callout?: string[] }>;
  /** A single fact may be answered in a sentence. */
  cardsOptional?: boolean;
};

const CASES: Case[] = [
  {
    id: "quarterly-revenue-spike",
    question: "How has revenue changed over the last 4 quarters?",
    result: [
      { quarter: "2025 Q4", revenue: 12699845.92, orders: 5571 },
      { quarter: "2026 Q1", revenue: 11565433.28, orders: 6161 },
      { quarter: "2026 Q2", revenue: 13848055.85, orders: 6557 },
      { quarter: "2026 Q3", revenue: 88415164.46, orders: 46717 },
    ],
    expect: [{ kinds: ["chart"], chartTypes: ["line", "bar", "area"] }, { callout: ["watch", "act"] }],
  },
  {
    id: "monthly-orders",
    question: "Show monthly order counts for the last 12 months.",
    result: ["Oct", "Nov", "Dec", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep"]
      .map((month, index) => ({ month: `${month} ${index < 3 ? 2025 : 2026}`, orders: 1800 + ((index * 137) % 400) })),
    expect: [{ kinds: ["chart"], chartTypes: ["line", "area", "bar"] }],
  },
  {
    id: "category-breakdown",
    question: "Break last quarter's revenue down by product category.",
    result: [
      { category: "Bikes", revenue: 11786566.31 },
      { category: "Components", revenue: 1436159.96 },
      { category: "Clothing", revenue: 326443.63 },
      { category: "Accessories", revenue: 298885.95 },
    ],
    expect: [{ kinds: ["chart"], chartTypes: ["bar", "donut"] }],
  },
  {
    id: "category-share",
    question: "What share of revenue does each sales channel contribute?",
    result: [
      { channel: "Wholesale", revenue: 9100000 },
      { channel: "Online", revenue: 3400000 },
      { channel: "Retail stores", revenue: 1300000 },
    ],
    expect: [{ kinds: ["chart"], chartTypes: ["donut", "bar"] }],
  },
  {
    id: "top-customers",
    question: "Who are our top 10 customers by revenue this year?",
    result: Array.from({ length: 10 }, (_, index) => ({
      customer: `Customer ${String.fromCharCode(65 + index)} Ltd`,
      revenue: 2400000 - index * 180000,
      orders: 140 - index * 9,
      region: ["North", "South", "East", "West"][index % 4],
    })),
    expect: [{ kinds: ["table"] }],
  },
  {
    id: "region-comparison",
    question: "Compare North and South region revenue over the last six months.",
    result: ["Apr", "May", "Jun", "Jul", "Aug", "Sep"].map((month, index) => ({
      month: `${month} 2026`,
      north: 1200000 + index * 40000,
      south: 980000 + index * 65000,
    })),
    expect: [{ kinds: ["chart"], chartTypes: ["line", "bar", "area"] }],
  },
  {
    id: "low-stock",
    question: "Which products are below their reorder point?",
    result: Array.from({ length: 8 }, (_, index) => ({
      sku: `FR-${4100 + index}`,
      product: `Frame size ${44 + index * 2}`,
      on_hand: 3 + index,
      reorder_point: 20,
      days_of_cover: 1 + index,
    })),
    expect: [{ kinds: ["table"] }],
  },
  {
    id: "week-over-week",
    question: "How did conversion rate change week over week?",
    result: { this_week: 0.0312, last_week: 0.0347 },
    expect: [{ kinds: ["keyFigures"] }],
  },
  {
    id: "single-fact",
    question: "How many orders did we receive yesterday?",
    result: { date: "2026-10-02", orders: 214 },
    expect: [],
    cardsOptional: true,
  },
  {
    id: "late-suppliers",
    question: "Are any suppliers late on open purchase orders?",
    result: [
      { supplier: "Allied Frames", open_pos: 4, late_pos: 3, max_days_late: 12 },
      { supplier: "Trek Components", open_pos: 6, late_pos: 1, max_days_late: 2 },
      { supplier: "Pacific Wheels", open_pos: 2, late_pos: 0, max_days_late: 0 },
    ],
    expect: [{ kinds: ["table"] }, { callout: ["watch", "act"] }],
  },
];

const RICH_CASES: Case[] = [
  {
    id: "compare-categories",
    question: "Compare our Mountain, Road and Touring bike categories side by side for the last 90 days: revenue, units sold and gross margin.",
    result: [
      { category: "Mountain Bikes", units: 1740, revenue: 2323674.45, gross_margin_pct: 31.32 },
      { category: "Road Bikes", units: 1822, revenue: 1848406.7, gross_margin_pct: 18.79 },
      { category: "Touring Bikes", units: 1845, revenue: 2089567.66, gross_margin_pct: 14.13 },
    ],
    expect: [{ kinds: ["compare"] }],
  },
  {
    id: "what-if-price",
    question: "If we had raised Road bike prices by 5% last quarter, what would revenue have been? Give me a control so I can try other percentages myself.",
    result: { quarter: "2026 Q3", road_bike_revenue: 1496791.67, other_revenue: 4532859.65, road_bike_units: 1519 },
    expect: [{ kinds: ["whatIf"] }],
  },
  {
    id: "territory-map",
    question: "Show me sales by sales territory on a map for the last 90 days.",
    result: [
      { territory: "Southwest", country: "US", sales: 1121473.37 },
      { territory: "Northwest", country: "US", sales: 904211.1 },
      { territory: "Canada", country: "CA", sales: 812004.2 },
      { territory: "Australia", country: "AU", sales: 701332.9 },
      { territory: "France", country: "FR", sales: 519114.5 },
      { territory: "United Kingdom", country: "GB", sales: 512019.0 },
      { territory: "Germany", country: "DE", sales: 498701.3 },
    ],
    expect: [{ kinds: ["map"] }],
  },
  {
    id: "order-flow-diagram",
    question: "Draw a diagram of how an order flows through our system, from customer order to shipment, with the tables involved.",
    result: {
      tables: ["sales.customer", "sales.salesorderheader", "sales.salesorderdetail", "production.transactionhistory", "purchasing.shipmethod"],
      relations: [
        "salesorderheader.customerid -> customer.customerid",
        "salesorderdetail.salesorderid -> salesorderheader.salesorderid",
        "transactionhistory.referenceorderid -> salesorderheader.salesorderid",
        "salesorderheader.shipmethodid -> shipmethod.shipmethodid",
      ],
    },
    expect: [{ kinds: ["diagram"] }],
  },
  {
    id: "reorder-tool",
    question: "Build me a small calculator: I pick a product and a target number of days of cover, and it tells me how many units to reorder.",
    result: [
      { product: "HL Road Frame - Black, 58", daily_units: 12.4, on_hand: 96 },
      { product: "Touring Tire", daily_units: 3.1, on_hand: 210 },
      { product: "Mountain-200 Black, 38", daily_units: 5.6, on_hand: 41 },
    ],
    expect: [{ kinds: ["tool", "whatIf"] }],
  },
];

const TOOL_NAME = "render_cards";
const BRIDGE = process.argv.includes("--bridge");
const RICH = process.argv.includes("--rich");
const SCHEMA = renderCardsInputSchema(RICH);
const TOOL = BRIDGE
  ? {
      name: "tool_call",
      description: "Invoke a deferred tool by name with the given arguments. Argument shape matches the tool's schema (see `tool_describe`).",
      parameters: {
        type: "object",
        properties: {
          name: { type: "string", description: "Exact tool name to invoke." },
          arguments: { type: "object", description: "Arguments for the tool, matching its schema." },
        },
        required: ["name", "arguments"],
      } as Record<string, unknown>,
    }
  : { name: TOOL_NAME, description: RENDER_CARDS_DESCRIPTION, parameters: SCHEMA };
const SYSTEM = [
  "You are OpenNeko, an operations analyst. The data query for this turn has already run;",
  "its result is in the operator's message. Answer the operator's question.",
  "",
  buildCardsSection(false, RICH),
  ...(BRIDGE
    ? ["", `tool_describe result: ${JSON.stringify({ name: TOOL_NAME, description: RENDER_CARDS_DESCRIPTION, parameters: SCHEMA })}`]
    : []),
].join("\n");

/** Return the render_cards arguments from a direct or bridged call. */
function unwrap(name: string | undefined, args: unknown): unknown | undefined {
  if (!BRIDGE) return name === TOOL_NAME ? args : undefined;
  const bridged = args as { name?: string; arguments?: unknown } | undefined;
  if (name !== "tool_call" || !bridged?.name?.includes(TOOL_NAME)) return undefined;
  if (typeof bridged.arguments !== "string") return bridged.arguments;
  try {
    return JSON.parse(bridged.arguments);
  } catch {
    return bridged.arguments;
  }
}

function userMessage(testCase: Case): string {
  return `${testCase.question}\n\nSuccessful data tool result for this turn:\n${JSON.stringify(testCase.result, null, 2)}`;
}

// The retired Hermes Gemini adapter kept only these schema keys
// (agent/gemini_schema.py). Applying the same filter reproduces that request shape.
const GEMINI_SCHEMA_KEYS = new Set([
  "type", "format", "title", "description", "nullable", "enum", "maxItems", "minItems",
  "properties", "required", "minProperties", "maxProperties", "minLength", "maxLength",
  "pattern", "example", "anyOf", "propertyOrdering", "default", "items", "minimum", "maximum",
]);

function geminiSchema(schema: unknown): Record<string, unknown> {
  if (!schema || typeof schema !== "object") return {};
  const cleaned: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(schema as Record<string, unknown>)) {
    if (!GEMINI_SCHEMA_KEYS.has(key)) continue;
    if (key === "properties") {
      cleaned[key] = Object.fromEntries(
        Object.entries(value as Record<string, unknown>).map(([name, child]) => [name, geminiSchema(child)]),
      );
    } else if (key === "items") {
      cleaned[key] = geminiSchema(value);
    } else if (key === "anyOf") {
      cleaned[key] = (value as unknown[]).map(geminiSchema);
    } else {
      cleaned[key] = value;
    }
  }
  return cleaned;
}

type Call = { called: boolean; args?: unknown; text?: string; error?: string };

async function post(url: string, headers: Record<string, string>, body: unknown): Promise<any> {
  const response = await fetch(url, {
    method: "POST",
    headers: { "content-type": "application/json", ...headers },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(120_000),
  });
  const json = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(`HTTP ${response.status}: ${JSON.stringify(json).slice(0, 300)}`);
  return json;
}

const PROVIDERS: Record<string, (model: string, key: string, testCase: Case) => Promise<Call>> = {
  async gemini(model, key, testCase) {
    const json = await post(
      `https://generativelanguage.googleapis.com/v1beta/models/${model}:generateContent`,
      { "x-goog-api-key": key },
      {
        systemInstruction: { parts: [{ text: SYSTEM }] },
        contents: [{ role: "user", parts: [{ text: userMessage(testCase) }] }],
        tools: [{ functionDeclarations: [{ ...TOOL, parameters: geminiSchema(TOOL.parameters) }] }],
      },
    );
    const parts: any[] = json.candidates?.[0]?.content?.parts ?? [];
    const args = parts.map((part) => unwrap(part.functionCall?.name, part.functionCall?.args)).find((value) => value !== undefined);
    const text = parts.map((part) => part.text ?? "").join("");
    return {
      called: args !== undefined,
      args,
      text: text || `finishReason=${json.candidates?.[0]?.finishReason}`,
    };
  },
  async anthropic(model, key, testCase) {
    const json = await post(
      "https://api.anthropic.com/v1/messages",
      { "x-api-key": key, "anthropic-version": "2023-06-01" },
      {
        model,
        max_tokens: 4096,
        system: SYSTEM,
        messages: [{ role: "user", content: userMessage(testCase) }],
        tools: [{ name: TOOL.name, description: TOOL.description, input_schema: TOOL.parameters }],
      },
    );
    const blocks: any[] = json.content ?? [];
    const args = blocks.map((block) => block.type === "tool_use" ? unwrap(block.name, block.input) : undefined)
      .find((value) => value !== undefined);
    return { called: args !== undefined, args, text: blocks.map((block) => block.text ?? "").join("") };
  },
  async openai(model, key, testCase) {
    const base = process.env.OPENAI_BASE_URL ?? "https://api.openai.com/v1";
    const json = await post(
      `${base}/chat/completions`,
      { authorization: `Bearer ${key}` },
      {
        model,
        messages: [{ role: "system", content: SYSTEM }, { role: "user", content: userMessage(testCase) }],
        tools: [{ type: "function", function: TOOL }],
      },
    );
    const message = json.choices?.[0]?.message ?? {};
    const args = (message.tool_calls ?? []).map((toolCall: any) => {
      let parsed: unknown = toolCall.function?.arguments;
      try {
        parsed = JSON.parse(toolCall.function.arguments);
      } catch {
        // Keep the raw text; validation reports it.
      }
      return unwrap(toolCall.function?.name, parsed);
    }).find((value: unknown) => value !== undefined);
    return { called: args !== undefined, args, text: message.content ?? "" };
  },
};

function score(testCase: Case, call: Call) {
  if (call.error) return { outcome: "error", detail: call.error };
  if (!call.called) {
    return { outcome: testCase.cardsOptional ? "pass" : "no_cards", detail: (call.text ?? "").slice(0, 120) };
  }
  const validation = validateRenderCardsInput(call.args, "eval", { rich: RICH });
  if (!validation.success) {
    return { outcome: "invalid", detail: validation.issues.map((issue) => issue.message).join("; ").slice(0, 300) };
  }
  const answer = normalizeRenderCardsInput(call.args, { rich: RICH }) as Record<string, any>;
  const present: Record<Kind, unknown> = {
    keyFigures: answer.keyFigures, chart: answer.chart, table: answer.table, callout: answer.callout,
    followUps: answer.followUps, compare: answer.layout === "compare" || undefined, whatIf: answer.computed,
    map: answer.map, diagram: answer.diagram, tool: answer.tool, drillDown: answer.drillDown,
  };
  const kinds = (Object.keys(present) as Kind[]).filter((kind) => present[kind] !== undefined);
  const missed = testCase.expect.filter((condition) => {
    if (condition.callout) return !answer.callout || !condition.callout.includes(answer.mood ?? "watch");
    if (!condition.kinds!.some((kind) => kinds.includes(kind))) return true;
    if (condition.chartTypes && condition.kinds!.includes("chart") && kinds.includes("chart")) {
      return !condition.chartTypes.includes(answer.chart);
    }
    return false;
  });
  return {
    outcome: missed.length === 0 ? "pass" : "wrong_blocks",
    detail: `${kinds.join(",")}${missed.length ? ` (expected ${JSON.stringify(missed)})` : ""}`,
  };
}

function targets(): Array<{ provider: string; model: string; key: string }> {
  const list: Array<{ provider: string; model: string; key: string }> = [];
  const add = (provider: string, keyVar: string, modelsVar: string) => {
    const key = process.env[keyVar];
    if (!key) return;
    for (const model of (process.env[modelsVar] ?? "").split(",").map((value) => value.trim()).filter(Boolean)) {
      list.push({ provider, model, key });
    }
  };
  add("gemini", "GEMINI_API_KEY", "GEMINI_MODELS");
  add("anthropic", "ANTHROPIC_API_KEY", "ANTHROPIC_MODELS");
  add("openai", "OPENAI_API_KEY", "OPENAI_MODELS");
  return list;
}

const SAVED_PROVIDER: Record<string, string> = { "google-gemini": "gemini", anthropic: "anthropic", openai: "openai" };

async function main() {
  const args = process.argv.slice(2);
  const option = (name: string) => {
    const index = args.indexOf(name);
    return index >= 0 ? args[index + 1] : undefined;
  };
  const runs = targets();
  const orgId = option("--org");
  if (orgId) {
    const saved = await resolvePrimaryProviderConfig(orgId);
    const provider = SAVED_PROVIDER[saved.provider];
    if (!provider || !saved.secrets.apiKey) throw new Error(`saved provider ${saved.provider} is not supported by this eval`);
    const models = [saved.model, ...(option("--models") ?? "").split(",").map((value) => value.trim()).filter(Boolean)];
    for (const model of new Set(models)) runs.push({ provider, model, key: saved.secrets.apiKey });
  }
  if (runs.length === 0) throw new Error("set a provider key and model list, or pass --org");

  const results: Array<Record<string, unknown>> = [];
  for (const run of runs) {
    const only = option("--only")?.split(",").map((value) => value.trim());
    const repeat = Math.max(1, Number(option("--repeat") ?? 1));
    const cases = (RICH ? [...CASES, ...RICH_CASES] : CASES).filter((testCase) => !only || only.includes(testCase.id));
    for (const testCase of cases.flatMap((testCase) => Array.from({ length: repeat }, () => testCase))) {
      let call: Call;
      try {
        call = await PROVIDERS[run.provider]!(run.model, run.key, testCase);
      } catch (error) {
        call = { called: false, error: error instanceof Error ? error.message : String(error) };
      }
      const scored = score(testCase, call);
      results.push({ provider: run.provider, model: run.model, case: testCase.id, ...scored, args: call.args });
      console.log(`${run.provider}/${run.model}  ${testCase.id.padEnd(24)} ${scored.outcome.padEnd(12)} ${scored.detail}`);
    }
  }

  console.log("\nSummary");
  for (const run of runs) {
    const mine = results.filter((result) => result.provider === run.provider && result.model === run.model);
    const count = (outcome: string) => mine.filter((result) => result.outcome === outcome).length;
    console.log(`${run.provider}/${run.model}: pass ${count("pass")}/${mine.length}, invalid ${count("invalid")}, wrong blocks ${count("wrong_blocks")}, no cards ${count("no_cards")}, errors ${count("error")}`);
  }
  const out = option("--out");
  if (out) await writeFile(out, JSON.stringify(results, null, 2));
}

main().then(
  () => process.exit(0),
  (error) => {
    console.error(error instanceof Error ? error.message : error);
    process.exit(1);
  },
);
