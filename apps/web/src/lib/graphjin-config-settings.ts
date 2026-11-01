import "server-only";

import {
  and,
  db,
  eq,
  GRAPHJIN_CONFIG_SCOPE,
  graphjinConfigSettingsFromConfig,
  llm_provider_config,
  type GraphjinConfigSettings,
} from "@neko/db";

export type GraphjinConfigSettingsPayload = {
  settings: GraphjinConfigSettings;
  source: "org" | "default";
};

const PROVIDER_TAG = "graphjin-config";

async function loadRow(orgId: string): Promise<{
  id: string;
  enabled: boolean;
  config: Record<string, unknown> | null;
} | null> {
  const rows = await db()
    .select({
      id: llm_provider_config.id,
      enabled: llm_provider_config.enabled,
      config: llm_provider_config.config,
    })
    .from(llm_provider_config)
    .where(
      and(
        eq(llm_provider_config.org_id, orgId),
        eq(llm_provider_config.scope, GRAPHJIN_CONFIG_SCOPE),
      ),
    )
    .limit(1);
  return (
    (rows[0] as
      | {
          id: string;
          enabled: boolean;
          config: Record<string, unknown> | null;
        }
      | undefined) ?? null
  );
}

export async function getGraphjinConfigSettingsPayload(
  orgId: string,
): Promise<GraphjinConfigSettingsPayload> {
  const row = await loadRow(orgId);
  return {
    settings: row
      ? graphjinConfigSettingsFromConfig(row.config, row.enabled)
      : { sourceConfigEnabled: false },
    source: row ? "org" : "default",
  };
}

export async function saveGraphjinConfigSettingsDraft(
  orgId: string,
  draft: Partial<GraphjinConfigSettings>,
): Promise<GraphjinConfigSettings> {
  const existing = await loadRow(orgId);
  const current = existing
    ? graphjinConfigSettingsFromConfig(existing.config, existing.enabled)
    : { sourceConfigEnabled: false };
  const merged: GraphjinConfigSettings = {
    sourceConfigEnabled:
      typeof draft.sourceConfigEnabled === "boolean"
        ? draft.sourceConfigEnabled
        : current.sourceConfigEnabled,
  };
  const config = { sourceConfigEnabled: merged.sourceConfigEnabled };

  if (existing) {
    await db()
      .update(llm_provider_config)
      .set({
        enabled: merged.sourceConfigEnabled,
        config,
        updated_at: new Date(),
      })
      .where(eq(llm_provider_config.id, existing.id));
  } else {
    await db().insert(llm_provider_config).values({
      org_id: orgId,
      scope: GRAPHJIN_CONFIG_SCOPE,
      provider: PROVIDER_TAG,
      enabled: merged.sourceConfigEnabled,
      config,
      secrets: {},
    });
  }

  return merged;
}
