import type { HarnessObserver } from "@neko/telemetry";
import type { AgentBackend } from "./agent-backend";
import { resolveAgentBackend } from "./agent-backend-resolver";
import { observeAgentJob } from "./agent-job-telemetry";
import { parseJsonFromOutput } from "./agent-backends/output";
import { sandboxAgentBackendForJob } from "./work/sandbox-launcher";
import { ensureIsolatedJobWorkspace } from "./work/workspace";

export type BootstrapMetric = {
  role: string;
  slug: string;
  title: string;
  why: string;
  chart_hint: "kpi" | "line" | "bar" | "donut" | "area";
};

export type BootstrapMetricsProgress = (note: string) => void;

const VALID_CHARTS = new Set(["kpi", "line", "bar", "donut", "area"]);
const SLUG_RE = /^[a-z0-9]+(-[a-z0-9]+)*$/;

const ROLE_FOCUS: Record<string, string> = {
  CEO: "company-wide health, channel mix, geographic mix, product concentration.",
  CFO: "margin, cost, AR/cash, FX/currency exposure if multi-currency.",
  COO: "production capacity, work center throughput, fulfillment, lead times, workforce productivity.",
  CRO: "sales rep performance, quota attainment, pipeline, channel sales mix, account health.",
  CMO: "customer acquisition, retention, segment performance, marketing ROI, brand reach in their actual markets.",
  CIO: "systems reliability, data freshness, integration health, infrastructure cost, security posture relative to their actual stack.",
  CPO: "product adoption, feature usage, activation and retention of their actual product lines, roadmap signal quality.",
};

function buildPrompt(args: {
  orgName: string;
  businessProfile: string;
  industryInsights: string;
  seats: string[];
}): string {
  const { orgName, businessProfile, industryInsights, seats } = args;
  const total = seats.length * 4;
  const perRoleList = seats.map((r) => `4 for ${r}`).join(", ");
  const focus = seats
    .map(
      (r) =>
        `- ${r}: ${ROLE_FOCUS[r] ?? "executive priorities relevant to this role for this specific company."}`,
    )
    .join("\n");
  const insightsBlock = industryInsights.trim()
    ? industryInsights
    : "(industry research is disabled for this org — work from business_profile alone)";

  return `You are a senior BI consultant designing the first dashboard a new customer sees. You are handed a business_profile (what the company actually does) and an industry_insights briefing (industry context). You must propose exactly ${total} starter dashboard cards: 4 per role for ${seats.join(", ")}.

NO TOOLS NEEDED. Do not call any tool — not \`terminal\`/\`Bash\`, not \`read_file\`/\`Read\`, not \`write_file\`/\`Write\`, not \`execute_code\`, not search/web/browser tools — every fact you need is inline below. Generate the JSON directly as your final answer.

GROUNDING RULE — non-negotiable:
- Every card must measure something the company ACTUALLY does, sells, operates, or employs, as named in the business_profile (products, channels, geographies, workforce, currencies, work centers, sales territories, etc.).
- industry_insights is INTERPRETATION CONTEXT ONLY. It tells you what's normal in the industry. It does NOT give you license to invent product lines, markets, or segments the company is not in.
- Concrete negative example: if business_profile lists only mountain, road, and touring bikes, you MUST NOT propose e-bike metrics, battery metrics, or e-bike-related KPIs — even if industry_insights talks about e-bike growth. Same rule for any other industry trend the company isn't actually exposed to.
- If you can't ground a card in the business_profile, drop it and pick another.

Per-role focus:
${focus}

Field rules:
- role: must be exactly one of ${seats.join(", ")}.
- slug: kebab-case, lowercase, unique within its role, descriptive (e.g. "wholesale-vs-dtc-mix", "rep-quota-attainment", "wc-utilization"). Match /^[a-z0-9]+(-[a-z0-9]+)*$/.
- title: short, scannable, plain English (under 60 chars), no marketing fluff.
- why: one sentence explaining why this matters for THIS specific company, citing a concrete fact from the business profile (a number, a product name, a territory, a workforce count). No generic "important for executives" prose.
- chart_hint: one of "kpi" (single big number), "line" (trend), "bar" (categories), "donut" (mix/share), "area" (cumulative).

================================================================================
INPUT — companyName: ${orgName}
================================================================================

================================================================================
business_profile:
================================================================================

${businessProfile}

================================================================================
industry_insights:
================================================================================

${insightsBlock}

================================================================================
OUTPUT CONTRACT — respond with ONE JSON object, exactly this shape, no prose, no code fences:
================================================================================

{
  "metrics": [
    {
      "role": "CEO",
      "slug": "kebab-case-slug",
      "title": "Short title",
      "why": "One sentence citing a concrete fact from the business profile.",
      "chart_hint": "kpi"
    }
  ]
}

Rules for the JSON:
- Output a SINGLE JSON object. No markdown fences, no prose before or after.
- metrics array has exactly ${total} entries: ${perRoleList}.
- chart_hint must be one of: kpi, line, bar, donut, area.
- slug must be unique within its role.
`;
}

