/**
 * Industry researcher agent — Perplexity sonar-deep-research mode.
 *
 * Two-stage pipeline:
 *
 *   1. Mission writer (GLM-5 via Vertex MaaS) — reads the just-built
 *      business_profile and writes a tailored research charter naming the
 *      industry, geographies, served-entity vocabulary, business model, and
 *      the dimensions to investigate. Output is a single paragraph in plain
 *      English, ~150-300 words, no preamble.
 *
 *   2. Deep research (Perplexity sonar-deep-research) — ONE call. The model
 *      internally plans, runs many web searches, reads sources, and synthesizes
 *      a long-form, citation-rich industry briefing. Takes minutes per call.
 *      We do not reinvent that loop with our own fan-out — sonar-deep-research
 *      IS the fan-out.
 *
 * Output: a markdown industry briefing that gets written into
 * customer_profile.industry_insights and read by downstream LLMs alongside
 * customer_profile.business_profile.
 */

import { ax } from "@ax-llm/ax";
import { modelFetch } from "./model-gateway";
import { and, db, eq, llm_provider_config } from "@neko/db";
import {
  type EditableProviderConfig,
  type ResearchProviderId,
  type StoredProviderConfigRow,
  getDefaultResearchModel,
  isResearchProvider,
  readResearchProviderConfigFromEnv,
} from "./config";
import { maybeDecryptSecret } from "./secrets";
import { buildLlm } from "./llm";

const PERPLEXITY_API_URL = "https://api.perplexity.ai/chat/completions";
const PERPLEXITY_MODEL = "sonar-deep-research";
// Perplexity deep-research can run for many minutes; cap so a hung request
// doesn't wedge a worker process forever.
const DEEP_RESEARCH_TIMEOUT_MS = 20 * 60 * 1000;

const MISSION_WRITER_INSTRUCTIONS = `You are a McKinsey associate scoping an
industry briefing for a partner. You will read a business_profile describing a
specific company and write a single research charter that a deep-research agent
will execute.

The charter must:

- Name the industry and sub-segments in plain language drawn from the
  business_profile. Do not invent industries the profile does not support.
- Name the geographies the company actually operates in (use the real country /
  region / city names from the profile, not generic phrases like "global").
- Use the served-entity vocabulary the company itself uses — patients, riders,
  members, accounts, tenants, students, customers — whichever the profile
  shows. Tell the researcher to keep that vocabulary throughout.
- Name the business model implied by the profile (transactional, subscription,
  asset-based, regulated, project-based, services, membership, etc.).
- Tell the researcher to investigate ALL of these dimensions, in this order:
  industry definition, market size & growth, competitive landscape (top
  players in THIS company's actual geographies), structural trends, regulatory
  environment, value chain economics, risks & disruption vectors.
- Tell the researcher to ground every claim in current public sources, prefer
  the most recent data available, and use the section headings:
  "Industry definition", "Market size & growth", "Competitive landscape",
  "Structural trends", "Regulatory environment", "Value chain & economics",
  "Risks & disruption vectors", "What a downstream LLM should hold in mind".
- Tell the researcher the briefing will be read by downstream LLMs FIRST and
  executives SECOND. Both want to scan, not read. So the output must be
  BULLET POINTS ONLY — every bullet a concrete number, named entity, date,
  or specific claim with an inline citation. No prose paragraphs. No narrative
  transitions. No "Overall" / "In summary" / "It is worth noting" sentences.
  Target 8-15 bullets per section, total briefing length MUST NOT exceed
  12,000 characters. Cut, do not pad.
  Marketing fluff is forbidden.

Hard rules:
- Output ONLY the charter as flowing prose. No headings, no bullets, no
  preamble like "Here is the charter:".
- 150-300 words.
- Address the researcher in second person ("Investigate...", "Cover...",
  "Use the vocabulary..."), not third person.
- If the business_profile is thin or vague on a dimension, instruct the
  researcher to use industry defaults appropriate to the named industry and
  geographies — do NOT make up facts about the company itself.`;

