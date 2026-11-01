// Applies the admin "Semantic search" switch to GraphJin: semantic catalog
// search and meaning-based Library file search. The config file gets the
// catalog_search.semantic block for GraphJin's next start, and the running
// GraphJin gets `enabled` through gj_config, live, with no restart.

import { readFile, rename, rm, writeFile } from "node:fs/promises";
import { data_source, db, desc, eq, readSemanticSearchEnabled } from "@neko/db";
import { applyGraphjinConfigUpdate } from "./config-apply";
import { acquireGraphjinConfigLock } from "./persist-source-config";
import { patchGraphjinSemanticSearch } from "./sources-config";
import { mintGraphjinToken } from "./token";

/** The gj_config update that turns GraphJin's semantic search on or off. */
export function graphjinSemanticSearchUpdate(enabled: boolean): Record<string, unknown> {
  return { serv: { catalog_search: { semantic: { enabled } } } };
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

async function writeConfig(path: string, content: string): Promise<void> {
  const temporary = `${path}.${process.pid}.${Date.now()}.tmp`;
  await writeFile(temporary, content, { mode: 0o664 });
  try {
    await rename(temporary, path);
  } catch (error) {
    await rm(temporary, { force: true });
    throw error;
  }
}

export type GraphjinSemanticSearchResult =
  | { status: "applied" }
  | { status: "skipped"; reason: string };

/**
 * Brings GraphJin in line with the switch (read from the database unless
 * given). The file is patched under the config lock; the live update is sent
 * even when the file was already current, so a GraphJin that missed an earlier
 * update catches up. Throws when GraphJin rejects the update.
 */
export async function provisionGraphjinSemanticSearch(
  orgId: string,
  options: {
    enabled?: boolean;
    configFile?: string;
    apply?: (endpoint: string, update: Record<string, unknown>) => Promise<unknown>;
  } = {},
): Promise<GraphjinSemanticSearchResult> {
  const enabled = options.enabled ?? (await readSemanticSearchEnabled());
  const configFile = options.configFile ?? process.env.OPENNEKO_GRAPHJIN_CONFIG?.trim();
  if (!configFile) return { status: "skipped", reason: "No GraphJin config file is set for this deployment." };
  const release = await acquireGraphjinConfigLock({ configFile });
  try {
    const patch = patchGraphjinSemanticSearch(await readFile(configFile, "utf8"), enabled);
    if (patch.changed) await writeConfig(configFile, patch.content);
  } finally {
    await release();
  }
  const endpoint = await graphjinEndpoint(orgId);
  if (!endpoint) return { status: "skipped", reason: "No GraphJin data source is connected." };
  const apply =
    options.apply ??
    ((url: string, update: Record<string, unknown>) =>
      applyGraphjinConfigUpdate({
        endpoint: url,
        headers: {
          authorization: `Bearer ${mintGraphjinToken({ orgId, userId: "semantic-search-settings", role: "admin", ttlSeconds: 120 })}`,
        },
        update,
      }));
  await apply(endpoint, graphjinSemanticSearchUpdate(enabled));
  return { status: "applied" };
}
