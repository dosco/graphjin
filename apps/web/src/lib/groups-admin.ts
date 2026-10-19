import "server-only";

import { NextResponse } from "next/server";
import {
  and,
  channel_identity,
  data_source,
  db,
  desc,
  eq,
  GroupError,
  isNull,
  library_concept,
  metric,
  operator_profile,
  pack_action_definition,
  pack_install,
  ne,
  type ItemType,
} from "@neko/db";
import { graphjinQuery, mintGraphjinToken } from "@neko/llm/graphjin";
import { listWatchers, listWorkflows } from "@neko/llm/workflows";
import { getActionDescriptors } from "@/lib/auth";
import { listConnectProviders } from "@/lib/integrations";
import { listWorkSkills } from "@/lib/work-files";

function workerAdminBase(): string {
  return (process.env.WORKER_ADMIN_URL ?? "http://127.0.0.1:4100").replace(/\/+$/, "");
}

export async function requestWorker(path: string, body?: unknown): Promise<{ status: number; body: unknown }> {
  const response = await fetch(`${workerAdminBase()}${path}`, {
    method: body === undefined ? "GET" : "POST",
    headers: body === undefined ? undefined : { "content-type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: "no-store",
    signal: AbortSignal.timeout(120_000),
  });
  return { status: response.status, body: await response.json().catch(() => ({ error: `Worker returned HTTP ${response.status}` })) };
}

/** Group changes that can alter GraphJin roles apply in a worker batch. */
export async function scheduleGroupGrantsApply(): Promise<void> {
  await requestWorker("/admin/graphjin/group-grants", {}).catch(() => undefined);
}

export function groupErrorResponse(error: unknown): NextResponse {
  if (error instanceof GroupError) {
    const status = error.code === "not_found" ? 404 : error.code === "conflict" || error.code === "lockout" ? 409 : 400;
    return NextResponse.json({ error: error.message, code: error.code }, { status });
  }
  return NextResponse.json({ error: error instanceof Error ? error.message : String(error) }, { status: 500 });
}

export async function readJsonBody(request: Request): Promise<Record<string, unknown> | null> {
  const body = await request.json().catch(() => null);
  return body && typeof body === "object" && !Array.isArray(body) ? (body as Record<string, unknown>) : null;
}

export type ItemOption = { id: string; label: string; detail?: string };

export const ITEM_TYPE_LABELS: Record<ItemType, { label: string; plural: string }> = {
  skill: { label: "Skill", plural: "Skills" },
  workflow: { label: "Workflow", plural: "Workflows" },
  library_collection: { label: "Library collection", plural: "Library collections" },
  library_concept: { label: "Library concept", plural: "Library concepts" },
  metric: { label: "Metric", plural: "Metrics" },
  dashboard: { label: "Dashboard", plural: "Dashboards" },
  watcher: { label: "Watcher", plural: "Watchers" },
  team_memory: { label: "Team memory", plural: "Team memory" },
  data_source: { label: "Data source", plural: "Data sources" },
  saved_query: { label: "Saved query", plural: "Saved queries" },
  api_operation: { label: "API operation", plural: "API operations" },
  action: { label: "Action", plural: "Actions" },
  integration: { label: "Integration", plural: "Integrations" },
  channel: { label: "Channel", plural: "Channels" },
  pack: { label: "Pack", plural: "Packs" },
};

async function graphjinAdmin(orgId: string) {
  const [source] = await db()
    .select({ graphqlUrl: data_source.graphql_url })
    .from(data_source)
    .where(and(eq(data_source.org_id, orgId), eq(data_source.enabled, true)))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);
  if (!source?.graphqlUrl) return null;
  const clean = source.graphqlUrl.replace(/\/+$/, "");
  return {
    endpoint: clean.endsWith("/api/v1/graphql") ? clean : `${clean}/api/v1/graphql`,
    headers: { authorization: `Bearer ${mintGraphjinToken({ orgId, userId: null, role: "admin", ttlSeconds: 120 })}` },
  };
}