type BootstrapMetricsArgs = {
  orgId: string;
  orgName: string;
  businessProfile: string;
  industryInsights: string;
  seats: string[];
  jobId?: string;
  onProgress?: BootstrapMetricsProgress;
  debug?: boolean;
  observer?: HarnessObserver;
};

export function runBootstrapMetricsWriter(
  args: BootstrapMetricsArgs,
): Promise<{ metrics: BootstrapMetric[] }> {
  return observeAgentJob(
    {
      observer: args.observer,
      operationId: `bootstrap-metrics:${args.jobId ?? args.orgId}`,
      productPath: "bootstrap_metrics",
    },
    (observed) => writeBootstrapMetrics(args, observed),
  );
}

async function writeBootstrapMetrics(
  args: BootstrapMetricsArgs,
  observed: (backend: AgentBackend) => AgentBackend,
): Promise<{ metrics: BootstrapMetric[] }> {
  const { orgId, orgName, businessProfile, industryInsights, seats, jobId, onProgress, debug } = args;

  if (seats.length === 0) {
    throw new Error("bootstrap-metrics: no active seats selected");
  }

  const backend = await resolveAgentBackend(orgId);
  const isolated = await ensureIsolatedJobWorkspace(
    `bootstrap-metrics-${jobId ?? orgId}`,
  );
  try {
    const sandboxedBackend = await sandboxAgentBackendForJob({
      backend,
      orgId,
      runId: jobId ?? orgId,
      workspace: isolated.workspace,
      // Model-only: no GraphJin endpoint, memory broker, or durable org tree.
      access: {},
    });
    console.log(
      `[bootstrap-metrics] org=${orgId} backend=${backend.id} runtime=openshell`,
    );

    const prompt = buildPrompt({
      orgName,
      businessProfile,
      industryInsights,
      seats,
    });

    onProgress?.("Generating bootstrap metrics");
    const startedAt = Date.now();
    const result = await observed(sandboxedBackend).run({
      prompt,
      orgId,
      tag: jobId ?? orgId,
      workspace: isolated.workspace,
      debug: debug === true,
    });
    if (result.status !== "completed") {
      throw new Error(
        result.error ?? `${backend.id} returned status=${result.status}`,
      );
    }
    const stdout = result.finalText;
    const elapsedSec = ((Date.now() - startedAt) / 1000).toFixed(0);

    const parsed = parseJsonFromOutput(stdout) as { metrics?: unknown };
    const metrics = validate(parsed.metrics, seats);
    console.log(
      `[bootstrap-metrics] org=${orgId} generated ${metrics.length} cards for seats ${seats.join(", ")} in ${elapsedSec}s`,
    );
    onProgress?.(`Generated ${metrics.length} cards`);
    return { metrics };
  } finally {
    await isolated.cleanup();
  }
}

function validate(raw: unknown, seats: string[]): BootstrapMetric[] {
  if (!Array.isArray(raw)) {
    throw new Error(`bootstrap-metrics: expected array, got ${typeof raw}`);
  }
  const seatSet = new Set(seats);
  const metrics = raw as Partial<BootstrapMetric>[];
  const errs: string[] = [];
  const seenByRole = new Map<string, Set<string>>();
  const cleaned: BootstrapMetric[] = [];

  for (const [i, m] of metrics.entries()) {
    const where = `[${i}]`;
    if (!m || typeof m !== "object") {
      errs.push(`${where} not an object`);
      continue;
    }
    if (typeof m.role !== "string" || !seatSet.has(m.role)) {
      errs.push(`${where} bad role: ${m.role}`);
      continue;
    }
    if (typeof m.slug !== "string" || !SLUG_RE.test(m.slug)) {
      errs.push(`${where} bad slug: ${m.slug}`);
      continue;
    }
    if (typeof m.title !== "string" || !m.title.trim()) {
      errs.push(`${where} missing title`);
      continue;
    }
    if (typeof m.why !== "string" || !m.why.trim()) {
      errs.push(`${where} missing why`);
      continue;
    }
    if (!VALID_CHARTS.has(m.chart_hint as string)) {
      errs.push(`${where} bad chart_hint: ${m.chart_hint}`);
      continue;
    }
    const role = m.role;
    let bag = seenByRole.get(role);
    if (!bag) {
      bag = new Set();
      seenByRole.set(role, bag);
    }
    if (bag.has(m.slug)) {
      errs.push(`${where} duplicate slug ${m.slug} in role ${role}`);
      continue;
    }
    bag.add(m.slug);
    cleaned.push({
      role,
      slug: m.slug,
      title: m.title.trim(),
      why: m.why.trim(),
      chart_hint: m.chart_hint as BootstrapMetric["chart_hint"],
    });
  }

  for (const role of seats) {
    const n = seenByRole.get(role)?.size ?? 0;
    if (n !== 4) errs.push(`role ${role} has ${n} cards, expected 4`);
  }

  if (errs.length > 0) {
    throw new Error(
      `bootstrap-metrics validation failed:\n  ${errs.join("\n  ")}`,
    );
  }
  return cleaned;
}
