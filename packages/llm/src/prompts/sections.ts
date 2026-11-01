// Shared prompt sections used by every Neko agent prompt builder
// (work chat, metric agent, and — soon — workflow agents). Anything that
// describes how to talk to GraphJin, what memories to apply, or the
// canonical data-access rules belongs here so the wording can't drift
// between agents.

import type { KnowledgePackContents } from "../knowledge-pack";
import type { AgentWorkspace } from "../agent-backend";

export const GRAPHJIN_DATE_RULE = `- For GraphJin date/range filters, do not put multiple operators under
  the same column object. Use
  \`where: { and: [{ orderdate: { gte: "2024-06-30" } },
                   { orderdate: { lte: "2025-06-29" } }] }\`
  rather than \`where: { orderdate: { gte: "...", lte: "..." } }\`.
- House rule for relative periods. The anchor is the newest date in the
  measure's own date column (its max_<date_col>). "Latest N days" ends on
  and includes the anchor: gte anchor minus N-1 days, lt anchor plus 1 day.
  "Preceding" is the N days just before: gte anchor minus 2N-1 days, lt
  anchor minus N-1 days. A year or 12 months is 365 days, a month 30, a
  week 7. Never count back to the same calendar date. State the dates used.`;

// Anti-fanout rule we keep separately so it can be cited in the
// metric-agent's HARD CONSTRAINTS *and* in the chat agent's data-access
// rules without copy-paste drift. Born from the CEO #1 fact-check.
export const GRAPHJIN_FANOUT_RULE = `- A nested GraphJin response is flattened — one row per child. Summing
  the parent column from that flattened payload double-counts by N
  children per parent. To get a correct parent-side total either (a)
  sum at the parent root only with no nested children, (b) use
  \`distinct: [parent_id]\` to deduplicate first, or (c) split into two
  queries — one parent-side aggregate and one child-side aggregate.`;

// Cost + correctness rule: make GraphJin do the math. Pulling raw rows to
// count/sum them yourself burns tokens (thousands of rows in context) and
// invites the fanout error above. Shared by both data-access variants.
export const GRAPHJIN_AGGREGATE_RULE = `- Make the database do the math. When the answer is a count, sum,
  average, min/max, or a per-group breakdown, ask GraphJin for the
  aggregate (lift a \`patterns\` template) instead of fetching the raw
  rows and tallying them yourself — that returns a few numbers, not
  thousands of rows. Only pull raw rows when the user needs the rows
  themselves (e.g. "list the orders"); then page with \`limit\`/\`offset\`
  and take the smallest set that answers the question.`;

export type MemorySaveMode = "tool" | "fence" | "none";

export type MemorySectionOptions = {
  /** True when the agent has the `mcp_neko_memory_search` MCP tool. */
  searchTool: boolean;
  /**
   * How the agent can persist new memories:
   * - "tool": call `mcp_neko_memory_save`
   * - "fence": emit a ```neko_memory fenced block (parsed post-run)
   * - "none": agent does not write memories (operator does it explicitly)
   */
  saveMode: MemorySaveMode;
  /** Prefetched memory list (string). Undefined / empty → "no memories" placeholder. */
  memoryContext: string | undefined;
};

