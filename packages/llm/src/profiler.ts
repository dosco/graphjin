import {
  agentTurnTimeoutMs,
  shellToolName,
  type AgentBackend,
  type AgentRunOptions,
  type AgentWorkspace,
} from "./agent-backend";
import type { HarnessObserver } from "@neko/telemetry";
import { resolveAgentBackend } from "./agent-backend-resolver";
import { observeAgentJob } from "./agent-job-telemetry";
import { runValidatedAgentTurn } from "./agent-validate-loop";
import {
  knowledgePackPaths,
  prefetchKnowledgeForOrg,
  readKnowledgePack,
  type KnowledgePackContents,
} from "./knowledge-pack";
import {
  AGENT_REQUEST_GUIDANCE,
  agentSchemaDigest,
  compactHelpCardIndex,
  compactInsightsDigest,
  compactTableDigest,
} from "./prompts/sections";
import { sandboxAgentBackendForJob } from "./work/sandbox-launcher";
import {
  ensureIsolatedJobWorkspace,
  ensureWorkWorkspace,
} from "./work/workspace";
import { graphjinAgentEnabledForOrg } from "@neko/db";
import {
  GRAPHJIN_AGENT_ASK_TOOL_TITLE,
  GRAPHJIN_EXECUTE_GRAPHQL_TOOL_TITLE,
  graphjinMcpToolTitle,
} from "./graphjin/mcp-names";

