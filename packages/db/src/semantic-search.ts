/**
 * The deployment-wide "Semantic search" switch. Off by default: with no row,
 * OpenNeko never calls the embedding service and searches by keyword.
 * Persisted in llm_provider_config with scope="semantic-search" (enabled
 * column), like the other admin settings. OpenNeko is single-tenant, so the
 * newest row of any org is the deployment's value.
 */

import { pool } from "./index";

export const SEMANTIC_SEARCH_SCOPE = "semantic-search";

/** SQL that reads the switch; graphjin-secret-init runs the same query. */
export const SEMANTIC_SEARCH_ENABLED_SQL =
  "select enabled from llm_provider_config where scope = 'semantic-search' order by updated_at desc limit 1";

export async function readSemanticSearchEnabled(): Promise<boolean> {
  const { rows } = await pool().query<{ enabled: boolean }>(SEMANTIC_SEARCH_ENABLED_SQL);
  return rows[0]?.enabled === true;
}

export async function writeSemanticSearchEnabled(orgId: string, enabled: boolean): Promise<void> {
  await pool().query(
    `insert into llm_provider_config (org_id, scope, provider, enabled)
     values ($1, $2, $2, $3)
     on conflict (org_id, scope) do update set enabled = excluded.enabled, updated_at = now()`,
    [orgId, SEMANTIC_SEARCH_SCOPE, enabled],
  );
}