export function buildMemorySection(opts: MemorySectionOptions): string {
  const { searchTool, saveMode, memoryContext } = opts;

  const loaded = memoryContext?.trim()
    ? memoryContext.trim()
    : "No memories are currently saved for this workspace.";

  const application = `Apply these memories when relevant. They are
operator-validated rules and facts and **take precedence over default
behavior described elsewhere in this prompt** — if a memory contradicts
a default, the memory wins. When you act on a memory, briefly cite it
in your reasoning so the operator can verify (e.g.
"applied memory: don't sum from a flattened nested response"). Don't
silently ignore a relevant memory.`;

  const usageBlocks: string[] = [];

  if (searchTool) {
    usageBlocks.push(`To find related memories beyond the ones loaded above: call
\`mcp_neko_memory_search\` with a short natural-language query. Do
this whenever the user's request mentions a domain, metric, or rule
that isn't already covered by the preloaded list.`);
    usageBlocks.push(`The Library holds the documents and spreadsheets the operator
uploaded (policies, contracts, price lists, reports). When they ask about
company documents, agreements, written policy, or figures from a file,
call \`mcp_neko_library_search\` with a short query before answering from
general knowledge. It returns matching files with passages. To read a
whole file or compute from a sheet, ask your GraphJin data tools about
the file key — they read its text and run SQL over its tables. Name the
file and page, slide or sheet you relied on.`);
  }

  if (saveMode === "tool") {
    usageBlocks.push(`To save a new memory: call \`mcp_neko_memory_save\` with the
exact rule the operator stated. Use \`global\` scope unless they say
it's only for this thread.

To change a saved memory: call \`mcp_neko_memory_update\` with its id and
the whole new text. Do this when the operator corrects or adds to a rule
that is already saved, so one rule stays in one memory.`);
  } else if (saveMode === "fence") {
    usageBlocks.push(`To save a new memory mid-conversation, emit a fenced block:

\`\`\`neko_memory
[{ "save": { "text": "the exact rule the operator stated",
             "scope": "global", "kind": "business_rule",
             "pinned": true } }]
\`\`\`

The runtime parses the fence and persists each entry. Multiple
\`{ "save": ... }\` items in the array are allowed. The block is
removed from the user-visible output. Only emit this when the operator
explicitly says to remember/save something — never speculatively.`);
  }

  const usage = usageBlocks.length > 0 ? `\n\n${usageBlocks.join("\n\n")}` : "";

  return `<long_term_memory>
${loaded}

${application}${usage}
</long_term_memory>`;
}

export type DataAccessOptions = {
  /** Native execute_graphql tool on the brokered GraphJin MCP surface. */
  queryTool?: string;
  /** Identity the trusted broker uses when it executes the query. */
  queryIdentity?: "actor" | "service";
  /** Restrict this agent's use of otherwise caller-visible execution tools. */
  readOnly?: boolean;
  /** GraphJin server-agent tool, when the org opted in to GraphJin's agent. */
  agentTool?: string;
  /** Metric cards ask once for a value, a baseline and chart rows. */
  agentPurpose?: "metric";
  workspace: AgentWorkspace;
  knowledge: KnowledgePackContents;
  // 'syntax': inline only the DSL reference, point at the other knowledge
  // files for the agent to read on demand. Best for interactive paths.
  // 'all': inline tables + namespaces + insights + syntax. Best for
  // one-shot agents (metric, single-card) that can't iterate.
  inlineKnowledge: "syntax" | "all";
};

export function buildDataAccessSection(opts: DataAccessOptions): string {
  if (opts.queryTool && opts.agentTool) {
    throw new Error("choose either queryTool or agentTool, not both");
  }
  if (opts.agentTool) return buildBrokeredAgentDataAccessSection(opts);
  if (opts.queryTool) return buildBrokeredDataAccessSection(opts);
  throw new Error("GraphJin data access requires a native broker tool");
}

function buildBrokeredAgentDataAccessSection(opts: DataAccessOptions): string {
  const { agentTool } = opts;
  if (!agentTool) throw new Error("agent data access requires agentTool");
  const metric = opts.agentPurpose === "metric";

  const ask = metric
    ? `Call \`${agentTool}\` once. Include the card title and rationale, anchor
dates to the latest date in live data, ask for the current value and a
comparable baseline in the same request, and ask for the smallest grouped result
that can populate chartData. You own the final OpenNeko JSON object; ask
GraphJin for the data, not for that format.`
    : `Call \`${agentTool}\` once per data request. Use more calls for follow-up
requests, and put what you learned in each new request: GraphJin's agent keeps
no memory between calls.`;

  return `<data_access>
The configured GraphJin database is the authoritative source for operational
questions. When the user attaches a file or explicitly references uploaded
data, read the file and use it as the source of truth for that turn. Otherwise
default to the database.

GraphJin's agent writes and validates the queries and runs them under the
configured source identity. You direct it. ${ask}

${AGENT_REQUEST_GUIDANCE}${agentSchemaDigest(opts.knowledge, opts.workspace)}

The response has status, answer, data and evidence. Base every number on
response.data and response.evidence; the answer field explains the result but
does not replace them. If the response is blocked, denied, has errors, or lacks
enough evidence, say so and do not invent a number. Retry only when its
structured refusal says retryable and gives a concrete unblock step. If
GraphJin returns no rows, there is no data.
</data_access>`;
}

