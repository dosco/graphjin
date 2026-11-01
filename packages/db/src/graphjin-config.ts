/**
 * Runtime switches for GraphJin administration. Persisted in
 * llm_provider_config with scope="graphjin-config" so the worker and web
 * process read the same operator-controlled state.
 */

import { and, db, eq } from "./index";
import { llm_provider_config } from "./schema";

export const GRAPHJIN_CONFIG_SCOPE = "graphjin-config";

export type GraphjinConfigSettings = {
  /**
   * Allows admin Ask sessions to mount the GraphJin source-config MCP server
   * and allows approved source_config_admin requests to execute.
   */
  sourceConfigEnabled: boolean;
};

export const DEFAULT_GRAPHJIN_CONFIG_SETTINGS: GraphjinConfigSettings = {
  sourceConfigEnabled: false,
};

function readBool(value: unknown, fallback: boolean): boolean {
  return typeof value === "boolean" ? value : fallback;
}

export function graphjinConfigSettingsFromConfig(
  config: Record<string, unknown> | null | undefined,
  rowEnabled = false,
): GraphjinConfigSettings {
  if (!config) {
    return { sourceConfigEnabled: rowEnabled };
  }
  return {
    sourceConfigEnabled:
      rowEnabled && readBool(config.sourceConfigEnabled, true),
  };
}

export async function getGraphjinConfigSettingsForOrg(
  orgId: string,
): Promise<GraphjinConfigSettings> {
  const rows = await db()
    .select({
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
  const row = rows[0] as
    | { enabled: boolean; config: Record<string, unknown> | null }
    | undefined;
  if (!row) return { ...DEFAULT_GRAPHJIN_CONFIG_SETTINGS };
  return graphjinConfigSettingsFromConfig(row.config, row.enabled);
}