const DEEP_RESEARCH_SYSTEM_PROMPT = `You are a McKinsey-style industry analyst
writing for downstream LLMs FIRST and executives SECOND. Both want to scan,
not read.

CRITICAL — response shape:
- Your response IS the published briefing. It will be stored verbatim in a
  database column and rendered to users as markdown.
- Begin your response with the H1 heading the user specifies. NOTHING may
  precede it — no preface, no "Here is the briefing:", no recap of the
  mission, no acknowledgement.

Output rules — STRICT:
- Use exactly the section headings (H2) the user requests, in the order
  requested.
- Each section is BULLET POINTS ONLY. No introductory paragraph. No transition
  prose. No narrative.
- Each bullet is one discrete fact: a concrete number, a named
  company/regulator/geography, a date, or a specific claim. No filler. No
  "Overall," / "In summary," / "It is worth noting" sentences.
- Inline citations like [1] are encouraged on every bullet that makes a
  factual claim.
- Sub-bullets are allowed for grouping (e.g. by region or by player) but
  keep nesting to one level deep.
- Aim for 8-15 bullets per section. Total briefing length MUST NOT exceed
  12,000 characters. Density beats length. Cut, do not pad.
- Ground every claim in current public sources. Prefer the most recent data
  available.
- No marketing language, no hedging adjectives, no "robust ecosystem" /
  "vibrant market" / "compelling growth story" type prose.`;

export type IndustryResearcherProgress = (note: string) => void;

export type IndustryResearcherResult = {
  industryInsights: string;
  missionCharter: string;
};

type ProviderSource = "org" | "env" | "default" | "draft";

export type ResolvedResearchProviderConfig = {
  source: ProviderSource;
  provider: ResearchProviderId;
  model: string;
  enabled: boolean;
  config: Record<string, unknown>;
  secrets: Record<string, string>;
};

function defaultResearchConfig(): ResolvedResearchProviderConfig {
  return {
    source: "default",
    provider: "disabled",
    model: getDefaultResearchModel("disabled"),
    enabled: false,
    config: {},
    secrets: {},
  };
}

async function loadStoredResearchConfig(
  orgId: string,
): Promise<StoredProviderConfigRow | null> {
  try {
    const rows = await db()
      .select({
        id: llm_provider_config.id,
        org_id: llm_provider_config.org_id,
        scope: llm_provider_config.scope,
        provider: llm_provider_config.provider,
        model: llm_provider_config.model,
        label: llm_provider_config.label,
        enabled: llm_provider_config.enabled,
        config: llm_provider_config.config,
        secrets: llm_provider_config.secrets,
      })
      .from(llm_provider_config)
      .where(
        and(
          eq(llm_provider_config.org_id, orgId),
          eq(llm_provider_config.scope, "research"),
        ),
      )
      .limit(1);
    return (rows[0] as StoredProviderConfigRow | undefined) ?? null;
  } catch {
    return null;
  }
}

function decryptSecrets(row: StoredProviderConfigRow | null): Record<string, string> {
  const secrets = row?.secrets ?? {};
  const decoded: Record<string, string> = {};
  for (const [key, value] of Object.entries(secrets)) {
    const decrypted = maybeDecryptSecret(value);
    if (decrypted) decoded[key] = decrypted;
  }
  return decoded;
}

function validateResearchConfig(config: ResolvedResearchProviderConfig): void {
  if (!config.enabled || config.provider === "disabled") return;
  if (config.provider === "perplexity" && !config.secrets.apiKey) {
    throw new Error("Perplexity research provider requires an API key");
  }
}

