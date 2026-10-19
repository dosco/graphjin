import "server-only";

import { and, db, eq, llm_provider_config } from "@neko/db";
import {
  PRIMARY_PROVIDER_OPTIONS,
  RESEARCH_PROVIDER_OPTIONS,
  type EditableProviderConfig,
  type PrimaryProviderId,
  type ProviderScope,
  type ResearchProviderId,
  type SecretMaskMap,
  type SettingsField,
  type StoredProviderConfigRow,
  getDefaultPrimaryModel,
  getDefaultResearchModel,
  getPrimaryProviderFields,
  getResearchProviderFields,
  isPrimaryProvider,
  isResearchProvider,
  maskSecret,
  readPrimaryProviderConfigFromEnv,
  readResearchProviderConfigFromEnv,
} from "@neko/llm/config";
import {
  maybeDecryptSecret,
  maybeEncryptSecret,
} from "@neko/llm/secrets";
import { resolveProviderRuntime } from "@neko/llm/provider-runtime";

export type PublicProviderConfig =
  | {
      scope: "primary";
      source: "org" | "env" | "default";
      provider: PrimaryProviderId;
      model: string;
      label: string | null;
      enabled: boolean;
      config: Record<string, unknown>;
      secretStatus: SecretMaskMap;
    }
  | {
      scope: "research";
      source: "org" | "env" | "default";
      provider: ResearchProviderId;
      model: string;
      label: string | null;
      enabled: boolean;
      config: Record<string, unknown>;
      secretStatus: SecretMaskMap;
    };

export type ProviderSettingsPayload = {
  primary: PublicProviderConfig;
  research: PublicProviderConfig;
  options: {
    primary: typeof PRIMARY_PROVIDER_OPTIONS;
    research: typeof RESEARCH_PROVIDER_OPTIONS;
  };
  defaults: {
    primary: Record<PrimaryProviderId, string>;
    research: Record<ResearchProviderId, string>;
  };
  fields: {
    primary: Record<PrimaryProviderId, SettingsField[]>;
    research: Record<ResearchProviderId, SettingsField[]>;
  };
};

type SaveDraftInput = {
  scope: ProviderScope;
  provider: string;
  model?: string;
  label?: string | null;
  enabled?: boolean;
  config?: Record<string, unknown>;
  secrets?: Record<string, string | null>;
};

type StoredWithSecrets = EditableProviderConfig & {
  id?: string;
  source: "org" | "env" | "default";
};

function primaryFieldCatalog(): Record<PrimaryProviderId, SettingsField[]> {
  return Object.fromEntries(
    PRIMARY_PROVIDER_OPTIONS.map((option) => [
      option.value,
      getPrimaryProviderFields(option.value),
    ]),
  ) as Record<PrimaryProviderId, SettingsField[]>;
}

function researchFieldCatalog(): Record<ResearchProviderId, SettingsField[]> {
  return Object.fromEntries(
    RESEARCH_PROVIDER_OPTIONS.map((option) => [
      option.value,
      getResearchProviderFields(option.value),
    ]),
  ) as Record<ResearchProviderId, SettingsField[]>;
}

async function loadStoredConfig(
  orgId: string,
  scope: ProviderScope,
): Promise<StoredProviderConfigRow | null> {
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
        eq(llm_provider_config.scope, scope),
      ),
    )
    .limit(1);
  return (rows[0] as StoredProviderConfigRow | undefined) ?? null;
}

function readSecrets(row: StoredProviderConfigRow | null): Record<string, string> {
  const secrets = row?.secrets ?? {};
  const decoded: Record<string, string> = {};

  for (const [key, value] of Object.entries(secrets)) {
    const decrypted = maybeDecryptSecret(value);
    if (decrypted) decoded[key] = decrypted;
  }

  return decoded;
}

function publicFromConfig(config: StoredWithSecrets): PublicProviderConfig {
  const secretStatus = Object.fromEntries(
    Object.entries(config.secrets).map(([key, value]) => [key, maskSecret(value)]),
  );

  return {
    scope: config.scope,
    source: config.source,
    provider: config.provider as never,
    model: config.model,
    label: config.label ?? null,
    enabled: config.enabled,
    config: config.config,
    secretStatus,
  } as PublicProviderConfig;
}

