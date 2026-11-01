import { access } from "node:fs/promises";
import { graphjinQuery, mintGraphjinToken } from "@neko/llm/graphjin";

/**
 * Where a pack's GraphJin changes go.
 *
 * - `api`: the GraphJin keeps its own configuration. The installer sends
 *   sources, OpenAPI documents and saved queries through gj_config, and
 *   GraphJin writes them to its config folder. This works for any GraphJin,
 *   including one the customer runs outside OpenNeko.
 * - `files`: the GraphJin runs in production mode, so it does not keep
 *   changes made through its API. OpenNeko writes the config folder that it
 *   shares with its own GraphJin.
 */
export type GraphjinTarget =
  // keystoreConfigured is undefined when the GraphJin does not report it.
  | { mode: "api"; endpoint: string; orgId: string; anonymous: boolean; keystoreConfigured?: boolean }
  | { mode: "files"; endpoint: string; orgId: string; configFile: string };

export const GRAPHJIN_NO_KEYSTORE_MESSAGE =
  "The connected GraphJin has no secrets keystore key, so it cannot store this pack's credentials. Set secrets.keystore.key in that GraphJin's config, for example with GJ_SECRETS_KEYSTORE_KEY, restart GraphJin, then install the pack again.";

export function graphjinAdminHeaders(orgId: string, userId = "pack-installer"): Record<string, string> {
  return {
    authorization: `Bearer ${mintGraphjinToken({ orgId, userId, role: "admin", ttlSeconds: 120 })}`,
  };
}

export async function resolveGraphjinTarget(input: {
  endpoint: string;
  orgId: string;
  configFile?: string;
}): Promise<GraphjinTarget> {
  // GraphJin reports serv.auth with its Go field names ({"Type":"jwt"}).
  type Serv = { production?: boolean; auth?: { type?: string; Type?: string }; secrets_keystore_configured?: boolean };
  const result = await graphjinQuery<{ gj_config?: { serv?: Serv | string } | Array<{ serv?: Serv | string }> }>({
    baseUrl: input.endpoint,
    configurationOnly: true,
    headers: graphjinAdminHeaders(input.orgId),
    query: 'query PackGraphjinTarget { gj_config(id: "current") { serv } }',
    signal: AbortSignal.timeout(30_000),
  });
  const row = Array.isArray(result.data?.gj_config) ? result.data?.gj_config[0] : result.data?.gj_config;
  if (result.errors?.length || !row) {
    throw new Error(
      `OpenNeko cannot read the GraphJin configuration at ${input.endpoint}: ${result.errors?.map((error) => error.message).join("; ") ?? "gj_config is unavailable"}. Allow config.read and gj_config access for OpenNeko on that GraphJin.`,
    );
  }
  const serv: Serv = typeof row.serv === "string" ? JSON.parse(row.serv) as Serv : row.serv ?? {};
  if (serv.production !== true) {
    // A GraphJin without auth sees OpenNeko as anonymous whatever token it sends.
    const authType = String(serv.auth?.type ?? serv.auth?.Type ?? "").trim().toLowerCase();
    const keystoreConfigured = typeof serv.secrets_keystore_configured === "boolean" ? serv.secrets_keystore_configured : undefined;
    return { mode: "api", endpoint: input.endpoint, orgId: input.orgId, anonymous: authType === "" || authType === "none", keystoreConfigured };
  }
  const configFile = input.configFile?.trim();
  if (configFile && (await access(configFile).then(() => true, () => false))) {
    return { mode: "files", endpoint: input.endpoint, orgId: input.orgId, configFile };
  }
  throw new Error(
    `The GraphJin at ${input.endpoint} runs in production mode, so it does not keep configuration changes made through its API. Set production: false on that GraphJin, or connect OpenNeko to the GraphJin whose config folder it manages.`,
  );
}

export type LiveGraphjinConfig = {
  catalogRevision: string | null;
  sources: Record<string, unknown>[];
  tables: Record<string, unknown>[];
  relationships: Record<string, unknown>[];
};

function rows(value: unknown): Record<string, unknown>[] {
  if (typeof value === "string") {
    try {
      return rows(JSON.parse(value));
    } catch {
      return [];
    }
  }
  return Array.isArray(value)
    ? value.filter((item): item is Record<string, unknown> => Boolean(item) && typeof item === "object" && !Array.isArray(item))
    : [];
}

/** The running configuration, with secrets redacted by GraphJin. */
export async function readLiveGraphjinConfig(target: { endpoint: string; orgId: string }): Promise<LiveGraphjinConfig> {
  const result = await graphjinQuery<{ gj_config?: Record<string, unknown> | Array<Record<string, unknown>> }>({
    baseUrl: target.endpoint,
    configurationOnly: true,
    headers: graphjinAdminHeaders(target.orgId),
    query: `query PackLiveConfig_${Date.now()} { gj_config(id: "current") { catalog_revision sources tables relationships } }`,
    signal: AbortSignal.timeout(60_000),
  });
  const row = Array.isArray(result.data?.gj_config) ? result.data?.gj_config[0] : result.data?.gj_config;
  if (result.errors?.length || !row) {
    throw new Error(`GraphJin configuration is unavailable: ${result.errors?.map((error) => error.message).join("; ") ?? "no gj_config row"}`);
  }
  return {
    catalogRevision: typeof row.catalog_revision === "string" ? row.catalog_revision : null,
    sources: rows(row.sources),
    tables: rows(row.tables),
    relationships: rows(row.relationships),
  };
}

/** The saved query text that the GraphJin serves under this name, or null. */
export async function readLiveSavedQuery(target: { endpoint: string; orgId: string }, name: string): Promise<string | null> {
  const result = await graphjinQuery<{ gj_catalog?: Record<string, unknown> | Array<Record<string, unknown>> }>({
    baseUrl: target.endpoint,
    configurationOnly: true,
    headers: graphjinAdminHeaders(target.orgId),
    query: `query PackSavedQuery_${Date.now()}($id: String!) { gj_catalog(id: $id) { id graphql_query } }`,
    variables: { id: `saved_query:${name}` },
    signal: AbortSignal.timeout(30_000),
  });
  const row = Array.isArray(result.data?.gj_catalog) ? result.data?.gj_catalog[0] : result.data?.gj_catalog;
  return typeof row?.graphql_query === "string" && row.graphql_query.trim() ? row.graphql_query : null;
}

export const apiTargetPrefix = "graphjin:";
