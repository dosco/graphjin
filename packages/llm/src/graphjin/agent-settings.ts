// Org-wide GraphJin agent opt-in: OpenNeko owns GraphJin's agent provider,
// model and key while the opt-in is on. When it is off, GraphJin's agent
// block stays as the operator configured it.

import { readFile, rename, rm, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import {
  GRAPHJIN_AGENT_REUSE_PRIMARY,
  and,
  data_source,
  db,
  desc,
  eq,
  getGraphjinAgentRow,
  llm_provider_config,
} from "@neko/db";
import { isMap, parseDocument } from "yaml";
import { isPrimaryProvider } from "../config";
import { resolveAxModelRoute, type AxModelRoute } from "../provider-runtime";
import { maybeDecryptSecret } from "../secrets";
import { acquireGraphjinConfigLock } from "./persist-source-config";
import { GRAPHJIN_AGENT_MAX_STEPS } from "./agent";
import { applyGraphjinConfigUpdate } from "./config-apply";
import { requestGraphjinRestart } from "./restart";
import { mintGraphjinToken } from "./token";

/**
 * The key file next to GraphJin's config. GraphJin reads it for each agent
 * run (agent.api_key_file, relative to its config directory), so a new key
 * needs no restart.
 */
export const GRAPHJIN_AGENT_KEY_FILE = ".openneko-graphjin-agent-key";
/** Under the 180s OpenNeko waits for an answer, so GraphJin reports its own timeout. */
export const GRAPHJIN_AGENT_TIMEOUT_SECONDS = 170;
/** Each step is one quick catalog or query call; high effort made steps 3x slower. */
export const GRAPHJIN_AGENT_REASONING = "low";

export type GraphjinAgentModel = {
  provider: string;
  model: string;
  baseUrl?: string;
  apiKey: string;
};

type ProviderRow = {
  provider: string;
  model: string | null;
  config: Record<string, unknown> | null;
  secrets: Record<string, unknown> | null;
};

/** GraphJin's agent is built on Ax, so it takes Ax provider profiles. */
export function graphjinAgentModelFromRow(row: ProviderRow): GraphjinAgentModel {
  const model = row.model?.trim();
  if (!model || !isPrimaryProvider(row.provider)) {
    throw new Error("The GraphJin agent needs a supported provider and a model.");
  }
  const route: AxModelRoute = resolveAxModelRoute({ provider: row.provider, model, config: row.config });
  // GraphJin has no Azure resource fields; Ax derives the endpoint from base_url.
  const baseUrl = route.options?.resource_name
    ? `https://${route.options.resource_name}.openai.azure.com/openai/deployments/${encodeURIComponent(route.model)}`
    : route.url;
  const apiKey = maybeDecryptSecret(row.secrets?.apiKey) || "none";
  return { provider: route.provider, model: route.model, ...(baseUrl ? { baseUrl } : {}), apiKey };
}

async function primaryRow(orgId: string): Promise<ProviderRow | null> {
  const rows = await db()
    .select({
      provider: llm_provider_config.provider,
      model: llm_provider_config.model,
      config: llm_provider_config.config,
      secrets: llm_provider_config.secrets,
      enabled: llm_provider_config.enabled,
    })
    .from(llm_provider_config)
    .where(and(eq(llm_provider_config.org_id, orgId), eq(llm_provider_config.scope, "primary")))
    .limit(1);
  const row = rows[0] as (ProviderRow & { enabled: boolean }) | undefined;
  return row?.enabled ? row : null;
}

/** The primary provider as GraphJin agent settings; throws when it is not configured. */
export async function primaryGraphjinAgentModel(orgId: string): Promise<GraphjinAgentModel> {
  const primary = await primaryRow(orgId);
  if (!primary) throw new Error("The GraphJin agent reuses the primary provider, which is not configured.");
  return graphjinAgentModelFromRow(primary);
}

/** The model GraphJin's agent should use, or null when the opt-in is off. */
export async function resolveGraphjinAgentModel(orgId: string): Promise<GraphjinAgentModel | null> {
  const row = await getGraphjinAgentRow(orgId);
  if (!row?.enabled) return null;
  if (row.provider === GRAPHJIN_AGENT_REUSE_PRIMARY) return primaryGraphjinAgentModel(orgId);
  return graphjinAgentModelFromRow(row);
}

/**
 * The agent settings OpenNeko owns while the opt-in is on. gj_config applies
 * all of them live (graphjin#655).
 */
export function graphjinAgentSettings(model: Omit<GraphjinAgentModel, "apiKey">): Record<string, unknown> {
  return {
    enabled: true,
    provider: model.provider,
    model: model.model,
    base_url: model.baseUrl ?? "",
    api_key_file: GRAPHJIN_AGENT_KEY_FILE,
    read_only: true,
    max_steps: GRAPHJIN_AGENT_MAX_STEPS,
    timeout_seconds: GRAPHJIN_AGENT_TIMEOUT_SECONDS,
    reasoning: GRAPHJIN_AGENT_REASONING,
  };
}

/**
 * Write the agent block to GraphJin's config file, which GraphJin reads at its
 * next start. The running GraphJin gets the same settings through gj_config.
 */
export function patchGraphjinAgentBlock(
  yamlText: string,
  model: Omit<GraphjinAgentModel, "apiKey">,
): { content: string; changed: boolean } {
  const document = parseDocument(yamlText);
  const before = document.get("agent");
  const current = isMap(before) ? (before.toJSON() as Record<string, unknown>) : {};
  const next: Record<string, unknown> = { ...current, ...graphjinAgentSettings(model) };
  if (!model.baseUrl) delete next.base_url;
  // The key comes from the file; an api_key_env from an older OpenNeko is dropped.
  delete next.api_key_env;
  const changed =
    "api_key_env" in current ||
    ["enabled", "provider", "model", "base_url", "api_key_file", "read_only", "max_steps", "timeout_seconds", "reasoning"].some((key) => current[key] !== next[key]);
  if (!changed) return { content: yamlText, changed: false };
  document.set("agent", document.createNode(next));
  return { content: document.toString(), changed: true };
}

async function writePrivate(path: string, content: string, mode: number): Promise<void> {
  const temporary = `${path}.${process.pid}.${Date.now()}.tmp`;
  await writeFile(temporary, content, { mode });
  try {
    await rename(temporary, path);
  } catch (error) {
    await rm(temporary, { force: true });
    throw error;
  }
}

async function graphjinEndpoint(orgId: string): Promise<string | null> {
  const [row] = await db()
    .select({ graphql_url: data_source.graphql_url })
    .from(data_source)
    .where(eq(data_source.org_id, orgId))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);
  return row?.graphql_url ?? null;
}