export const AGENT_REQUEST_GUIDANCE = `Write each instruction as a precise data request. Name the measure and how to
compute it, the tables, the join path, the filters, the time window and the
grouping. Ask for aggregates, not raw rows. For example: "Count
sales.salesorderheader rows per sales.salesterritory.name, joined on
territoryid. Return each territory with its count, highest first."`;

function tablesBySchema(raw: string): string {
  let tables: Array<{ id?: string; schema?: string; name?: string }>;
  try {
    const parsed = JSON.parse(raw) as { tables?: typeof tables };
    tables = Array.isArray(parsed.tables) ? parsed.tables : [];
  } catch {
    return "";
  }
  const bySchema = new Map<string, string[]>();
  for (const t of tables) {
    const qualified = t.schema && t.name ? `${t.schema}.${t.name}` : t.id?.split(":").at(-1) ?? t.name;
    if (!qualified) continue;
    const dot = qualified.lastIndexOf(".");
    const schema = dot > 0 ? qualified.slice(0, dot) : "";
    bySchema.set(schema, [...(bySchema.get(schema) ?? []), qualified.slice(dot + 1)]);
  }
  return [...bySchema].map(([schema, names]) => `- ${schema ? `${schema}: ` : ""}${names.join(", ")}`).join("\n").slice(0, TABLE_DIGEST_MAX_CHARS);
}

/** Tables and join paths the outer agent names in its requests to GraphJin's agent. */
export function agentSchemaDigest(knowledge: KnowledgePackContents, workspace: AgentWorkspace): string {
  const tables = tablesBySchema(knowledge.tables);
  if (!tables) return "";
  const joins = compactInsightsDigest(knowledge.insights, false);
  return `

Tables (columns for every table are in ${workspace.knowledgeRoot}/tables.json):

${tables}${joins ? `

Main tables and join paths:

${joins}` : ""}`;
}

function buildBrokeredDataAccessSection(opts: DataAccessOptions): string {
  const {
    queryTool,
    knowledge,
    queryIdentity = "service",
    readOnly = false,
  } = opts;
  if (!queryTool) throw new Error("brokered data access requires queryTool");
  const graphjinTool = (name: string) =>
    queryTool.replace(/execute_graphql$/, name);
  const catalogTool = graphjinTool("query_catalog");
  const helpTool = graphjinTool("graphql_help");
  const validateTool = graphjinTool("validate_where_clause");
  const savedQueryTool = graphjinTool("execute_saved_query");
  const agentic = knowledge.mode === "agentic";
  const knowledgeBlock = agentic
    ? `================================================================================
Tables visible to the service role (deeper detail via gj_catalog):
================================================================================

${compactTableDigest(knowledge.tables)}

================================================================================
API operations visible to the service role (load details via query_catalog):
==============================================================================

${compactApiOperationDigest(knowledge.insights)}

==============================================================================
Hub tables and ready query templates:
================================================================================

${compactInsightsDigest(knowledge.insights)}

================================================================================
Help-card index (load detail with gj_catalog(id: "help:<topic>")):
================================================================================

${compactHelpCardIndex(knowledge.insights)}

================================================================================
GraphJin DSL essentials:
================================================================================

${knowledge.syntax}`
    : `================================================================================
Tables — prefetched schema summary:
================================================================================

${knowledge.tables}

================================================================================
Namespaces:
================================================================================

${knowledge.namespaces}

================================================================================
Insights — relationships and query templates:
================================================================================

${knowledge.insights}

================================================================================
GraphJin DSL reference:
================================================================================

${knowledge.syntax}`;

  return `<data_access>
The configured GraphJin server is the authoritative source for operational data.
Its complete caller-visible MCP tool catalog is brokered into this run with the
native tool names and schemas. The source URL and short-lived
${queryIdentity === "actor" ? "actor credential" : "service credential"} never enter this run.

For a goal-driven request, start with \`${catalogTool}\` using the user's natural
language instruction. When the tool list has no \`${catalogTool}\`, start with
\`${helpTool}\` and \`for\` set to \`discovery\` instead:

  { "search": "<the user's business question>" }

Inspect the best returned row with \`{ "id": "<returned id>" }\`. For complete
table columns, query kind \`column\` filtered by the returned table_name; for
joins, search kind \`relationship\`. Use \`${helpTool}\` with \`for\` set to
\`discovery\`, \`schema\`, \`query\`, or \`mutations\` only when intent-first
catalog search does not provide enough guidance. Never guess a table, field,
relationship, or GraphJin operator.

Validate non-trivial filters with \`${validateTool}\`. Prefer an approved saved
query through \`${savedQueryTool}\` when the catalog supplies one. Otherwise,
execute GraphQL by calling \`${queryTool}\` with:

  { "query": "<your GraphQL operation>" }

GraphJin itself applies the caller's role, source capabilities, and tool gates.
Treat a refusal or unavailable tool as authoritative.

${
  readOnly
    ? `This run is read-only. Never submit a mutation, configuration change, or
other state-changing operation even if the caller-visible server advertises it.`
    : `Database sources are read-only; the host rejects any mutation against
them. Use state-changing tools against API sources only when the operator's
request and the caller-visible GraphJin capability explicitly authorize the
operation.`
}

${
  agentic
    ? `Use query_catalog detail rows as the primary discovery contract. Read
details_json, examples_json, and edges_json before drafting a non-trivial query.`
    : `The complete prefetched schema and DSL context is inlined below. Use it
instead of attempting separate discovery or configuration commands.`
}

If a response contains an errors array, read the error, correct the GraphQL,
and call the same tool again. Do not repeat an unchanged failing query.

${GRAPHJIN_DATE_RULE}

${GRAPHJIN_FANOUT_RULE}

${GRAPHJIN_AGGREGATE_RULE}

- Keep query results small. Prefer server-side aggregation and grouped
  summaries over fetching raw rows.
- Never invent or interpolate. If a query returned no rows, the answer is
  "no data", not a guess.

${knowledgeBlock}
</data_access>`;
}

