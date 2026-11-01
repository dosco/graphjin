import { readFile } from "node:fs/promises";
import { parse } from "yaml";
import { graphjinConfigPatchHash } from "./config-change";
import { pool } from "@neko/db";

export type PackSourceSelection = { id: string; graphqlUrl: string; authMode: string };

/** A changed, disabled or cross-org endpoint must never silently replace an approved target. */
export async function resolvePackSource(orgId: string, selected: PackSourceSelection): Promise<PackSourceSelection> {
  const result = await pool().query<{ id: string; graphqlUrl: string; authMode: string }>(
    'select id, graphql_url as "graphqlUrl", auth_mode as "authMode" from data_source where org_id=$1 and id=$2 and enabled',
    [orgId, selected.id],
  );
  const source = result.rows[0];
  if (!source || source.graphqlUrl !== selected.graphqlUrl || source.authMode !== selected.authMode) {
    throw new Error("Pack data source changed or is unavailable; reconfigure and revalidate the pack");
  }
  return source;
}

/** Legacy/non-pack artifacts retain their existing default-source behavior. */
export async function packArtifactSource(orgId: string, kind: "metric" | "watcher", nativeId: string): Promise<PackSourceSelection | null> {
  const locator = kind === "metric"
    ? "a.metadata->'locator'->>'role'=n.role and a.metadata->'locator'->>'slug'=n.slug"
    : "a.metadata->'locator'->>'name'=n.name";
  const result = await pool().query<{ selection: PackSourceSelection | null; bindings: Record<string, string>; hashes: Record<string, string>; tables: Array<Record<string, string>>; status: string; enabled: boolean }>(
    `select i.config->'_runtime'->'source' as selection, i.config->'_runtime'->'bindings' as bindings, i.config->'_runtime'->'bindingHashes' as hashes, i.config->'_runtime'->'tables' as tables, i.status, n.${kind === "metric" ? "active" : "enabled"} as enabled
     from pack_artifact a join pack_install i on i.id=a.pack_install_id and i.org_id=a.org_id
     join ${kind === "metric" ? "metric" : "watcher"} n on n.org_id=a.org_id and ${locator}
     where a.org_id=$1 and a.artifact_kind=$2 and n.id=$3`, [orgId, kind, nativeId],
  );
  const installed = result.rows[0];
  if (!installed?.selection) return null;
  if (installed.status !== "installed" || !installed.enabled) throw new Error("Pack is not active");
  if (Object.keys(installed.bindings ?? {}).length) {
    const file = process.env.OPENNEKO_GRAPHJIN_CONFIG;
    if (!file) throw new Error("Pack source bindings cannot be verified");
    const config = parse(await readFile(file, "utf8")) as { sources?: Array<Record<string, unknown>> };
    for (const [key, name] of Object.entries(installed.bindings)) {
      const source = config.sources?.find(value => value.name === name);
      if (!source || graphjinConfigPatchHash(source) !== installed.hashes?.[key]) throw new Error("Pack source binding changed; reconfigure and revalidate the pack");
    }
  }
  await verifyPackQueryTables(installed.tables ?? []);
  return resolvePackSource(orgId, installed.selection);
}

/** Generated table aliases must still point at the reviewed physical database. */
export async function verifyPackQueryTables(tables: Array<Record<string, string>> = []): Promise<void> {
  if (!tables.length) return;
  const file = process.env.OPENNEKO_GRAPHJIN_CONFIG;
  if (!file) throw new Error("Pack query routing cannot be verified");
  const config = parse(await readFile(file, "utf8")) as { tables?: Array<Record<string, unknown>> };
  for (const expected of tables) {
    const current = config.tables?.find(value => value.name === expected.name);
    if (!current || graphjinConfigPatchHash(current) !== graphjinConfigPatchHash(expected)) throw new Error("Pack query routing changed; restore the reviewed table mapping");
  }
}
