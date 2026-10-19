/**
 * Per-org agent backend resolution.
 *
 * Ax Harness is the only agent backend. The agent row holds the org-wide
 * concurrency settings. Provider credentials are provisioned independently.
 * The non-secret primary provider/model selection is retained so callers can
 * attest it against the identity the live backend reports.
 */

import { and, db, eq, llm_provider_config } from "@neko/db";
import {
  AGENT_DEFAULT_GLOBAL_CAP,
  type AgentBackend,
} from "./agent-backend";
import type { AxBackendConfig } from "./agent-backends/ax";
import { isReasoningEffort } from "./agent-limits";
import { makeAgentBackend } from "./agent-runtime";
import { isPrimaryProvider } from "./config";
import { resolveAxModelRoute } from "./provider-runtime";

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

/**
 * Whether the org's agent may use the terminal. On unless an admin turned it
 * off at Admin → Settings → Agent; read for every run, so a change applies to
 * the next run.
 */
export async function resolveAgentTerminal(orgId: string): Promise<boolean> {
  const row = await loadRow(orgId, "agent");
  return (row?.config as { terminal?: unknown } | null)?.terminal !== false;
}

function positiveInt(value: unknown): number | undefined {
  return typeof value === "number" && Number.isInteger(value) && value > 0 ? value : undefined;
}

/** Ax needs the route itself, not only the identity; the key is loaded per run. */
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

/**
 * Resolve the org's agent backend with its non-secret model route. The
 * provider key is loaded per run by the launcher and is never returned with
 * the backend. `model` overrides the primary model.
 */
export async function resolveAgentBackend(orgId: string, model?: string): Promise<AgentBackend> {
  const primary = await loadRow(orgId, "primary");
  return makeAgentBackend({ id: "ax", ax: axBackendConfig(model && primary ? { ...primary, model } : primary) });
}