/**
 * Apply the opt-in: write the key file, which GraphJin reads for each agent
 * run, write the agent block to the config file for GraphJin's next start, and
 * apply the same block to the running GraphJin through gj_config. Nothing
 * restarts. A GraphJin too old to apply the block live (before graphjin#655)
 * is restarted instead.
 */
export async function provisionGraphjinAgent(
  orgId: string,
  configFile = process.env.OPENNEKO_GRAPHJIN_CONFIG?.trim(),
  apply: (endpoint: string, update: Record<string, unknown>) => Promise<unknown> = (endpoint, update) =>
    applyGraphjinConfigUpdate({
      endpoint,
      headers: { authorization: `Bearer ${mintGraphjinToken({ orgId, userId: "graphjin-agent-settings", role: "admin", ttlSeconds: 120 })}` },
      update,
    }),
): Promise<void> {
  if (!configFile) return;
  const model = await resolveGraphjinAgentModel(orgId);
  if (!model) return;
  const release = await acquireGraphjinConfigLock({ configFile });
  let changed = false;
  try {
    const keyFile = join(dirname(configFile), GRAPHJIN_AGENT_KEY_FILE);
    const currentKey = await readFile(keyFile, "utf8").catch(() => null);
    if (currentKey !== model.apiKey) await writePrivate(keyFile, model.apiKey, 0o600);
    const patch = patchGraphjinAgentBlock(await readFile(configFile, "utf8"), model);
    if (patch.changed) await writePrivate(configFile, patch.content, 0o664);
    changed = patch.changed;
  } finally {
    await release();
  }
  if (!changed) return;
  const endpoint = await graphjinEndpoint(orgId);
  if (!endpoint) return;
  try {
    await apply(endpoint, { serv: { agent: graphjinAgentSettings(model) } });
  } catch (error) {
    if (!/serv\.agent\.\S+ is not writable/.test(error instanceof Error ? error.message : String(error))) throw error;
    await requestGraphjinRestart(configFile, endpoint);
  }
}
