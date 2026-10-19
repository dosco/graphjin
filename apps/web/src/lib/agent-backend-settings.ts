import "server-only";

import { and, db, eq, llm_provider_config } from "@neko/db";
import { AGENT_DEFAULT_GLOBAL_CAP } from "@neko/llm";

const AGENT_SCOPE = "agent";
export type AgentSettings = {
  source: "org" | "default";
  globalCap: number;
  /** Whether the agent may run shell commands in its sandbox. On by default. */
  terminal: boolean;
};

export type AgentSettingsPayload = {
  agent: AgentSettings;
  defaults: {
    globalCap: number;
    terminal: boolean;
  };
};

async function loadAgentRow(orgId: string): Promise<{
  id: string;
  config: Record<string, unknown> | null;
} | null> {
  const rows = await db()
    .select({
      id: llm_provider_config.id,
      config: llm_provider_config.config,
    })
    .from(llm_provider_config)
    .where(
      and(
        eq(llm_provider_config.org_id, orgId),
        eq(llm_provider_config.scope, AGENT_SCOPE),
      ),
    )
    .limit(1);
  return (
    (rows[0] as
      | { id: string; config: Record<string, unknown> | null }
      | undefined) ?? null
  );
}

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

export async function getAgentSettings(
  orgId: string,
): Promise<AgentSettings> {
  const row = await loadAgentRow(orgId);
  const cfg = (row?.config ?? {}) as { globalCap?: unknown; terminal?: unknown };
  return {
    source: row ? "org" : "default",
    globalCap: readPositiveInt(cfg.globalCap, AGENT_DEFAULT_GLOBAL_CAP),
    terminal: cfg.terminal !== false,
  };
}

export async function getAgentSettingsPayload(
  orgId: string,
): Promise<AgentSettingsPayload> {
  const agent = await getAgentSettings(orgId);
  return {
    agent,
    defaults: {
      globalCap: AGENT_DEFAULT_GLOBAL_CAP,
      terminal: true,
    },
  };
}

export type AgentSaveDraft = {
  globalCap?: number | string;
  terminal?: boolean;
};

export async function saveAgentSettingsDraft(
  orgId: string,
  draft: AgentSaveDraft,
): Promise<AgentSettings> {
  const existing = await loadAgentRow(orgId);
  const existingCfg = (existing?.config ?? {}) as { globalCap?: unknown; terminal?: unknown };
  const globalCap = readPositiveInt(
    draft.globalCap ?? existingCfg.globalCap,
    AGENT_DEFAULT_GLOBAL_CAP,
  );
  if (draft.terminal !== undefined && typeof draft.terminal !== "boolean") {
    throw new Error("terminal must be true or false");
  }
  const terminal = draft.terminal ?? existingCfg.terminal !== false;
  const config = { globalCap, terminal };

  if (existing) {
    await db()
      .update(llm_provider_config)
      .set({
        provider: "ax",
        config,
        updated_at: new Date(),
      })
      .where(eq(llm_provider_config.id, existing.id));
  } else {
    await db().insert(llm_provider_config).values({
      org_id: orgId,
      scope: AGENT_SCOPE,
      provider: "ax",
      enabled: true,
      config,
      secrets: {},
    });
  }

  return { source: "org", globalCap, terminal };
}
