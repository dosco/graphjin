import "server-only";

import {
  GRAPHJIN_AGENT_REUSE_PRIMARY,
  GRAPHJIN_AGENT_SCOPE,
  and,
  db,
  eq,
  getGraphjinAgentRow,
  graphjinAgentEnabled,
  llm_provider_config,
} from "@neko/db";
import {
  PRIMARY_PROVIDER_OPTIONS,
  getDefaultPrimaryModel,
  getPrimaryProviderFields,
  isPrimaryProvider,
  maskSecret,
  type SettingsField,
} from "@neko/llm/config";
import { graphjinAgentModelFromRow, primaryGraphjinAgentModel, provisionGraphjinAgent } from "@neko/llm/graphjin";
import { maybeDecryptSecret, maybeEncryptSecret } from "@neko/llm/secrets";

export type GraphjinAgentSettings = {
  enabled: boolean;
  reusePrimary: boolean;
  provider: string;
  model: string;
  config: Record<string, unknown>;
  secretStatus: Record<string, string>;
};

export type GraphjinAgentSettingsPayload = {
  settings: GraphjinAgentSettings;
  options: typeof PRIMARY_PROVIDER_OPTIONS;
  fields: Record<string, SettingsField[]>;
  defaults: Record<string, string>;
};

export type GraphjinAgentDraft = {
  enabled?: unknown;
  reusePrimary?: unknown;
  provider?: unknown;
  model?: unknown;
  config?: unknown;
  secrets?: unknown;
};

function decrypted(secrets: Record<string, unknown> | null | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(secrets ?? {})) {
    const plain = maybeDecryptSecret(value);
    if (plain) out[key] = plain;
  }
  return out;
}

export async function getGraphjinAgentSettings(orgId: string): Promise<GraphjinAgentSettings> {
  const row = await getGraphjinAgentRow(orgId);
  const reusePrimary = !row || row.provider === GRAPHJIN_AGENT_REUSE_PRIMARY;
  const provider = reusePrimary ? PRIMARY_PROVIDER_OPTIONS[0].value : row.provider;
  return {
    enabled: graphjinAgentEnabled(row),
    reusePrimary,
    provider,
    model: reusePrimary ? getDefaultPrimaryModel(provider as never) : row?.model ?? "",
    config: reusePrimary ? {} : row?.config ?? {},
    secretStatus: reusePrimary
      ? {}
      : Object.fromEntries(Object.entries(decrypted(row?.secrets)).map(([key, value]) => [key, maskSecret(value)])),
  };
}

export async function getGraphjinAgentSettingsPayload(orgId: string): Promise<GraphjinAgentSettingsPayload> {
  return {
    settings: await getGraphjinAgentSettings(orgId),
    options: PRIMARY_PROVIDER_OPTIONS,
    fields: Object.fromEntries(PRIMARY_PROVIDER_OPTIONS.map((o) => [o.value, getPrimaryProviderFields(o.value)])),
    defaults: Object.fromEntries(PRIMARY_PROVIDER_OPTIONS.map((o) => [o.value, getDefaultPrimaryModel(o.value)])),
  };
}

function stringRecord(value: unknown, label: string): Record<string, string | null> {
  if (value === undefined) return {};
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${label} must be an object`);
  const out: Record<string, string | null> = {};
  for (const [key, entry] of Object.entries(value)) {
    if (entry !== null && typeof entry !== "string") throw new Error(`${label}.${key} must be a string`);
    out[key] = entry === null ? null : entry.trim();
  }
  return out;
}

/** Saves the data path, then applies it to GraphJin. A provisioning error fails the save. */
export async function saveGraphjinAgentSettings(orgId: string, draft: GraphjinAgentDraft): Promise<GraphjinAgentSettings> {
  if (typeof draft.enabled !== "boolean" || typeof draft.reusePrimary !== "boolean") {
    throw new Error("enabled and reusePrimary must be booleans");
  }
  const existing = await getGraphjinAgentRow(orgId);
  let provider = GRAPHJIN_AGENT_REUSE_PRIMARY;
  let model: string | null = null;
  let config: Record<string, unknown> = {};
  let secrets: Record<string, string> = {};
  if (draft.reusePrimary && draft.enabled) await primaryGraphjinAgentModel(orgId);
  if (!draft.reusePrimary) {
    if (typeof draft.provider !== "string" || !isPrimaryProvider(draft.provider)) {
      throw new Error(`Unsupported provider: ${String(draft.provider)}`);
    }
    provider = draft.provider;
    model = typeof draft.model === "string" && draft.model.trim() ? draft.model.trim() : getDefaultPrimaryModel(provider as never);
    const sameProvider = existing?.provider === provider;
    config = { ...(sameProvider ? existing?.config ?? {} : {}), ...stringRecord(draft.config, "config") };
    secrets = sameProvider ? decrypted(existing?.secrets) : {};
    for (const [key, value] of Object.entries(stringRecord(draft.secrets, "secrets"))) {
      if (value === null) delete secrets[key];
      else if (value) secrets[key] = value;
    }
    for (const field of getPrimaryProviderFields(provider as never)) {
      const value = field.kind === "secret" ? secrets[field.key] : config[field.key];
      if (field.required && !value) throw new Error(`${field.label} is required.`);
    }
    // Rejects a provider the GraphJin agent cannot use.
    graphjinAgentModelFromRow({ provider, model, config, secrets });
  }
  const values = {
    provider,
    model,
    enabled: draft.enabled,
    config,
    secrets: Object.fromEntries(Object.entries(secrets).map(([key, value]) => [key, maybeEncryptSecret(value)])),
    updated_at: new Date(),
  };
  if (existing) {
    await db()
      .update(llm_provider_config)
      .set(values)
      .where(and(eq(llm_provider_config.org_id, orgId), eq(llm_provider_config.scope, GRAPHJIN_AGENT_SCOPE)));
  } else {
    await db().insert(llm_provider_config).values({ org_id: orgId, scope: GRAPHJIN_AGENT_SCOPE, ...values });
  }
  await provisionGraphjinAgent(orgId);
  return getGraphjinAgentSettings(orgId);
}
