// Prompt builder for the per-card metric agent. Composed from the shared
// prompts/sections module (data-access, memory, GraphJin rules) plus
// metric-specific sections (time-window semantics, mood/chart contract,
// JSON output contract). Keeping it composition-shaped means anti-fanout
// guidance, date-filter rules, and memory injection stay in lock-step
// with the chat and workflow agents.

import type { AgentWorkspace } from "./agent-backend";
import type { KnowledgePackContents } from "./knowledge-pack";
import {
  buildDataAccessSection,
  buildMemorySection,
} from "./prompts/sections";

export type MetricPromptInput = {
  question?: string | null;
  title: string;
  why: string | null;
  role: string;
  slug: string;
  chartHint: string | null;
};

const METRIC_TIME_WINDOW = `<time_window>
Every card's headline is computed against a specific time window. The
window goes in the \`timeWindow\` output field so the operator (and
downstream code) knows what slice of data produced the headline.

The \`grain\` field is one of: day | week | month | quarter | year |
all_time | snapshot.

Pick the window like this:
1. If the card's title or rationale names an explicit window — "this
   quarter", "last 30 days", "MTD", "YTD", "since launch", "all time",
   etc. — use that.
2. Otherwise default to a year-grain TTM (trailing twelve months ending
   at the most recent data date). TTM is what most CXO dashboards
   expect and gives stable period-over-period comparisons.
   TTM end = the latest data date; start = end minus one year plus one
   day (end 2026-10-08 → start 2025-10-09). The preceding period is the
   twelve months before start. Filter with date >= start and
   date < the day after end: date columns can hold times, so a
   date-only "<= end" drops the end day.
3. For metrics that are inherently snapshot-style — current employee
   count, open opportunities, accounts at risk, inventory on hand — use
   grain="snapshot" with start = end = today.

Field rules:
- grain  : enum value above. Match the natural duration of the window.
           "TTM" → year. "Last 30 days" → month. "Q3" → quarter. "Today" → day.
- start  : ISO yyyy-mm-dd. Required for all grains except "all_time".
- end    : ISO yyyy-mm-dd. Required for all grains except "all_time".
- label  : 1–4 words a CXO would say — "TTM", "YTD", "Q3 2025", "Last 30 days",
           "All time", "Snapshot".

Quarter convention: calendar quarters start Jan 1 / Apr 1 / Jul 1 / Oct 1.
Q1 2025 = 2025-01-01 to 2025-03-31. Q2 = 04-01 to 06-30, Q3 = 07-01 to 09-30,
Q4 = 10-01 to 12-31. "Previous quarter" means the most recently-completed
calendar quarter. "Current quarter" / QTD = the partial quarter in progress.

Consistency: the timeWindow MUST match whatever date filter the query
actually used.
</time_window>`;

const METRIC_MOOD_CHART = `<mood_and_chart>
Mood is mandatory and must be derived from the data, never guessed:
- Always fetch a baseline (prior period of equal length, or rolling
  average) in the same workflow as the current value.
- "good" only if current is materially better than baseline.
- "bad" if current is materially worse than baseline (a >15% drop on a
  metric where higher is better, or a >15% rise on a metric where lower
  is better).
- "watch" if within ±15%.
- A "good" mood with a >15% downward delta is a contradiction. Re-check
  before responding.

chartType MUST match the shape of chartData. Mismatches will not render:
- kpi: exactly 1 item with both v (current) and t (baseline). Use only
  when there is one headline number with a comparison.
- donut: 2-6 items, each item is a category share. Use for "X by
  category" mixes.
- bar: 2-20 items, each item is a category or period. Use for category
  breakdowns or short time series with comparisons.
- line / area: 4+ items, time series. Use only for trends over time.
- If you have multiple categories (e.g. 3 countries, 5 work centers),
  the chartType is donut or bar — never kpi. kpi is for a single
  number only.
</mood_and_chart>`;

