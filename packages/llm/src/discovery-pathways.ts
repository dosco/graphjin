import type { ExecRole } from "./metric-agent";

/**
 * GJ3 — the discovery-pathways supplier. Replaces "dump the whole schema
 * into the prompt" with a thin, role-shaped warm-start for catalog-first
 * discovery: curated entrypoints, persona-relevant seed searches, and
 * preferred query templates. Derived today from the knowledge pack's
 * insights.json; once GJ4 attaches the per-run caller-aware catalog MCP,
 * these become gj_catalog entrypoint/query_pattern rows and this module
 * keeps the same surface.
 */

export type DiscoveryPathways = {
  /** Hub tables / curated starting points for discovery. */
  entrypoints: string[];
  /** Seed searches the role should issue first (intent phrases). */
  seedSearches: string[];
  /** Curated query templates relevant to the role. */
  queryTemplates: Array<{ title: string; note: string }>;
};

const ROLE_SEED_SEARCHES: Record<string, string[]> = {
  CEO: ["revenue by month", "top customers", "channel mix"],
  CFO: ["margin by product", "accounts receivable aging", "cost trends"],
  COO: ["order fulfillment lead time", "inventory below reorder point", "throughput"],
  CRO: ["sales by rep", "pipeline by stage", "quota attainment"],
  CMO: ["customer acquisition by segment", "retention cohorts", "campaign performance"],
  CIO: ["data freshness", "integration volumes", "system activity"],
  CPO: ["product adoption", "feature usage", "activation funnel"],
};

type CacheEntry = { value: DiscoveryPathways; expiresAt: number };
const cache = new Map<string, CacheEntry>();
const CACHE_TTL_MS = 60 * 60 * 1000;

/** Test hook. */
export function _clearDiscoveryPathwaysCacheForTesting(): void {
  cache.clear();
}

/**
 * Compute (or reuse) the pathways for an (org, role, intent) triple. The
 * cache bounds the cost of iterative × per-user card generation: parallel
 * card builds for the same role reuse one computation. Regenerated on
 * TTL, not per run; persona changes (CV3) will invalidate explicitly.
 */
export function getDiscoveryPathways(opts: {
  orgId: string;
  role: ExecRole | string;
  intent?: string;
  /** insights.json text from the knowledge pack (current supplier). */
  insightsJson: string;
}): DiscoveryPathways {
  const key = `${opts.orgId}\0${opts.role}\0${opts.intent ?? ""}`;
  const hit = cache.get(key);
  if (hit && hit.expiresAt > Date.now()) return hit.value;

  const value = derivePathways(opts);
  cache.set(key, { value, expiresAt: Date.now() + CACHE_TTL_MS });
  return value;
}

function derivePathways(opts: {
  role: ExecRole | string;
  intent?: string;
  insightsJson: string;
}): DiscoveryPathways {
  let entrypoints: string[] = [];
  const queryTemplates: Array<{ title: string; note: string }> = [];
  try {
    const insights = JSON.parse(opts.insightsJson) as {
      hub_tables?: Array<{ table?: string; name?: string }>;
      query_templates?: Array<{ title?: string; description?: string; note?: string }>;
    };
    entrypoints = (insights.hub_tables ?? [])
      .map((h) => h.table ?? h.name ?? "")
      .filter(Boolean)
      .slice(0, 12);
    for (const t of (insights.query_templates ?? []).slice(0, 8)) {
      if (t.title) {
        queryTemplates.push({
          title: t.title,
          note: t.description ?? t.note ?? "",
        });
      }
    }
  } catch {
    // Malformed/absent insights: pathways degrade to role seeds only.
  }
  const seedSearches = [
    ...(opts.intent ? [opts.intent] : []),
    ...(ROLE_SEED_SEARCHES[opts.role] ?? []),
  ].slice(0, 5);
  return { entrypoints, seedSearches, queryTemplates };
}

/**
 * The prompt block: a warm-start, not a schema dump. Tells the agent
 * where to START discovering, never what every table looks like.
 */
export function buildDiscoveryPathwaysSection(
  pathways: DiscoveryPathways,
): string {
  if (
    pathways.entrypoints.length === 0 &&
    pathways.seedSearches.length === 0 &&
    pathways.queryTemplates.length === 0
  ) {
    return "";
  }
  const lines = ["<discovery-pathways>"];
  if (pathways.entrypoints.length > 0) {
    lines.push(
      "Start discovery from these hub tables (inspect them before searching wider):",
      ...pathways.entrypoints.map((e) => `- ${e}`),
      "",
    );
  }
  if (pathways.seedSearches.length > 0) {
    lines.push(
      "Seed searches worth running first for this role:",
      ...pathways.seedSearches.map((s) => `- "${s}"`),
      "",
    );
  }
  if (pathways.queryTemplates.length > 0) {
    lines.push(
      "Curated query patterns (prefer adapting one over writing from scratch):",
      ...pathways.queryTemplates.map(
        (t) => `- ${t.title}${t.note ? ` — ${t.note}` : ""}`,
      ),
    );
  }
  lines.push("</discovery-pathways>");
  return lines.join("\n");
}