export function buildProfilerPrompt(args: {
  orgName: string;
  companyNote: string;
  knowledge: KnowledgePackContents;
  shellTool: string;
  queryTool: string;
  /** Set when the org opted in to GraphJin's server-side agent. */
  agentTool?: string;
  workspace: AgentWorkspace;
}): string {
  const { orgName, companyNote, knowledge, queryTool } = args;
  if (args.agentTool) return buildProfilerAgentInstructions(args.agentTool, agentSchemaDigest(knowledge, args.workspace)) + profilerOutput(orgName, companyNote);
  const agentic = knowledge.mode === "agentic";
  const catalogTool = graphjinMcpToolTitle("query_catalog");
  const helpTool = graphjinMcpToolTitle("graphql_help");
  const savedQueryTool = graphjinMcpToolTitle("execute_saved_query");
  const executeQuery = `call \`${queryTool}\` with {"query":"<your read-only graphql>"}`;
  const discoveryRule = agentic
    ? `1. The knowledge sections below are a SLIM role-aware bootstrap, not the whole schema. Start with \`${catalogTool}\` using {"search":"<the profiling goal>"}, then inspect the best returned ids for evidence, examples, and relationship edges. Use \`${helpTool}\` only when the catalog route is unclear. Prefer \`${savedQueryTool}\` when an approved query fits; never guess a field or relationship.`
    : `1. Read the prefetched GraphJin knowledge sections below before writing any query. They are the authoritative DSL + schema/relationship context for this database. Don't run schema-discovery commands; that context is already prefetched here.`;
  return `You build a short markdown business profile about a customer company by querying its database via GraphJin.

EXECUTION PATTERN:
${discoveryRule}
2. Skim the tables + insights sections to identify what this business actually does (industry, offering, business model). Pick the handful of tables that matter.
3. Run focused GraphQL queries to gather facts: main business event (date range, recent volume + value), top categories / products / services, geography, who is served, who does the work.
4. Run queries by ${executeQuery}.
5. If a response contains an "errors" array, read the GraphQL error and any errors[].extensions.graphjin_repair hint, then correct the query yourself.
6. When you have enough facts, emit the final markdown body exactly per the OUTPUT FORMAT. No prose around it, no code fences.

COMPLETION CONTRACT:
- Treat the requested output sections as an evidence checklist, not an invitation to exhaustively explore the schema.
- A section is ready when you have representative queried evidence for it, or the available source does not expose it and the section can honestly say "Not measured."
- Stop querying and synthesize the profile as soon as every section is ready. Additional segmentations, alternative tables, or marginal precision that would not change the short profile are out of scope.
- Do not get stuck repairing an optional fact. If a query cannot be corrected from the returned error and repair hint, use the evidence already collected and mark that fact "Not measured."

DATA ACCESS — READ-ONLY:
Use only the caller-visible GraphJin MCP tools. The trusted host supplies their exact schemas and keeps the source URL and credential outside the sandbox. This profiler run is read-only: never submit a mutation, configuration change, or other state-changing operation even if a listed tool could perform one. DO NOT use \`execute_code\`, a shell, Python, raw HTTP requests, or any other path to talk to GraphJin.

- Discover with \`${catalogTool}\` and inspect returned ids before querying data.
- Execute data reads through \`${savedQueryTool}\` or \`${queryTool}\`.
- Do not use configuration or write tools in this job.
- For join planning, use catalog table cards, relationship rows, details_json, examples_json, and edges_json.
- Do not use GraphJin dev tools or try to reach GraphJin outside its MCP surface.
- Never invent data — every number in the profile must trace back to a GraphJin query-tool response from this run.

QUERY CONSTRUCTION — let the database aggregate:
Prefer one bulk query with server-side aggregation (count, sum, avg) over multiple round-trips that pull rows back to the agent. Specific GraphJin capabilities to reach for (consult \`syntax.json\` for full DSL reference):

- Expression aggregates — sum(expr: {...}), ratio(expr: {...}) — USE THESE FIRST when a fact involves arithmetic across columns (e.g. SUM(price × qty)).
- Joined-column access via dot-notation: { col: "product.standardcost" } works across FKs up to 3 hops.
- For top-N by an aggregate, follow the prefetched syntax limitations. If GraphJin cannot order by an aggregate alias, fetch the grouped aggregate rows and sort the small result set in your reasoning.
- Global single-row aggregate: a top-level select whose fields are ALL aggregates collapses to one row, no distinct needed.

HARD CONSTRAINTS (violating any of these is a critical failure):
- Never hardcode calendar years; compute periods with relative arithmetic. If you need an anchor date, use a recent date from the data.
- For date/range filters, do not put multiple operators under the same column object. Use an explicit \`and\` array:
  \`where: { and: [{ orderdate: { gte: "2024-06-30" } }, { orderdate: { lte: "2025-06-29" } }] }\`
  not \`where: { orderdate: { gte: "...", lte: "..." } }\`.
- Never use a bare limit without pagination. Use cursor-based pagination to process all rows, or use GraphQL aggregation with distinct to let the database aggregate.
- Watch the silent 20-row default limit on every query level (top AND nested) — set explicit limit or use distinct+aggregation.
- Never invent or interpolate. If a query returned no rows, the answer is "Not measured.", not a guess.

${
  // The agentic pack files are raw catalog JSON sized for on-demand reads,
  // not for prompts — inlining them verbatim (~50KB+) is the inline class
  // that reproducibly hangs the first model stream. Same digests + caps
  // as the chat agent; deeper detail comes through gj_catalog on demand.
  agentic
    ? `================================================================================
Tables visible to your role (deeper detail via gj_catalog on demand):
================================================================================

${compactTableDigest(knowledge.tables)}

================================================================================
Hub tables — join paths and ready query templates (adapt, don't rediscover):
================================================================================

${compactInsightsDigest(knowledge.insights)}

================================================================================
Help-card index — pull any card's full guidance with gj_catalog(id: "help:<topic>"):
================================================================================

${compactHelpCardIndex(knowledge.insights)}

================================================================================
Query-DSL essentials — filters, query shape, aggregate patterns:
================================================================================

${knowledge.syntax}`
    : `================================================================================
Tables — every table in the database (name, schema, column_count):
================================================================================

${knowledge.tables}

================================================================================
Namespaces — multi-database routing context:
================================================================================

${knowledge.namespaces}

================================================================================
Insights — hub tables, hot relationships, relationship paths, query templates, data-quality flags:
================================================================================

${knowledge.insights}

================================================================================
Syntax — authoritative GraphJin DSL reference (operators, aggregations, pagination):
================================================================================

${knowledge.syntax}`
}

` + profilerOutput(orgName, companyNote);
}

function buildProfilerAgentInstructions(agentTool: string, schema: string): string {
  return `You build a short markdown business profile about a customer company from its database.

EXECUTION PATTERN:
1. Call \`${agentTool}\` with {"instruction":"<one precise data request>"}. GraphJin's agent runs validated read-only queries and returns status, answer, data and evidence. ${AGENT_REQUEST_GUIDANCE}
2. Ask first what the business does: its main business event with the date range, recent volume and value, and the tables that describe customers, products and locations.
3. Then ask focused questions for each output section: top categories, products or services; geography; who is served; who does the work. Put what you already learned in each instruction; GraphJin's agent keeps no memory between calls.
4. Anchor periods to the latest date in the data.
5. When you have enough facts, emit the final markdown body exactly per the OUTPUT FORMAT. No prose around it, no code fences.${schema}

COMPLETION CONTRACT:
- Treat the requested output sections as an evidence checklist, not an invitation to explore everything.
- A section is ready when you have representative evidence for it, or the data does not cover it and the section can honestly say "Not measured."
- Stop asking and write the profile as soon as every section is ready.
- If a response is blocked, denied, has errors, or lacks evidence, use what you already have and mark that fact "Not measured."
- Every number in the profile must come from response.data or response.evidence in this run. Never invent or interpolate.

`;
}

function profilerOutput(orgName: string, companyNote: string): string {
  return `================================================================================
OUTPUT FORMAT — respond with EXACTLY this markdown body, no code fences, no prose around it:
================================================================================

# ${orgName} — Business Profile

## What they are
1–2 sentences: industry, offering, business model.

## Who they serve
Recipients in the business's own terms (customers, patients, members, accounts, …) with counts.

## Where they operate
Geographic / facility footprint with real names.

## Scale
- Date range covered, recent volume + value
- Top revenue drivers
- People served and people who do the work

## Operational footprint
Business functions the data represents.

## What a downstream LLM should hold in mind
3–5 non-obvious facts about the company's shape.

Rules for the markdown:
- Every number must come from a query you actually ran.
- Use human-readable names from the data, never bare IDs.
- Match vocabulary to the business (a hospital has patients, not customers).
- No meta talk about databases, schemas, queries, or the dataset.
- If a fact isn't queryable, write "Not measured."
- Begin your response with the H1 \`# ${orgName} — Business Profile\`. NOTHING may precede it — no preamble, no code fence, no acknowledgement.

================================================================================
INPUT — the company you must profile:
================================================================================

${JSON.stringify(
  {
    companyName: orgName,
    companyNote,
  },
  null,
  2,
)}
`;
}

export type ProfilerProgress = (note: string) => void;

export type ProfilerResult = {
  businessProfile: string;
};

export function profilerTimeoutMs(): number {
  const env = Number(process.env.OPENNEKO_PROFILER_TIMEOUT_MS);
  return Number.isFinite(env) && env > 0 ? env : agentTurnTimeoutMs();
}

export function profilerAgentRunControls(): Pick<
  AgentRunOptions,
  "retries" | "timeoutMs"
> {
  return {
    retries: 0,
    timeoutMs: profilerTimeoutMs(),
  };
}

type ProfilerArgs = {
  orgId: string;
  mcpUrl: string;
  orgName: string;
  companyNote: string;
  jobId?: string;
  onProgress?: ProfilerProgress;
  debug?: boolean;
  observer?: HarnessObserver;
};

export function runProfiler(args: ProfilerArgs): Promise<ProfilerResult> {
  return observeAgentJob(
    {
      observer: args.observer,
      operationId: `profiler:${args.jobId ?? args.orgId}`,
      productPath: "profiler",
    },
    (observed) => profile(args, observed),
  );
}

async function profile(
  args: ProfilerArgs,
  observed: (backend: AgentBackend) => AgentBackend,
): Promise<ProfilerResult> {
  const { orgId, mcpUrl, orgName, companyNote, jobId, onProgress, debug } = args;

  const knowledgeWorkspace = await ensureWorkWorkspace(
    orgId,
    "profiler",
    jobId ?? orgId,
  );
  const agentPath = await graphjinAgentEnabledForOrg(orgId);
  const knowledge = await loadProfilerKnowledge(orgId, knowledgeWorkspace.knowledgeRoot);

  const backend = await resolveAgentBackend(orgId);
  const isolated = await ensureIsolatedJobWorkspace(
    `profiler-${jobId ?? orgId}`,
  );
  try {
    const sandboxedBackend = await sandboxAgentBackendForJob({
      backend,
      orgId,
      runId: jobId ?? orgId,
      workspace: isolated.workspace,
      access: agentPath ? { graphjinAgent: true } : { graphjinRead: true },
    });
    console.log(
      `[profiler] org=${orgId} backend=${backend.id} runtime=openshell`,
    );

    const prompt = buildProfilerPrompt({
      orgName,
      companyNote,
      knowledge,
      shellTool: shellToolName(backend.id),
      queryTool: GRAPHJIN_EXECUTE_GRAPHQL_TOOL_TITLE,
      ...(agentPath ? { agentTool: GRAPHJIN_AGENT_ASK_TOOL_TITLE } : {}),
      workspace: isolated.workspace,
    });

    if (onProgress) onProgress("Running profiler agent…");
    const startedAt = Date.now();
    // GJ2: iterative validation loop — a profile missing required sections
    // (or containing failure text) goes back to the agent for a corrective
    // turn instead of failing the onboarding job.
    const { value: businessProfile, finalText } = await runValidatedAgentTurn({
      backend: observed(sandboxedBackend),
      run: {
        prompt,
        orgId,
        tag: jobId ?? orgId,
        workspace: isolated.workspace,
        ...profilerAgentRunControls(),
        debug: debug === true,
      },
      label: `profiler org=${orgId}`,
      validate: (txt) => validateBusinessProfile(txt, orgName),
    });
    const elapsedSec = ((Date.now() - startedAt) / 1000).toFixed(0);
    console.log(
      `[profiler] org=${orgId} done in ${elapsedSec}s (${finalText.length} chars)`,
    );
    if (onProgress) onProgress("Profile drafted");

    return { businessProfile };
  } finally {
    await isolated.cleanup();
  }
}

function stripFences(raw: string): string {
  const trimmed = raw.trim();
  const fenced = trimmed.match(/^```(?:markdown|md)?\s*([\s\S]*?)\s*```\s*$/);
  return (fenced?.[1] ?? trimmed).trim();
}

const FAILURE_TEXT_RE =
  /\b(i am sorry|unable to connect|restricted network|business_profile\.md|execute the graphql query yourself|graphjin server|could not access|couldn't access|cannot access|no direct access)\b/i;

export function validateBusinessProfile(raw: string, _orgName: string): string {
  const profile = stripFences(raw);
  if (!profile) {
    throw new Error("profiler returned an empty business profile");
  }
  if (FAILURE_TEXT_RE.test(profile)) {
    throw new Error(
      "profiler returned failure text instead of a business profile",
    );
  }
  return profile;
}

async function loadProfilerKnowledge(orgId: string, knowledgeRoot: string) {
  const refresh = await prefetchKnowledgeForOrg(
    orgId,
    knowledgeRoot,
  );
  if (refresh.ok) {
    const totalBytes = refresh.files.reduce((n, f) => n + f.bytes, 0);
    console.log(
      `[profiler] org=${orgId} knowledge refreshed (${refresh.files.length} files, ${totalBytes}B)`,
    );
  } else {
    console.warn(
      `[profiler] org=${orgId} knowledge refresh failed (${refresh.error}); proceeding with on-disk pack`,
    );
  }
  return readKnowledgePack(
    knowledgePackPaths(knowledgeRoot),
  );
}
