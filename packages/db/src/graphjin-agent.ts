/**
 * Org-wide choice of how agent runs reach data. Persisted in
 * llm_provider_config with scope="graphjin-agent". With no row, runs write
 * their own queries with GraphJin's tools. An admin saves enabled=true to opt
 * into GraphJin's server-side agent, which reuses the primary provider unless
 * the row names another one.
 */

import { and, db, eq } from "./index";
import { llm_provider_config } from "./schema";

export const GRAPHJIN_AGENT_SCOPE = "graphjin-agent";
/** Provider value that reuses the primary provider and model. */
export const GRAPHJIN_AGENT_REUSE_PRIMARY = "primary";

export type GraphjinAgentRow = {
  enabled: boolean;
  provider: string;
  model: string | null;
  config: Record<string, unknown> | null;
  secrets: Record<string, unknown> | null;
};

export async function getGraphjinAgentRow(orgId: string): Promise<GraphjinAgentRow | null> {
  const rows = await db()
    .select({
      enabled: llm_provider_config.enabled,
      provider: llm_provider_config.provider,
      model: llm_provider_config.model,
      config: llm_provider_config.config,
      secrets: llm_provider_config.secrets,
    })
    .from(llm_provider_config)
    .where(and(eq(llm_provider_config.org_id, orgId), eq(llm_provider_config.scope, GRAPHJIN_AGENT_SCOPE)))
    .limit(1);
  return (rows[0] as GraphjinAgentRow | undefined) ?? null;
}

/**
 * True when every agent run gets data only through GraphJin's agent, which an
 * admin must opt into.
 */
export async function graphjinAgentEnabledForOrg(orgId: string): Promise<boolean> {
  return graphjinAgentEnabled(await getGraphjinAgentRow(orgId));
}

/** The agent path is off unless a saved row turns it on. */
export function graphjinAgentEnabled(row: Pick<GraphjinAgentRow, "enabled"> | null): boolean {
  return row?.enabled === true;
}