const TABLE_DIGEST_MAX_CHARS = 4_000;
const INSIGHTS_DIGEST_MAX_CHARS = 6_000;

/** Hub tables with ready query templates and join paths, from the agentic
 *  pack's insights.json — the part of the legacy pack that made first
 *  answers fast. Compact and hard-capped: NEVER inline raw pack JSON
 *  (a 26KB inline reproducibly hung the model stream). */
export function compactInsightsDigest(raw: string, templates = true): string {
  let hubs: Array<{
    name?: string;
    summary?: string;
    examples?: unknown[];
    join_paths?: unknown[];
  }>;
  try {
    const parsed = JSON.parse(raw) as { hub_tables?: typeof hubs };
    hubs = Array.isArray(parsed.hub_tables) ? parsed.hub_tables : [];
  } catch {
    return "";
  }
  if (hubs.length === 0) return "";
  let out = "";
  for (const hub of hubs) {
    let block = `## ${hub.name ?? "?"}${hub.summary ? ` — ${String(hub.summary).slice(0, 110)}` : ""}\n`;
    for (const path of (hub.join_paths ?? []).slice(0, 6)) {
      block += `  join: ${String(path).slice(0, 140)}\n`;
    }
    for (const ex of templates ? (hub.examples ?? []).slice(0, 2) : []) {
      const q =
        typeof ex === "string"
          ? ex
          : ((ex as { query?: string }).query ?? JSON.stringify(ex));
      block += `  template: ${q.replace(/\s+/g, " ").slice(0, 300)}\n`;
    }
    if (out.length + block.length > INSIGHTS_DIGEST_MAX_CHARS) break;
    out += block;
  }
  return out.trimEnd();
}

const HELP_INDEX_MAX_CHARS = 2_000;
const API_OPERATION_INDEX_MAX_CHARS = 4_000;

/** Keep catalog-backed API operations visible in an API-only source. */
export function compactApiOperationDigest(raw: string): string {
  let operations: Array<{ name?: string; summary?: string }>;
  try {
    const parsed = JSON.parse(raw) as { api_operations?: typeof operations };
    operations = Array.isArray(parsed.api_operations) ? parsed.api_operations : [];
  } catch {
    return "";
  }
  let out = "";
  for (const operation of operations) {
    if (!operation.name) continue;
    const line = `- ${operation.name}${operation.summary ? ` — ${String(operation.summary).slice(0, 90)}` : ""}\n`;
    if (out.length + line.length > API_OPERATION_INDEX_MAX_CHARS) break;
    out += line;
  }
  return out.trimEnd();
}

