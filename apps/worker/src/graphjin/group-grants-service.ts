import { randomUUID } from "node:crypto";
import { readFile, rename, stat, writeFile } from "node:fs/promises";
import {
  and,
  data_source,
  db,
  desc,
  eq,
  getGroupGrantsEnabled,
  getPreviousReadModes,
  recordPreviousReadModes,
  loadGroupGrantInputs,
  seedEveryoneDataAccess,
  setGroupGrantsEnabled,
} from "@neko/db";
import {
  acquireGraphjinConfigLock,
  applyGraphjinConfigUpdate,
  applyGroupGrantsToConfig,
  buildGroupGrantsModel,
  graphjinQuery,
  listConfigApiOperations,
  mintGraphjinToken,
  groupGrantsLivePatch,
  readDatabaseSourceReadModes,
  removeGroupGrantsFromConfig,
} from "@neko/llm/graphjin";

export type GroupGrantsApplyResult = { enabled: boolean; changed: boolean; roles: string[] };

export type ColumnCatalog = Map<string, Array<{ schema: string; table: string; columns: string[] }>>;

export type GroupGrantsDeps = {
  configFile: string | null;
  /** Applies a gj_config update to the running GraphJin, with no restart. */
  applyLive: (update: Record<string, unknown>) => Promise<void>;
  /** Tables and columns GraphJin exposes for each database source. */
  catalog?: (sources: string[]) => Promise<ColumnCatalog>;
};

async function defaultEndpoint(orgId: string): Promise<string | null> {
  const [source] = await db()
    .select({ graphqlUrl: data_source.graphql_url })
    .from(data_source)
    .where(and(eq(data_source.org_id, orgId), eq(data_source.enabled, true)))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);
  if (!source?.graphqlUrl) return null;
  const clean = source.graphqlUrl.replace(/\/+$/, "");
  return clean.endsWith("/api/v1/graphql") ? clean : `${clean}/api/v1/graphql`;
}

type CatalogColumnRow = { database_name: string | null; schema_name: string | null; table_name: string | null; column_name: string | null };

/** Reads column rows from gj_catalog with an admin token, one page at a time. */
export async function fetchGraphjinColumnCatalog(orgId: string, endpoint: string, sources: string[]): Promise<ColumnCatalog> {
  const wanted = new Set(sources);
  const tables = new Map<string, Map<string, { schema: string; table: string; columns: string[] }>>();
  const token = mintGraphjinToken({ orgId, userId: null, role: "admin", ttlSeconds: 120 });
  const pageSize = 500;
  for (let offset = 0; offset < 200_000; offset += pageSize) {
    const result = await graphjinQuery<{ gj_catalog?: CatalogColumnRow[] }>({
      baseUrl: endpoint,
      headers: { authorization: `Bearer ${token}` },
      query: `query GroupGrantsCatalog { gj_catalog(where: { kind: { eq: "column" } }, limit: ${pageSize}, offset: ${offset}, order_by: { id: asc }) { database_name schema_name table_name column_name } }`,
      signal: AbortSignal.timeout(60_000),
    });
    if (result.errors?.length) throw new Error(`GraphJin catalog read failed: ${result.errors.map((e) => e.message).join("; ")}`);
    const page = result.data?.gj_catalog ?? [];
    for (const row of page) {
      if (!row.database_name || !row.table_name || !row.column_name || !wanted.has(row.database_name)) continue;
      const bySource = tables.get(row.database_name) ?? new Map();
      const key = `${row.schema_name ?? ""}.${row.table_name}`;
      const entry = bySource.get(key) ?? { schema: row.schema_name ?? "", table: row.table_name, columns: [] };
      if (!entry.columns.includes(row.column_name)) entry.columns.push(row.column_name);
      bySource.set(key, entry);
      tables.set(row.database_name, bySource);
    }
    if (page.length < pageSize) break;
  }
  return new Map([...tables].map(([source, byTable]) => [source, [...byTable.values()]]));
}

export function defaultGroupGrantsDeps(orgId: string): GroupGrantsDeps {
  return {
    configFile: process.env.OPENNEKO_GRAPHJIN_CONFIG?.trim() || null,
    applyLive: async (update) => {
      const endpoint = await defaultEndpoint(orgId);
      if (!endpoint) return;
      await applyGraphjinConfigUpdate({
        endpoint,
        headers: { authorization: `Bearer ${mintGraphjinToken({ orgId, userId: "group-grants", role: "admin", ttlSeconds: 120 })}` },
        update,
      });
    },
    catalog: async (sources) => {
      const endpoint = await defaultEndpoint(orgId);
      if (!endpoint) throw new Error("no enabled GraphJin data source");
      return fetchGraphjinColumnCatalog(orgId, endpoint, sources);
    },
  };
}