function defaultPrimaryConfig(): StoredWithSecrets {
  return {
    scope: "primary",
    source: "default",
    provider: "google-gemini",
    model: getDefaultPrimaryModel("google-gemini"),
    label: null,
    enabled: true,
    config: {},
    secrets: {},
  };
}

function defaultResearchConfig(): StoredWithSecrets {
  return {
    scope: "research",
    source: "default",
    provider: "perplexity",
    model: getDefaultResearchModel("perplexity"),
    label: null,
    enabled: true,
    config: {},
    secrets: {},
  };
}

function validateConfig(config: EditableProviderConfig): string[] {
  const researchOff =
    config.scope === "research" && (!config.enabled || config.provider === "disabled");

  if (researchOff) return [];

  if (!config.model) {
    return ["Model is required."];
  }

  const fields =
    config.scope === "primary"
      ? getPrimaryProviderFields(config.provider)
      : getResearchProviderFields(config.provider);

  const errors: string[] = [];

  for (const field of fields) {
    const value =
      field.kind === "secret"
        ? config.secrets[field.key]
        : typeof config.config[field.key] === "string"
          ? String(config.config[field.key]).trim()
          : "";

    if (field.required && !value) {
      errors.push(`${field.label} is required.`);
    }
  }

  if (errors.length === 0 && config.scope === "primary") {
    try {
      resolveProviderRuntime({
        provider: config.provider,
        model: config.model,
        config: config.config,
      });
    } catch (error) {
      errors.push(error instanceof Error ? error.message : "Invalid provider configuration.");
    }
  }

  return errors;
}

function toEditable(row: StoredProviderConfigRow): StoredWithSecrets {
  return {
    id: row.id,
    source: "org",
    scope: row.scope,
    provider: row.provider as never,
    model: row.model ?? "",
    label: row.label ?? null,
    enabled: row.enabled,
    config: (row.config as Record<string, unknown>) ?? {},
    secrets: readSecrets(row),
  } as StoredWithSecrets;
}

async function resolvePrimary(orgId: string): Promise<StoredWithSecrets> {
  const row = await loadStoredConfig(orgId, "primary");
  if (row && isPrimaryProvider(row.provider)) {
    return toEditable(row);
  }

  const env = readPrimaryProviderConfigFromEnv();
  if (env && env.scope === "primary") {
    return { ...env, source: "env" };
  }

  return defaultPrimaryConfig();
}

async function resolveResearch(orgId: string): Promise<StoredWithSecrets> {
  const row = await loadStoredConfig(orgId, "research");
  if (row && isResearchProvider(row.provider)) {
    return toEditable(row);
  }

  const env = readResearchProviderConfigFromEnv();
  if (env && env.scope === "research") {
    return { ...env, source: "env" };
  }

  return defaultResearchConfig();
}

export async function getProviderSettingsPayload(
  orgId: string,
): Promise<ProviderSettingsPayload> {
  const [primary, research] = await Promise.all([
    resolvePrimary(orgId),
    resolveResearch(orgId),
  ]);

  return {
    primary: publicFromConfig(primary),
    research: publicFromConfig(research),
    options: {
      primary: PRIMARY_PROVIDER_OPTIONS,
      research: RESEARCH_PROVIDER_OPTIONS,
    },
    defaults: {
      primary: Object.fromEntries(
        PRIMARY_PROVIDER_OPTIONS.map((option) => [
          option.value,
          getDefaultPrimaryModel(option.value),
        ]),
      ) as Record<PrimaryProviderId, string>,
      research: Object.fromEntries(
        RESEARCH_PROVIDER_OPTIONS.map((option) => [
          option.value,
          getDefaultResearchModel(option.value),
        ]),
      ) as Record<ResearchProviderId, string>,
    },
    fields: {
      primary: primaryFieldCatalog(),
      research: researchFieldCatalog(),
    },
  };
}