export async function resolveResearchProviderConfig(
  orgId?: string,
  draft?: EditableProviderConfig,
): Promise<ResolvedResearchProviderConfig> {
  if (draft) {
    if (draft.scope !== "research") {
      throw new Error(`Expected research config draft, got ${draft.scope}`);
    }

    let mergedSecrets: Record<string, string> = { ...draft.secrets };
    let mergedConfig: Record<string, unknown> = { ...draft.config };
    if (orgId) {
      const stored = await loadStoredResearchConfig(orgId);
      if (stored && isResearchProvider(stored.provider) && stored.provider === draft.provider) {
        const storedSecrets = decryptSecrets(stored);
        mergedSecrets = { ...storedSecrets, ...draft.secrets };
        const storedConfig = (stored.config as Record<string, unknown>) ?? {};
        mergedConfig = { ...storedConfig, ...draft.config };
      }
    }

    const resolved: ResolvedResearchProviderConfig = {
      source: "draft",
      provider: draft.provider,
      model: draft.model || getDefaultResearchModel(draft.provider),
      enabled: draft.enabled,
      config: mergedConfig,
      secrets: mergedSecrets,
    };
    validateResearchConfig(resolved);
    return resolved;
  }

  if (orgId) {
    const stored = await loadStoredResearchConfig(orgId);
    if (stored && isResearchProvider(stored.provider)) {
      const resolved: ResolvedResearchProviderConfig = {
        source: "org",
        provider: stored.provider,
        model: stored.model ?? getDefaultResearchModel(stored.provider),
        enabled: stored.enabled,
        config: (stored.config as Record<string, unknown>) ?? {},
        secrets: decryptSecrets(stored),
      };
      validateResearchConfig(resolved);
      return resolved;
    }
  }

  const env = readResearchProviderConfigFromEnv();
  if (env && env.scope === "research") {
    const resolved: ResolvedResearchProviderConfig = {
      source: "env",
      provider: env.provider,
      model: env.model || getDefaultResearchModel(env.provider),
      enabled: env.enabled,
      config: env.config,
      secrets: env.secrets,
    };
    validateResearchConfig(resolved);
    return resolved;
  }

  return defaultResearchConfig();
}

export async function runIndustryResearcher(args: {
  orgId: string;
  orgName: string;
  companyNote: string;
  businessProfile: string;
  onProgress?: IndustryResearcherProgress;
}): Promise<IndustryResearcherResult> {
  const { orgId, orgName, companyNote, businessProfile, onProgress } = args;
  const research = await resolveResearchProviderConfig(orgId);
  if (!research.enabled || research.provider === "disabled") {
    return { industryInsights: "", missionCharter: "" };
  }

  // 1. Mission writer (main LLM — vertex or anthropic per env).
  onProgress?.("Drafting research mission");
  const llm = await buildLlm(orgId);

  const missionWriter = ax(
    "companyName:string, companyNote:string, businessProfile:string -> missionCharter:string",
    { description: MISSION_WRITER_INSTRUCTIONS },
  );

  const missionResult = await missionWriter.forward(llm, {
    companyName: orgName,
    companyNote,
    businessProfile,
  });
  const missionCharter = String(missionResult.missionCharter ?? "").trim();
  if (!missionCharter) {
    throw new Error("mission writer returned empty charter");
  }
  console.log(
    `[industry] mission written (${missionCharter.length} chars)`,
  );
  onProgress?.(`Mission ready (${missionCharter.length} chars)`);

  // 2. Perplexity deep research.
  onProgress?.("Running deep research (sonar-deep-research, may take minutes)");
  const startedAt = Date.now();
  console.log(`[industry] sonar-deep-research call started`);

  const userPrompt = buildDeepResearchUserPrompt(orgName, missionCharter);
  const industryInsights = await runDeepResearch(userPrompt, research);

  const elapsedSec = ((Date.now() - startedAt) / 1000).toFixed(0);
  console.log(
    `[industry] sonar-deep-research completed in ${elapsedSec}s (${industryInsights.length} chars)`,
  );
  onProgress?.(`Deep research complete (${elapsedSec}s)`);

  return { industryInsights, missionCharter };
}

