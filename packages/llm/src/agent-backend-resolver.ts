/**
 * Per-org agent backend resolution.
 *
 * The agent row holds the org-wide backend choice (Hermes or Ax) and the
 * concurrency settings. Provider credentials are provisioned independently.
 * The non-secret primary provider/model selection is retained so callers can
 * attest it against the identity the live backend reports.
 */

import { and, db, eq, llm_provider_config } from "@neko/db";
import {
  AGENT_DEFAULT_GLOBAL_CAP,
  type AgentBackend,
  type AgentBackendId,
  type AgentModelIdentity,
  DEFAULT_AGENT_BACKEND,
  isAgentBackendId,
} from "./agent-backend";
import type { AxBackendConfig } from "./agent-backends/ax";
import { isReasoningEffort } from "./agent-limits";
import { makeAgentBackend } from "./agent-runtime";
import { isPrimaryProvider } from "./config";
import { resolveAxModelRoute, resolveHermesProviderRuntime } from "./provider-runtime";

type StoredRow = {
  provider: string;
  model: string | null;
  enabled: boolean;
  config: Record<string, unknown> | null;
};

async function loadRow(orgId: string, scope: string): Promise<StoredRow | null> {
  try {
    const rows = await db()
      .select({
        provider: llm_provider_config.provider,
        model: llm_provider_config.model,
        enabled: llm_provider_config.enabled,
        config: llm_provider_config.config,
      })
      .from(llm_provider_config)
      .where(
        and(
          eq(llm_provider_config.org_id, orgId),
          eq(llm_provider_config.scope, scope),
        ),
      )
      .limit(1);
    return (rows[0] as StoredRow | undefined) ?? null;
  } catch {
    return null;
  }
}

export async function resolveAgentBackendId(orgId: string): Promise<AgentBackendId> {
  const row = await loadRow(orgId, "agent");
  const backend = (row?.config ?? {}).backend;
  if (typeof backend === "string" && isAgentBackendId(backend)) return backend;
  if (DEFAULT_AGENT_BACKEND === "ax" && !axBackendConfig(await loadRow(orgId, "primary"))) return "hermes";
  return DEFAULT_AGENT_BACKEND;
}

export type AgentConcurrency = {
  globalCap: number;
};

function readPositiveInt(
  raw: unknown,
  fallback: number,
  { min = 1, max = 1000 }: { min?: number; max?: number } = {},
): number {
  const n = Number(raw);
  if (!Number.isFinite(n)) return fallback;
  if (n < min || n > max) return fallback;
  return Math.floor(n);
}

/**
 * Resolves the agent's concurrency caps for this worker boot.
 *
 * Read order: DB (scope='agent') → default. The /admin/settings/agent UI is
 * the only source of truth.
 *
 * Note: pg-boss `batchSize` is fixed at `b.work()` time, so changes to
 * `globalCap` only take effect after the next worker restart.
 */
export async function resolveAgentConcurrency(orgId: string): Promise<AgentConcurrency> {
  const row = await loadRow(orgId, "agent");
  const cfg = (row?.config ?? {}) as { globalCap?: unknown };
  return {
    globalCap: readPositiveInt(cfg.globalCap, AGENT_DEFAULT_GLOBAL_CAP),
  };
}

function configuredHermesModelIdentity(
  row: StoredRow | null,
): AgentModelIdentity | undefined {
  const model = row?.model?.trim();
  if (!row?.enabled || !model || !isPrimaryProvider(row.provider)) {
    return undefined;
  }
  const runtime = resolveHermesProviderRuntime({
    provider: row.provider,
    model,
    config: row.config,
  });
  return { provider: runtime.provider, model: runtime.model };
}

function positiveInt(value: unknown): number | undefined {
  return typeof value === "number" && Number.isInteger(value) && value > 0 ? value : undefined;
}

/** Ax needs the route itself, not only the identity; the key stays in OpenShell. */
export function axBackendConfig(row: StoredRow | null): AxBackendConfig | undefined {
  const model = row?.model?.trim();
  if (!row?.enabled || !model || !isPrimaryProvider(row.provider)) return undefined;
  const config = row.config ?? {};
  return {
    route: resolveAxModelRoute({ provider: row.provider, model, config }),
    ...(positiveInt(config.context_window_tokens) ? { contextWindowTokens: positiveInt(config.context_window_tokens) } : {}),
    ...(positiveInt(config.max_output_tokens) ? { maxOutputTokens: positiveInt(config.max_output_tokens) } : {}),
    // Medium answered q08 in 63s against 153s at high, with the same answer.
    reasoningEffort: isReasoningEffort(config.reasoning_effort) ? config.reasoning_effort : "medium",
  };
}

/** Refuses the Ax switch when the primary provider has no Ax route. */
export async function assertAxSupported(orgId: string): Promise<void> {
  const primary = await loadRow(orgId, "primary");
  if (!axBackendConfig(primary)) {
    throw new Error("Configure and enable a primary model provider before switching to Ax.");
  }
}

/**
 * Resolve the org's backend and its non-secret configured model identity. The
 * real provider key remains independently provisioned through OpenShell and is
 * never returned with the backend.
 */
/** `model` overrides the primary model for Ax runs; Hermes reads its model from its provisioned config. */
export async function resolveAgentBackend(orgId: string, backendId?: AgentBackendId, model?: string): Promise<AgentBackend> {
  const [id, primary] = await Promise.all([
    backendId ?? resolveAgentBackendId(orgId),
    loadRow(orgId, "primary"),
  ]);
  if (id === "ax") return makeAgentBackend({ id, ax: axBackendConfig(model && primary ? { ...primary, model } : primary) });
  return makeAgentBackend({
    id,
    ...(primary
      ? { configuredIdentity: configuredHermesModelIdentity(primary) }
      : {}),
  });
}