/** One line per help card from the agentic pack's insights file. The raw
 *  file also carries hub_tables (rendered separately by
 *  compactInsightsDigest) — inlining it verbatim duplicates that and
 *  reinflates the prompt the digests exist to shrink. */
export function compactHelpCardIndex(raw: string): string {
  let cards: Array<{ id?: string; summary?: string }>;
  try {
    const parsed = JSON.parse(raw) as { help_cards?: typeof cards };
    cards = Array.isArray(parsed.help_cards) ? parsed.help_cards : [];
  } catch {
    return "";
  }
  let out = "";
  for (const card of cards) {
    if (!card.id) continue;
    const line = `- ${card.id}${card.summary ? ` — ${String(card.summary).slice(0, 90)}` : ""}\n`;
    if (out.length + line.length > HELP_INDEX_MAX_CHARS) break;
    out += line;
  }
  return out.trimEnd();
}

/** One short line per table from the pack's tables file (legacy or agentic
 *  shape), hard-capped — a prompt block, not a schema dump. */
export function compactTableDigest(raw: string): string {
  let lines: string[];
  try {
    const parsed = JSON.parse(raw) as {
      tables?: Array<{
        name?: string;
        schema?: string;
        database?: string;
        column_count?: number;
        summary?: string;
        id?: string;
      }>;
    };
    const tables = Array.isArray(parsed.tables) ? parsed.tables : [];
    if (tables.length === 0) throw new Error("no tables array");
    lines = tables.map((t) => {
      const name = [t.schema, t.name].filter(Boolean).join(".") || t.id || "?";
      const extra = t.summary
        ? ` — ${String(t.summary).slice(0, 60)}`
        : t.column_count != null
          ? ` (${t.column_count} cols)`
          : "";
      return `- ${name}${extra}`;
    });
  } catch {
    lines = raw.split("\n").filter((l) => l.trim());
  }
  let out = "";
  let dropped = 0;
  for (const line of lines) {
    if (out.length + line.length + 1 > TABLE_DIGEST_MAX_CHARS) {
      dropped = lines.length - out.split("\n").filter(Boolean).length;
      break;
    }
    out += line + "\n";
  }
  if (dropped > 0) {
    out += `… ${dropped} more — list the rest via gj_catalog (kind: "table").\n`;
  }
  return out.trimEnd();
}

// Shared analysis time-saved guidance; work and workflows use the same schema.
export const VALUE_ESTIMATE_INSTRUCTIONS = `The time a data analyst or BI specialist would need to produce this answer
   from scratch — finding the right data, writing and validating the queries,
   and assembling the result. The operator got it from one plain-English
   question instead of briefing a specialist and waiting on the report.
   Estimate honestly in minutes, rounded down:

\`\`\`neko_value
{ "minutes_saved": 90, "basis": "Joined orders to products, ranked by revenue, cross-checked against returns — a half-day BI request" }
\`\`\`

   Anchors: a single metric lookup 15-30 · a multi-table breakdown or
   drill-down 45-120 · a multi-step diagnostic like "why did revenue drop"
   120-300. Use 0 for a check that surfaced nothing. If the clarification tool
   is unavailable and you must ask in prose, use 0; a clarification-tool turn
   emits no value block at all.
   An action you propose (an email, a purchase order) carries its own
   \`minutes_saved\`.`;

/** Ax delegation: the harness admits one child agent with the parent's tools. */
export const AX_DELEGATION = `<delegation>
Your code can call \`team.worker({task})\` to hand a focused subtask to a child
agent with a fresh context. Use it when a subtask needs many tool calls whose
raw results you do not need to keep, or for independent subtasks you can start
together. The child sees only the task, so include the business question,
source or table hints, constraints, and the result shape you want. The child
has your tools under the same policies and approvals, except that only you can
ask the user a question. Do the work yourself when a few direct tool calls
finish it.
</delegation>`;