type CatalogRow = { id: string; name: string | null; summary: string | null; database_name: string | null; schema_name: string | null; table_name: string | null; column_name: string | null };

/** GraphJin rejects an `and` with one expression, so a single condition stays bare. */
export function catalogWhere(kind: string, databaseName?: string): string {
  const byKind = `{ kind: { eq: ${JSON.stringify(kind)} } }`;
  return databaseName === undefined ? byKind : `{ and: [${byKind}, { database_name: { eq: ${JSON.stringify(databaseName)} } }] }`;
}

export async function graphjinCatalog(orgId: string, kind: string, databaseName?: string): Promise<CatalogRow[]> {
  const admin = await graphjinAdmin(orgId);
  if (!admin) return [];
  const pageSize = 1000;
  const rows: CatalogRow[] = [];
  for (let offset = 0; offset < 200_000; offset += pageSize) {
    const result = await graphjinQuery<{ gj_catalog?: CatalogRow[] }>({
      baseUrl: admin.endpoint,
      headers: admin.headers,
      query: `query AdminCatalog { gj_catalog(where: ${catalogWhere(kind, databaseName)}, limit: ${pageSize}, offset: ${offset}, order_by: { id: asc }) { id name summary database_name schema_name table_name column_name } }`,
      signal: AbortSignal.timeout(30_000),
    }).catch(() => ({ data: undefined, errors: [{ message: "GraphJin is unavailable" }] }));
    if (result.errors?.length) return rows;
    const page = result.data?.gj_catalog ?? [];
    rows.push(...page);
    if (page.length < pageSize) break;
  }
  return rows;
}

/** Tables and columns of one GraphJin source, for the data access editor. */
export async function sourceTables(orgId: string, source: string): Promise<Array<{ schema: string; table: string; columns: string[] }>> {
  const rows = await graphjinCatalog(orgId, "column", source);
  const tables = new Map<string, { schema: string; table: string; columns: string[] }>();
  for (const row of rows) {
    if (!row.table_name || !row.column_name) continue;
    const key = `${row.schema_name ?? ""}.${row.table_name}`;
    const entry = tables.get(key) ?? { schema: row.schema_name ?? "", table: row.table_name, columns: [] };
    if (!entry.columns.includes(row.column_name)) entry.columns.push(row.column_name);
    tables.set(key, entry);
  }
  return [...tables.values()].sort((a, b) => `${a.schema}.${a.table}`.localeCompare(`${b.schema}.${b.table}`));
}