function mergeDraft(
  existing: StoredWithSecrets,
  draft: SaveDraftInput,
): EditableProviderConfig {
  if (draft.scope === "primary") {
    if (!isPrimaryProvider(draft.provider)) {
      throw new Error(`Unsupported primary provider: ${draft.provider}`);
    }

    const provider = draft.provider;
    const providerChanged = existing.scope !== "primary" || existing.provider !== provider;
    const mergedSecrets = providerChanged ? {} : { ...existing.secrets };
    for (const [key, value] of Object.entries(draft.secrets ?? {})) {
      if (value === null) delete mergedSecrets[key];
      else if (value.trim()) mergedSecrets[key] = value.trim();
    }

    return {
      scope: "primary",
      provider,
      model:
        draft.model?.trim() ||
        (providerChanged
          ? getDefaultPrimaryModel(provider)
          : existing.model || getDefaultPrimaryModel(provider)),
      label: draft.label ?? existing.label ?? null,
      enabled: draft.enabled ?? existing.enabled,
      config: providerChanged ? { ...(draft.config ?? {}) } : { ...existing.config, ...(draft.config ?? {}) },
      secrets: mergedSecrets,
    };
  }

  if (!isResearchProvider(draft.provider)) {
    throw new Error(`Unsupported research provider: ${draft.provider}`);
  }

  const provider = draft.provider;
  const providerChanged = existing.scope !== "research" || existing.provider !== provider;
  const mergedSecrets = providerChanged ? {} : { ...existing.secrets };
  for (const [key, value] of Object.entries(draft.secrets ?? {})) {
    if (value === null) delete mergedSecrets[key];
    else if (value.trim()) mergedSecrets[key] = value.trim();
  }

  return {
    scope: "research",
    provider,
    model:
      draft.model?.trim() ||
      (providerChanged
        ? getDefaultResearchModel(provider)
        : existing.model || getDefaultResearchModel(provider)),
    label: draft.label ?? existing.label ?? null,
    enabled: draft.enabled ?? existing.enabled,
    config: providerChanged ? { ...(draft.config ?? {}) } : { ...existing.config, ...(draft.config ?? {}) },
    secrets: mergedSecrets,
  };
}

async function upsertProviderConfig(
  orgId: string,
  existingId: string | undefined,
  config: EditableProviderConfig,
): Promise<void> {
  const encryptedSecrets = Object.fromEntries(
    Object.entries(config.secrets).map(([key, value]) => [key, maybeEncryptSecret(value)]),
  );

  if (existingId) {
    await db()
      .update(llm_provider_config)
      .set({
        scope: config.scope,
        provider: config.provider,
        model: config.model || null,
        label: config.label ?? null,
        enabled: config.enabled,
        config: config.config,
        secrets: encryptedSecrets,
        updated_at: new Date(),
      })
      .where(eq(llm_provider_config.id, existingId));
    return;
  }

  await db().insert(llm_provider_config).values({
    org_id: orgId,
    scope: config.scope,
    provider: config.provider,
    model: config.model || null,
    label: config.label ?? null,
    enabled: config.enabled,
    config: config.config,
    secrets: encryptedSecrets,
  });
}

export async function saveProviderDraft(
  orgId: string,
  draft: SaveDraftInput,
): Promise<PublicProviderConfig> {
  const existingRow = await loadStoredConfig(orgId, draft.scope);
  const existing =
    existingRow && existingRow.scope === draft.scope
      ? toEditable(existingRow)
      : draft.scope === "primary"
        ? defaultPrimaryConfig()
        : defaultResearchConfig();

  const merged = mergeDraft(existing, draft);

  const errors = validateConfig(merged);
  if (errors.length > 0) {
    throw new Error(errors.join(" "));
  }

  await upsertProviderConfig(orgId, existing.id, merged);
  return publicFromConfig({ ...merged, source: "org" });
}

export async function resolveResearchStatus(orgId: string): Promise<"enabled" | "disabled"> {
  const resolved = await resolveResearch(orgId);
  if (!resolved.enabled || resolved.provider === "disabled") return "disabled";
  return "enabled";
}

export async function hasPrimaryProviderSetup(orgId: string): Promise<boolean> {
  const stored = await loadStoredConfig(orgId, "primary");
  if (stored && isPrimaryProvider(stored.provider)) {
    const resolved = toEditable(stored);
    return resolved.enabled && validateConfig(resolved).length === 0;
  }

  const env = readPrimaryProviderConfigFromEnv();
  if (env && env.scope === "primary") {
    return env.enabled && validateConfig(env).length === 0;
  }

  return false;
}