const METRIC_OUTPUT_CONTRACT = `<output_contract>
This is the complete output specification; the workspace holds no other.
Respond with ONE JSON object, exactly this shape, no prose:

{
  "reasoning": "string — which tables, aggregation, timeframe you used",
  "headlineMetric": "string — the single most important number with units, e.g. \\"$7.98M\\"",
  "headlineLabel": "string — short label for the headline metric, e.g. \\"Revenue MTD\\"",
  "insightText": "string — one-sentence plain-English observation a CXO can act on",
  "detailText": "string — 1-2 sentence drill-down explaining the driver",
  "mood": "good | watch | bad",
  "chartType": "kpi | line | bar | donut | area",
  "chartData": [{ "d": "string label", "v": 0, "t": 0 }],
  "baselineValue": 0,
  "timeWindow": {
    "grain": "day | week | month | quarter | year | all_time | snapshot",
    "start": "yyyy-mm-dd or null (only when grain='all_time')",
    "end":   "yyyy-mm-dd or null (only when grain='all_time')",
    "label": "1–4 word display label, e.g. \\"TTM\\", \\"YTD\\", \\"Q3 2025\\", \\"Snapshot\\""
  }
}

Rules for the JSON:
- Output a SINGLE JSON object. No markdown fences, no prose before or after.
- mood and chartType must be one of the listed enum values, lowercase.
- chartData must be a non-empty array. For kpi, exactly 1 item with both
  v and t. For donut, 2-6 items. For bar, 2-20 items. For line/area, 4+
  items.
- All numbers in chartData.v and chartData.t must be plain numbers (not
  strings, no units).
- baselineValue is the comparison-period value of the headline metric, as a
  full plain number on the same scale as chartData: 29983688.69, never 29.98
  for $29.98M. Use null only when no comparison exists.
- timeWindow.grain must be one of the listed enum values, lowercase.
- timeWindow.start and timeWindow.end must be ISO yyyy-mm-dd strings (or
  null only when grain='all_time'). They must match the date filter the
  query actually used.
</output_contract>`;

const METRIC_QUERY_CONSTRAINTS = `- If you need an anchor date, query \`max(<date_col>)\` from the live data
  and use that — don't trust sample dates from the knowledge pack.
- Never use a bare limit without pagination. Use cursor-based pagination
  to process all rows, or use GraphQL aggregation with distinct to let
  the database aggregate.
- Never compute a sum-of-products (revenue = price × qty per row) by
  multiplying avg_<price> × sum_<quantity> — mathematically wrong. Use
  sum(expr: { mul: [...] }) instead.
- Watch the silent 20-row default limit on every query level (top AND
  nested) — set explicit limit or use distinct+aggregation.
`;

const metricHardConstraints = (direct: boolean) => `<hard_constraints>
- Never hardcode calendar years (e.g. "2025-07-20", "2014-01-01");
  compute periods with relative arithmetic.
- Never hardcode baseline values or magic numbers. Always compute
  baselines from the data (prior period of equal length, YoY, rolling
  average, etc).
${direct ? METRIC_QUERY_CONSTRAINTS : ""}- Never invent or interpolate. If a query returned no rows, the answer
  is "no data", not a guess.
- If your queries fail or return data you cannot reason from, DO NOT
  narrate the failure as a metric. The worker treats this as a
  successful run and the dashboard renders your error string as if it
  were data. Instead: exit with the raw error text on stdout (any
  non-JSON output triggers a job failure → automatic retry).
  Specifically: do NOT emit \`headlineMetric\` values like "Error" /
  "errors" / "N/A" / "Unavailable" / "Data Unavailable" / "No data" /
  "—" / "TBD" / pure punctuation — those are rejected by the validator
  anyway.
</hard_constraints>`;

type MetricPromptBaseArgs = {
  input: MetricPromptInput;
  knowledge: KnowledgePackContents;
  workspace: AgentWorkspace;
  memoryContext?: string;
  /** True when the backend supports `mcp_neko_memory_search`. */
  supportsMemorySearch?: boolean;
};

type MetricPromptDataAccess =
  | { queryTool: string; dataAgentTool?: never }
  | { queryTool?: never; dataAgentTool: string };

export function buildMetricPrompt(
  args: MetricPromptBaseArgs & MetricPromptDataAccess,
): string {
  const {
    input,
    knowledge,
    workspace,
    queryTool,
    dataAgentTool,
    memoryContext,
    supportsMemorySearch,
  } = args;
  const sections: string[] = [
    `<role>
You answer ONE dashboard card using the configured read-only GraphJin
data capability, then return a single JSON object describing the
snapshot. No prose around the JSON.
</role>`,
    // saveMode "none" — one-shot agent never writes memories itself; the
    // operator does that explicitly via `save:`. searchTool is wired
    // when the backend supports MCP so the agent can look beyond the
    // preloaded top-5 globals.
    buildMemorySection({
      searchTool: supportsMemorySearch ?? false,
      saveMode: "none",
      memoryContext,
    }),
    buildDataAccessSection({
      queryTool,
      agentTool: dataAgentTool,
      agentPurpose: "metric",
      workspace,
      knowledge,
      readOnly: true,
      // One-shot agent can't iterate — inline the full pack.
      inlineKnowledge: "all",
    }),
    METRIC_TIME_WINDOW,
    METRIC_MOOD_CHART,
    metricHardConstraints(Boolean(queryTool)),
    METRIC_OUTPUT_CONTRACT,
    `<input>
The card you must answer:

${JSON.stringify(
  {
    ...(input.question ? { userQuestion: input.question } : {}),
    cardTitle: input.title,
    cardRationale: input.why,
    cardRole: input.role,
    cardSlug: input.slug,
    chartHint: input.chartHint,
  },
  null,
  2,
)}
</input>`,
  ];
  return sections.join("\n\n");
}