/**
 * Turns group grants on: records each source's read mode, gives Everyone
 * today's member access to every existing table, then applies the grants.
 */
export async function enableGroupGrants(
  orgId: string,
  actorUserId: string | null,
  deps: GroupGrantsDeps = defaultGroupGrantsDeps(orgId),
): Promise<GroupGrantsApplyResult & { seededRules: number }> {
  if (!deps.configFile) throw new Error("GraphJin configuration is unavailable");
  if (await getGroupGrantsEnabled(orgId)) return { ...(await applyGroupGrants(orgId, deps)), seededRules: 0 };
  const modes = readDatabaseSourceReadModes(await readFile(deps.configFile, "utf8"));
  const sources = Object.keys(modes);
  const catalog = sources.length && deps.catalog ? await deps.catalog(sources) : new Map();
  const seededRules = await seedEveryoneDataAccess(orgId, catalog);
  await setGroupGrantsEnabled(orgId, true, actorUserId, modes);
  return { ...(await applyGroupGrants(orgId, deps)), seededRules };
}

/** Turns group grants off and restores GraphJin's previous read policy. */
export async function disableGroupGrants(
  orgId: string,
  actorUserId: string | null,
  deps: GroupGrantsDeps = defaultGroupGrantsDeps(orgId),
): Promise<{ changed: boolean }> {
  if (!deps.configFile) throw new Error("GraphJin configuration is unavailable");
  const configFile = deps.configFile;
  await setGroupGrantsEnabled(orgId, false, actorUserId);
  const release = await acquireGraphjinConfigLock({ configFile });
  let patch: Record<string, unknown> | null = null;
  try {
    const raw = await readFile(configFile, "utf8");
    const restored = removeGroupGrantsFromConfig(raw, await getPreviousReadModes(orgId));
    if (restored.changed) {
      patch = groupGrantsLivePatch(raw, restored.content);
      await writeConfig(configFile, restored.content);
    }
  } finally {
    await release();
  }
  if (patch) await deps.applyLive(patch);
  return { changed: patch !== null };
}

async function writeConfig(configFile: string, content: string): Promise<void> {
  const mode = (await stat(configFile)).mode & 0o777;
  const temporary = `${configFile}.${randomUUID()}.group-grants`;
  await writeFile(temporary, content, { mode });
  await rename(temporary, configFile);
}

/**
 * Writes the org's group roles and grants into the GraphJin config file, for
 * GraphJin's next start, and applies the same change to the running GraphJin
 * through gj_config. Does nothing while group grants are off.
 */
export async function applyGroupGrants(orgId: string, deps: GroupGrantsDeps = defaultGroupGrantsDeps(orgId)): Promise<GroupGrantsApplyResult> {
  if (!(await getGroupGrantsEnabled(orgId))) return { enabled: false, changed: false, roles: [] };
  if (!deps.configFile) throw new Error("GraphJin configuration is unavailable");
  const configFile = deps.configFile;
  const release = await acquireGraphjinConfigLock({ configFile });
  let result: GroupGrantsApplyResult;
  let patch: Record<string, unknown> | null = null;
  try {
    const raw = await readFile(configFile, "utf8");
    const inputs = await loadGroupGrantInputs(orgId, listConfigApiOperations(raw));
    const model = buildGroupGrantsModel(inputs);
    const stored = await getPreviousReadModes(orgId);
    const unseen = Object.fromEntries(
      Object.entries(readDatabaseSourceReadModes(raw)).filter(([source, mode]) => !(source in stored) && mode !== "admin"),
    );
    if (Object.keys(unseen).length > 0) await recordPreviousReadModes(orgId, unseen);
    const patched = applyGroupGrantsToConfig(raw, model, { ...unseen, ...stored });
    if (patched.changed) {
      patch = groupGrantsLivePatch(raw, patched.content);
      await writeConfig(configFile, patched.content);
    }
    result = { enabled: true, changed: patched.changed, roles: model.roles.map((r) => r.role) };
  } finally {
    await release();
  }
  if (patch) await deps.applyLive(patch);
  return result;
}

const timers = new Map<string, NodeJS.Timeout>();

/** Batches changes: one apply runs 30 seconds after the last change. */
export function scheduleGroupGrantsApply(orgId: string, delayMs = 30_000, run: (orgId: string) => Promise<unknown> = applyGroupGrants): void {
  const existing = timers.get(orgId);
  if (existing) clearTimeout(existing);
  timers.set(
    orgId,
    setTimeout(() => {
      timers.delete(orgId);
      run(orgId).catch((error) =>
        console.warn(`[group-grants] apply failed for ${orgId}: ${error instanceof Error ? error.message : error}`),
      );
    }, delayMs),
  );
}