function buildDeepResearchUserPrompt(orgName: string, missionCharter: string): string {
  // The mission charter already contains the section headings and instructions;
  // we just frame it with the company name and ask for a markdown briefing
  // titled with the company.
  return `# Research mission

${missionCharter}

# Output

Produce the briefing as markdown. Your FIRST character must be the \`#\` of
the H1 title below. Nothing may precede it — no preamble, no <think>
block, no recap of this mission, no acknowledgement of these instructions.

# ${orgName} — Industry Insights

Then use the eight section headings (H2) listed in the mission, in the
order given.

# Format — STRICT

- Do NOT emit <think>…</think> tags, planning notes, step counts, search
  logs, or any meta-commentary. Output ONLY the finished briefing body.
- Sections are H2 headings followed immediately by a bulleted list. NO
  introductory paragraph under any heading. NO narrative prose. NO transition
  sentences.
- Each bullet = one discrete fact: a concrete number, a named
  company/regulator/geography/standard, a date, or a specific claim with
  inline citation.
- Aim for 8-15 bullets per section. Total briefing length MUST NOT exceed
  12,000 characters. Density beats length. Cut, do not pad.
- Sub-bullets are allowed for grouping (e.g. region breakdowns under a
  player, or per-country regulation), but keep nesting to one level only.
- Under "What a downstream LLM should hold in mind", give 5-7 bullets each
  naming a non-obvious industry-specific fact (with the number or named
  entity that anchors it) that should color how this company's KPIs are
  interpreted.
- No preamble before the H1. No trailing summary. No "Sources:" appendix —
  inline citations only.
- No marketing language. No "robust" / "vibrant" / "compelling" /
  "exciting growth story". Plain factual prose inside each bullet.`;
}

async function runDeepResearch(
  userPrompt: string,
  research: ResolvedResearchProviderConfig,
  attempt = 1,
): Promise<string> {
  try {
    const res = await modelFetch()(PERPLEXITY_API_URL, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${research.secrets.apiKey}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        model: research.model || PERPLEXITY_MODEL,
        reasoning_effort: "medium",
        messages: [
          { role: "system", content: DEEP_RESEARCH_SYSTEM_PROMPT },
          { role: "user", content: userPrompt },
        ],
      }),
      signal: AbortSignal.timeout(DEEP_RESEARCH_TIMEOUT_MS),
    });

    if (!res.ok) {
      const errBody = await res.text().catch(() => "");
      const transient = res.status >= 500 || res.status === 429;
      if (transient && attempt === 1) {
        console.warn(
          `[industry] perplexity ${res.status}, retrying once: ${errBody.slice(0, 200)}`,
        );
        return runDeepResearch(userPrompt, research, attempt + 1);
      }
      throw new Error(
        `perplexity ${res.status} ${res.statusText}: ${errBody.slice(0, 500)}`,
      );
    }

    const data = (await res.json()) as {
      choices?: Array<{ message?: { content?: string } }>;
      citations?: string[];
    };
    // sonar-deep-research with reasoning_effort can still emit a leading
    // <think>…</think> block despite prompt instructions. Belt-and-braces:
    // strip any such blocks before persisting. Matches across newlines,
    // multiple blocks, and is case-insensitive.
    const content = (data.choices?.[0]?.message?.content ?? "")
      .replace(/<think\b[^>]*>[\s\S]*?<\/think>\s*/gi, "")
      .trim();
    if (!content) {
      throw new Error("perplexity returned empty content");
    }

    // Append citation URLs as markdown reference definitions so [1], [2]
    // become clickable links when rendered with react-markdown.
    if (data.citations && data.citations.length > 0) {
      const refs = data.citations
        .map((url, i) => `[${i + 1}]: ${url}`)
        .join("\n");
      return `${content}\n\n${refs}`;
    }
    return content;
  } catch (e) {
    // Network error / abort. One retry.
    if (attempt === 1 && (e instanceof Error) && !e.message.startsWith("perplexity ")) {
      console.warn(`[industry] perplexity network error, retrying once: ${e.message}`);
      return runDeepResearch(userPrompt, research, attempt + 1);
    }
    throw e;
  }
}

export async function testResearchProvider(
  orgId?: string,
  draft?: EditableProviderConfig,
): Promise<{ provider: string; model: string; source: ProviderSource }> {
  const resolved = await resolveResearchProviderConfig(orgId, draft);
  if (!resolved.enabled || resolved.provider === "disabled") {
    return { provider: "disabled", model: "", source: resolved.source };
  }

  const res = await modelFetch()(PERPLEXITY_API_URL, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${resolved.secrets.apiKey}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      model: resolved.model || PERPLEXITY_MODEL,
      messages: [{ role: "user", content: "Reply with READY only." }],
    }),
    signal: AbortSignal.timeout(30_000),
  });

  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new Error(`perplexity ${res.status} ${res.statusText}: ${body.slice(0, 300)}`);
  }

  return {
    provider: resolved.provider,
    model: resolved.model,
    source: resolved.source,
  };
}