/** Every grantable item of a type, for pickers and access panels. */
export async function listItemOptions(orgId: string, type: ItemType): Promise<ItemOption[]> {
  switch (type) {
    case "skill":
      return (await listWorkSkills(orgId)).map((s) => ({ id: s.name, label: s.name, detail: s.description }));
    case "workflow":
      return (await listWorkflows(orgId))
        .filter((w) => !w.ownerUserId)
        .map((w) => ({ id: w.id, label: w.name, detail: w.description }));
    case "library_concept":
      return (await db()
        .select({ id: library_concept.id, title: library_concept.title, path: library_concept.path })
        .from(library_concept)
        .where(and(eq(library_concept.org_id, orgId), isNull(library_concept.user_id), isNull(library_concept.archived_at)))
        .orderBy(library_concept.path)).map((c) => ({ id: c.id, label: c.title, detail: c.path }));
    case "library_collection": {
      const paths = await db()
        .select({ path: library_concept.path })
        .from(library_concept)
        .where(and(eq(library_concept.org_id, orgId), isNull(library_concept.user_id), isNull(library_concept.archived_at)));
      const prefixes = new Set<string>();
      for (const { path } of paths) {
        const parts = path.split("/").filter(Boolean);
        for (let i = 1; i < parts.length; i++) prefixes.add(`${parts.slice(0, i).join("/")}/`);
      }
      return [...prefixes].sort().map((p) => ({ id: p, label: p }));
    }
    case "metric":
      return (await db()
        .select({ id: metric.id, title: metric.title, role: metric.role })
        .from(metric)
        .where(and(eq(metric.org_id, orgId), eq(metric.active, true)))
        .orderBy(metric.title)).map((m) => ({ id: m.id, label: m.title, detail: m.role }));
    case "dashboard": {
      const roles = new Set<string>();
      for (const r of await db().selectDistinct({ role: metric.role }).from(metric).where(eq(metric.org_id, orgId))) roles.add(r.role);
      for (const r of await db()
        .selectDistinct({ role: operator_profile.role_template })
        .from(operator_profile)
        .where(and(eq(operator_profile.org_id, orgId), ne(operator_profile.role_template, "")))) roles.add(r.role);
      return [...roles].filter(Boolean).sort().map((r) => ({ id: r, label: `${r} dashboard` }));
    }
    case "watcher":
      return (await listWatchers(orgId)).map((w) => ({ id: w.id, label: w.name, detail: w.description ?? undefined }));
    case "team_memory": {
      const sources = (await graphjinCatalog(orgId, "database")).map((d) => d.name ?? d.id);
      return [{ id: "global", label: "Global company memory" }, ...sources.map((s) => ({ id: `database:${s}`, label: `Memory for ${s}` }))];
    }
    case "data_source":
      return (await graphjinCatalog(orgId, "database")).map((d) => ({ id: d.name ?? d.id, label: d.name ?? d.id, detail: d.summary ?? undefined }));
    case "saved_query":
      return (await graphjinCatalog(orgId, "saved_query")).map((q) => ({ id: q.name ?? q.id, label: q.name ?? q.id, detail: q.summary ?? undefined }));
    case "api_operation": {
      const result = await requestWorker("/admin/graphjin/api-operations").catch(() => ({ status: 500, body: {} }));
      const operations = (result.body as { operations?: string[] }).operations ?? [];
      return operations.map((id) => ({ id, label: id.split(":").slice(1).join(" · "), detail: id.split(":")[0] }));
    }
    case "action": {
      const builtIn = (await getActionDescriptors()).map((a) => ({ id: a.kind, label: a.kind, detail: a.description }));
      const pack = (await db()
        .select({ kind: pack_action_definition.kind, description: pack_action_definition.description })
        .from(pack_action_definition)
        .where(eq(pack_action_definition.org_id, orgId))).map((a) => ({ id: a.kind, label: a.kind, detail: a.description }));
      return [...new Map([...builtIn, ...pack].map((a) => [a.id, a])).values()].sort((a, b) => a.label.localeCompare(b.label));
    }
    case "integration":
      return (await listConnectProviders()).map((p) => ({ id: p.pluginName, label: p.providerLabel, detail: p.pluginName }));
    case "channel": {
      const result = await requestWorker("/admin/channels/providers").catch(() => ({ status: 500, body: {} }));
      const providers = (result.body as { providers?: Array<{ id: string; providerLabel: string }> }).providers ?? [];
      const labels = new Map<string, string>(providers.map((provider) => [provider.id, provider.providerLabel]));
      for (const row of await db().selectDistinct({ plugin: channel_identity.channel_plugin }).from(channel_identity).where(eq(channel_identity.org_id, orgId))) {
        if (!labels.has(row.plugin)) labels.set(row.plugin, row.plugin);
      }
      return [...labels].sort(([a], [b]) => a.localeCompare(b)).map(([id, label]) => ({ id, label, detail: id }));
    }
    case "pack":
      return (await db()
        .select({ id: pack_install.pack_id, version: pack_install.version })
        .from(pack_install)
        .where(and(eq(pack_install.org_id, orgId), ne(pack_install.status, "removed")))).map((p) => ({ id: p.id, label: p.id, detail: `v${p.version}` }));
  }
}
