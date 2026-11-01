import { startupPhase } from "@neko/telemetry/startup";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { clearKnowledgeSnapshot, readKnowledgeSnapshot, refreshKnowledgeSnapshot, removeKnowledgeSnapshot } from "./knowledge-cache";

export const KNOWLEDGE_SECTIONS = [
  { section: "tables?limit=500", file: "tables.json", label: "tables" },
  { section: "namespaces", file: "namespaces.json", label: "namespaces" },
  { section: "insights", file: "insights.json", label: "insights" },
  { section: "syntax", file: "syntax.json", label: "syntax" },
] as const;

export const KNOWLEDGE_INDEX_FILE = "INDEX.md";

const INDEX_BODY = `# GraphJin knowledge pack

This directory holds JSON files prefetched from the running GraphJin
server's HTTP discovery endpoints (\`/api/v1/discovery/{section}\`),
plus this index. The four JSONs are regenerated on every worker boot
and on demand. Broad discovery dumps are already on disk in the JSONs
below. Use the runtime's native GraphQL query tool for database reads;
never use a shell command, direct GraphJin connection, or dev tool.

## Files

- **\`tables.json\`** — every table in the database with name, schema,
  database, type, column count. Read this when you need to know which
  tables exist before constructing a query.

- **\`namespaces.json\`** — the namespaces / databases configured in
  GraphJin (when there are multiple). For most analytics tasks the
  default namespace is fine; consult this only when a query has to
  target a non-default database.

- **\`insights.json\`** — hub tables, hot relationships,
  pre-computed \`relationship_paths\`, query templates,
  data-quality flags. Read this **first** when planning a
  multi-table query. Use the relationship paths already present here.

- **\`syntax.json\`** — the GraphJin DSL reference (operators,
  aggregations, pagination, ordering, expression aggregates,
  directives, common mistakes). Read this when authoring a non-trivial
  query — especially aggregations (\`count_*\`, \`sum(expr: ...)\`,
  \`avg(...)\`) and \`where:\` predicates.

## Running queries

After consulting the files above, call the runtime's native GraphQL query
tool with:

\`\`\`json
{ "query": "<read-only graphql>" }
\`\`\`

If a query returns errors, read the GraphQL error and any
\`errors[].extensions.graphjin_repair\` hint, correct the query, and call the
same native tool again.

**Mutations and subscriptions are not allowed.** Any query containing
the words \`mutation\` or \`subscription\` is denied at the tool gate.
`;

export type PrefetchKnowledgeResult = {
  ok: boolean;
  files: { file: string; bytes: number }[];
  error?: string;
};

/**
 * How schema knowledge reaches the agent.
 *  - "legacy": broad discovery dumps prefetched from `/api/v1/discovery/*`
 *    and inlined wholesale into the prompt (anonymous GraphJin).
 *  - "agentic": a SLIM bootstrap prefetched from the role-aware
 *    `gj_catalog` root (table/database summaries + the help-card index +
 *    DSL essentials); everything deeper the agent pulls on demand with
 *    `gj_catalog` queries through its own actor token (GJ4).
 */
export type KnowledgeMode = "legacy" | "agentic";

export type KnowledgePackPaths = {
  knowledgeRoot: string;
  files: {
    tables: string;
    namespaces: string;
    insights: string;
    syntax: string;
    index: string;
    mode: string;
  };
};

export type KnowledgePackContents = {
  mode: KnowledgeMode;
  tables: string;
  namespaces: string;
  insights: string;
  syntax: string;
};

export function knowledgePackPaths(knowledgeRoot: string): KnowledgePackPaths {
  return {
    knowledgeRoot,
    files: {
      tables: join(knowledgeRoot, "tables.json"),
      namespaces: join(knowledgeRoot, "namespaces.json"),
      insights: join(knowledgeRoot, "insights.json"),
      syntax: join(knowledgeRoot, "syntax.json"),
      index: join(knowledgeRoot, KNOWLEDGE_INDEX_FILE),
      mode: join(knowledgeRoot, "mode.json"),
    },
  };
}

async function readOrEmpty(path: string): Promise<string> {
  try {
    return await readFile(path, "utf8");
  } catch {
    return "{}\n";
  }
}

export async function readKnowledgePack(
  knowledge: KnowledgePackPaths,
): Promise<KnowledgePackContents> {
  const snapshot = await readKnowledgeSnapshot(knowledge.knowledgeRoot);
  const read = (file: keyof NonNullable<typeof snapshot>["files"], fallback: string) =>
    snapshot ? Promise.resolve(snapshot.files[file]) : readOrEmpty(fallback);
  const [tables, namespaces, insights, syntax, modeRaw] = await Promise.all([
    read("tables.json", knowledge.files.tables),
    read("namespaces.json", knowledge.files.namespaces),
    read("insights.json", knowledge.files.insights),
    read("syntax.json", knowledge.files.syntax),
    read("mode.json", knowledge.files.mode),
  ]);
  let mode: KnowledgeMode = "legacy";
  try {
    if (JSON.parse(modeRaw).mode === "agentic") mode = "agentic";
  } catch {
    // pre-agentic packs have no mode file — legacy.
  }
  return { mode, tables, namespaces, insights, syntax };
}

const FETCH_TIMEOUT_MS = 10_000;
const MAX_RETRIES = 3;

async function fetchSection(
  discoveryUrl: string,
  section: string,
  label: string,
): Promise<string> {
  const url = `${discoveryUrl.replace(/\/+$/, "")}/${section}`;
  let lastErr: unknown;
  for (let attempt = 1; attempt <= MAX_RETRIES; attempt++) {
    const ctrl = new AbortController();
    const timeout = setTimeout(() => ctrl.abort(), FETCH_TIMEOUT_MS);
    try {
      const res = await fetch(url, {
        headers: { Accept: "application/json" },
        signal: ctrl.signal,
      });
      if (!res.ok) {
        throw new Error(`${res.status} ${res.statusText}`);
      }
      return JSON.stringify(await res.json(), null, 2);
    } catch (e) {
      lastErr = e;
      if (attempt < MAX_RETRIES) {
        await new Promise((r) => setTimeout(r, 1000 * 2 ** (attempt - 1)));
      }
    } finally {
      clearTimeout(timeout);
    }
  }
  const msg = lastErr instanceof Error ? lastErr.message : String(lastErr);
  throw new Error(
    `discovery ${label} fetch failed after ${MAX_RETRIES} attempts: ${msg}`,
  );
}

export async function prefetchKnowledgePack(args: {
  discoveryUrl: string;
  destDir: string;
}): Promise<PrefetchKnowledgeResult> {
  const { discoveryUrl, destDir } = args;
  await mkdir(destDir, { recursive: true });
  try {
    const results = await Promise.all(
      KNOWLEDGE_SECTIONS.map((s) => fetchSection(discoveryUrl, s.section, s.label)),
    );
    const written: { file: string; bytes: number }[] = [];
    for (let i = 0; i < KNOWLEDGE_SECTIONS.length; i++) {
      const file = KNOWLEDGE_SECTIONS[i].file;
      const json = results[i];
      await writeFile(join(destDir, file), json, "utf8");
      written.push({ file, bytes: json.length });
    }
    await writeFile(join(destDir, KNOWLEDGE_INDEX_FILE), INDEX_BODY, "utf8");
    // Stamp the mode: a deployment can move agentic→legacy (auth_mode
    // change), and a stale agentic marker over legacy-format files makes
    // the prompt builder describe a pack that isn't there.
    await writeFile(
      join(destDir, "mode.json"),
      JSON.stringify({ mode: "legacy" }),
      "utf8",
    );
    return { ok: true, files: written };
  } catch (e) {
    return {
      ok: false,
      files: [],
      error: e instanceof Error ? e.message : String(e),
    };
  }
}

export function discoveryUrlFromMcpUrl(mcpUrl: string): string {
  return mcpUrl.replace(/\/mcp\/?$/, "/discovery");
}

export function graphqlUrlFromMcpUrl(mcpUrl: string): string {
  return mcpUrl.replace(/\/mcp\/?$/, "/graphql");
}

const AGENTIC_INDEX_BODY = `# GraphJin knowledge pack (agentic mode)

This directory holds a SLIM bootstrap prefetched from the role-aware
\`gj_catalog\` root: table, database, and API operation summaries, the help-card index,
and the query-DSL essentials. It is deliberately not the whole schema.

Everything deeper you discover ON DEMAND with catalog queries through the
runtime's native GraphQL query tool. The trusted broker executes them under
your actor identity, so you only ever see what your role allows:

\`\`\`graphql
query { gj_catalog(search: "orders customers join", limit: 10) { id kind name summary } }
query { gj_catalog(id: "table:<db>:<schema>.<table>") { id name summary details_json examples_json edges_json } }
query { gj_catalog(where: { kind: { eq: "column" } }, search: "<table name>", limit: 30) { id name summary } }
\`\`\`

Catalog row kinds: help, database, table, api_operation, column, relationship,
function, capability. \`gj_catalog(id: "...")\` returns one detailed
card (details_json, examples_json, edges_json, safety_json). Start from
\`help:discovery\` when unsure.

For join planning, use relationship catalog rows plus table-card
\`details_json\`, \`examples_json\`, and \`edges_json\`. Source-mode
deployments may disable GraphJin dev tools, so do not call \`find_path\`,
\`explore_relationships\`, \`get_table_sample\`, \`list_tables\`,
\`describe_table\`, or \`health\`.

## Files

- **\`tables.json\`** — every table visible to the service role
  (catalog id, name, one-line summary). Use the catalog id with
  \`gj_catalog(id:)\` for column-level detail.
- **\`namespaces.json\`** — the configured databases/sources.
- **\`insights.json\`** — the help-card index: what the catalog can
  teach you and which card to pull for each topic, plus API operations.
- **\`syntax.json\`** — query-DSL essentials (filters + query shape)
  pulled from the catalog's help cards. Pull other help cards on
  demand for mutations, fragments, workflows, errors.

## Running queries

\`\`\`json
{ "query": "<read-only graphql>" }
\`\`\`

If a query returns errors, use \`errors[].extensions.graphjin_repair\`
when present, correct the query, and call the same native tool again.
Mutations and subscriptions are denied at the broker.
`;

const HELP_DETAIL_IDS = ["help:query", "help:filters"] as const;

type CatalogQueryFn = (
  query: string,
) => Promise<{ data: unknown; errors?: Array<{ message: string }> }>;

async function catalogRows(
  query: CatalogQueryFn,
  gql: string,
): Promise<unknown[]> {
  const result = await query(gql);
  if (result.errors?.length) {
    throw new Error(
      `gj_catalog query failed: ${result.errors.map((e) => e.message).join("; ")}`,
    );
  }
  const rows = (result.data as { gj_catalog?: unknown } | null)?.gj_catalog;
  // List queries return an array; gj_catalog(id:) returns one object.
  if (Array.isArray(rows)) return rows;
  if (rows && typeof rows === "object") return [rows];
  throw new Error("gj_catalog returned no rows");
}

/**
 * Agentic-mode prefetch: build the slim bootstrap pack from the
 * `gj_catalog` root using a minted service token, instead of the
 * legacy anonymous `/api/v1/discovery/*` dumps. Same four files, far
 * smaller content — the agent pulls detail on demand, role-scoped.
 */
export async function prefetchAgenticKnowledgePack(args: {
  graphqlUrl: string;
  token: string;
  destDir: string;
  fetchImpl?: typeof fetch;
}): Promise<PrefetchKnowledgeResult> {
  const doFetch = args.fetchImpl ?? fetch;
  const query: CatalogQueryFn = async (q) => startupPhase("knowledge.catalog_request", async () => {
    const res = await doFetch(args.graphqlUrl, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        authorization: `Bearer ${args.token}`,
      },
      body: JSON.stringify({ query: q }),
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
    });
    if (!res.ok) throw new Error(`gj_catalog HTTP ${res.status}`);
    return (await res.json()) as {
      data: unknown;
      errors?: Array<{ message: string }>;
    };
  });

  await mkdir(args.destDir, { recursive: true });
  try {
    const [tables, databases, apiOperations, helpIndex, helpDetails, relationships, languageIndex] =
      await Promise.all([
        catalogRows(
          query,
          `query { gj_catalog(where: { kind: { eq: "table" } }, limit: 500) { id name summary } }`,
        ),
        catalogRows(
          query,
          `query { gj_catalog(where: { kind: { eq: "database" } }, limit: 50) { id name summary } }`,
        ),
        catalogRows(
          query,
          `query { gj_catalog(where: { kind: { eq: "api_operation" } }, limit: 500) { id name summary } }`,
        ),
        catalogRows(
          query,
          `query { gj_catalog(where: { kind: { eq: "help" } }, limit: 50) { id name summary } }`,
        ),
        Promise.all(
          HELP_DETAIL_IDS.map((id) =>
            catalogRows(
              query,
              `query { gj_catalog(id: "${id}") { id name summary details_json examples_json } }`,
            ),
          ),
        ),
        catalogRows(
          query,
          `query { gj_catalog(where: { kind: { eq: "relationship" } }, limit: 1000) { id name summary } }`,
        ),
        catalogRows(
          query,
          `query { gj_catalog(where: { kind: { in: ["query_pattern", "directive", "operator_set"] } }, limit: 50) { id kind name summary } }`,
        ).catch(() => [] as unknown[]),
      ]);

    // The language cards carry the DSL idioms (grouped summaries via
    // distinct + sum_<col>, expression aggregates, analytics directives)
    // that let one aggregate query replace dozens of paginated selects.
    // The pointer cards alone teach nothing — resolve their examples now,
    // query patterns first: they're the cards that change how the agent
    // queries, the directive list is reference detail.
    const kindRank: Record<string, number> = {
      query_pattern: 0,
      operator_set: 1,
      directive: 2,
    };
    const languageCards = (
      await Promise.all(
        (languageIndex as Array<{ id?: string; kind?: string }>)
          .filter((c) => c.id)
          .sort(
            (a, b) =>
              (kindRank[a.kind ?? ""] ?? 9) - (kindRank[b.kind ?? ""] ?? 9),
          )
          .slice(0, 24)
          .map((c) =>
            catalogRows(
              query,
              `query { gj_catalog(id: "${c.id}") { id kind name summary examples_json } }`,
            ).catch(() => [] as unknown[]),
          ),
      )
    ).flat();
    const SYNTAX_PATTERN_BUDGET = 4_000;
    let patternBytes = 0;
    const patterns: Array<{
      name?: string;
      summary?: string;
      examples: unknown[];
    }> = [];
    for (const card of languageCards) {
      const c = card as {
        kind?: string;
        name?: string;
        summary?: string;
        examples_json?: string;
      };
      let examples: unknown[] = [];
      try {
        const v = JSON.parse(c.examples_json ?? "[]") as unknown;
        examples = (Array.isArray(v) ? v : [v]).slice(0, 2).filter(Boolean);
      } catch {
        examples = [];
      }
      // A directive without an example is a name and nothing else —
      // not worth prompt budget. Query patterns earn a slot on their
      // summaries alone ("group_by does not exist").
      if (examples.length === 0 && c.kind !== "query_pattern") continue;
      const entry = { name: c.name, summary: c.summary, examples };
      const size = JSON.stringify(entry).length;
      if (patternBytes + size > SYNTAX_PATTERN_BUDGET) continue;
      patternBytes += size;
      patterns.push(entry);
    }

    // Hub tables — the legacy pack's insights (hub tables, join paths, ready
    // query templates) are what made first answers fast; rebuild them from
    // the catalog. Relationship row ids encode full join paths
    // ("relationship:db:schema.table.col->db:schema.table.col"): rank tables
    // by how many paths touch them, then attach the readable paths to the
    // top tables' cards.
    type CatalogRow = { id?: string; name?: string; summary?: string };
    const relSide = (s: string) => {
      const label = s.replace(/^[^:]+:/, "");
      const parts = label.split(".");
      return { label, table: parts.length >= 2 ? parts[parts.length - 2] : label };
    };
    const joinPaths = (relationships as CatalogRow[])
      .map((rel) => /^relationship:(.+?)->(.+)$/.exec(rel.id ?? ""))
      .filter((m): m is RegExpExecArray => m !== null)
      .map((m) => ({ from: relSide(m[1]), to: relSide(m[2]) }));
    const idByTableName = new Map<string, string>();
    for (const t of tables as CatalogRow[]) {
      if (t.id && t.name) idByTableName.set(t.name, t.id);
    }
    // Weight the referencing (from) side heavier: fact tables hold the
    // measures analytical questions aggregate over, and they show up as
    // the from-side of many paths — referenced-count alone crowns lookup
    // tables and leaves the fact tables out of the hub list.
    const hubScores = new Map<string, number>();
    for (const p of joinPaths) {
      const fromId = idByTableName.get(p.from.table);
      if (fromId) hubScores.set(fromId, (hubScores.get(fromId) ?? 0) + 3);
      const toId = idByTableName.get(p.to.table);
      if (toId) hubScores.set(toId, (hubScores.get(toId) ?? 0) + 1);
    }
    // Outgoing-FK count per table: fact tables (salesorderdetail) reference
    // many tables, lookup tables (unitmeasure) reference none — the signal
    // that separates analytically hot joins from alphabetical noise.
    const fkOut = new Map<string, number>();
    for (const p of joinPaths) {
      fkOut.set(p.from.table, (fkOut.get(p.from.table) ?? 0) + 1);
    }
    // Tie-break equal scores by the weight of each table's neighbors:
    // the bridge into the busiest tables (salesorderdetail -> product +
    // salesorderheader) beats history/junction tables of equal FK count.
    const neighborScore = new Map<string, number>();
    for (const p of joinPaths) {
      const fromId = idByTableName.get(p.from.table);
      const toId = idByTableName.get(p.to.table);
      if (fromId)
        neighborScore.set(
          fromId,
          (neighborScore.get(fromId) ?? 0) + (toId ? (hubScores.get(toId) ?? 0) : 0),
        );
      if (toId)
        neighborScore.set(
          toId,
          (neighborScore.get(toId) ?? 0) + (fromId ? (hubScores.get(fromId) ?? 0) : 0),
        );
    }
    const hubIds = [...hubScores.entries()]
      .sort(
        (a, b) =>
          b[1] - a[1] ||
          (neighborScore.get(b[0]) ?? 0) - (neighborScore.get(a[0]) ?? 0),
      )
      .slice(0, 10)
      .map(([id]) => id);
    // A table bridging several hubs (salesorderdetail: product +
    // salesorderheader) is the join that answers analytical questions —
    // weigh distinct-hub connectivity above raw FK count.
    const hubIdSet = new Set(hubIds);
    const hubNames = new Set(
      [...idByTableName.entries()]
        .filter(([, id]) => hubIdSet.has(id))
        .map(([name]) => name),
    );
    const hubsTouched = new Map<string, Set<string>>();
    for (const p of joinPaths) {
      for (const [self, other] of [
        [p.from.table, p.to.table],
        [p.to.table, p.from.table],
      ] as const) {
        if (hubNames.has(other)) {
          let touched = hubsTouched.get(self);
          if (!touched) hubsTouched.set(self, (touched = new Set()));
          touched.add(other);
        }
      }
    }
    const linkScore = (t: string) =>
      (hubsTouched.get(t)?.size ?? 0) * 4 + (fkOut.get(t) ?? 0);
    const hubCards = (
      await Promise.all(
        hubIds.map((id) =>
          catalogRows(
            query,
            `query { gj_catalog(id: "${id}") { id name summary examples_json } }`,
          ).catch(() => []),
        ),
      )
    ).flat();
    const hubTables = hubCards.map((card) => {
      const c = card as {
        id?: string;
        name?: string;
        summary?: string;
        examples_json?: string;
      };
      const parse = (raw: string | undefined): unknown[] => {
        try {
          const v = JSON.parse(raw ?? "[]");
          return Array.isArray(v) ? v : [v];
        } catch {
          return [];
        }
      };
      return {
        id: c.id,
        name: c.name,
        summary: c.summary,
        examples: parse(c.examples_json).slice(0, 2),
        join_paths: joinPaths
          .filter((p) => p.from.table === c.name || p.to.table === c.name)
          .sort((a, b) => {
            const other = (p: (typeof joinPaths)[number]) =>
              p.from.table === c.name ? p.to.table : p.from.table;
            return linkScore(other(b)) - linkScore(other(a));
          })
          .slice(0, 8)
          .map((p) => `${p.from.label} -> ${p.to.label}`),
      };
    });

    const sections: Array<{ file: string; json: string }> = [
      { file: "tables.json", json: JSON.stringify({ tables }, null, 2) },
      { file: "namespaces.json", json: JSON.stringify({ databases }, null, 2) },
      {
        file: "insights.json",
        json: JSON.stringify(
          {
            hub_tables: hubTables,
            api_operations: apiOperations,
            help_cards: helpIndex,
            note:
              "Pull any card's full guidance on demand: gj_catalog(id: \"help:<topic>\") { details_json examples_json }",
          },
          null,
          2,
        ),
      },
      {
        file: "syntax.json",
        json: JSON.stringify(
          { essentials: helpDetails.flat(), patterns },
          null,
          1,
        ),
      },
    ];
    const written: { file: string; bytes: number }[] = [];
    for (const { file, json } of sections) {
      await writeFile(join(args.destDir, file), json, "utf8");
      written.push({ file, bytes: json.length });
    }
    await writeFile(join(args.destDir, KNOWLEDGE_INDEX_FILE), AGENTIC_INDEX_BODY, "utf8");
    await writeFile(
      join(args.destDir, "mode.json"),
      JSON.stringify({ mode: "agentic" }),
      "utf8",
    );
    return { ok: true, files: written };
  } catch (e) {
    return {
      ok: false,
      files: [],
      error: e instanceof Error ? e.message : String(e),
    };
  }
}

/**
 * The one prefetch entry point callers use: reads the org's default
 * data source and picks the mode — auth_mode 'jwt' (sources/agentic
 * deployment) prefetches the slim catalog pack with a minted service
 * token; anything else keeps today's legacy discovery dumps.
 */
export async function prefetchKnowledgeForOrg(
  orgId: string,
  destDir: string,
  options: { refresh?: boolean } = {},
): Promise<PrefetchKnowledgeResult & { mode: KnowledgeMode }> {
  const { data_source, db, desc, eq } = await import("@neko/db");
  const [src] = await db()
    .select({
      id: data_source.id,
      authMode: data_source.auth_mode,
      mcpUrl: data_source.mcp_url,
      graphqlUrl: data_source.graphql_url,
    })
    .from(data_source)
    .where(eq(data_source.org_id, orgId))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);
  if (!src?.mcpUrl && !src?.graphqlUrl) {
    await clearKnowledgeSnapshot(destDir, "none", "legacy");
    return { ok: false, files: [], error: "no data source configured", mode: "legacy" };
  }
  if (src.authMode === "jwt") {
    const { mintGraphjinToken } = await import("./graphjin/token");
    const graphqlUrl = src.graphqlUrl || graphqlUrlFromMcpUrl(src.mcpUrl as string);
    const result = await refreshKnowledgeSnapshot({
      root: destDir, mode: "agentic", refresh: options.refresh,
      source: JSON.stringify([orgId, src.id, graphqlUrl, src.authMode, "service", 2]),
      revision: async () => {
        // Revision metadata is admin-only; knowledge content remains service-scoped.
        const response = await fetch(graphqlUrl, {
          method: "POST",
          headers: { "content-type": "application/json", authorization: `Bearer ${mintGraphjinToken({ orgId, userId: null, role: "admin" })}` },
          body: JSON.stringify({ query: 'query { gj_config(id: "current") { catalog_revision } }' }),
          signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
        });
        const body = await response.json();
        if (!response.ok || body.errors?.length) throw new Error("GraphJin catalog revision unavailable");
        const row = Array.isArray(body.data?.gj_config) ? body.data.gj_config[0] : body.data?.gj_config;
        if (typeof row?.catalog_revision !== "string") throw new Error("GraphJin returned no catalog revision");
        return row.catalog_revision;
      },
      build: directory => prefetchAgenticKnowledgePack({
        graphqlUrl, token: mintGraphjinToken({ orgId, userId: null, role: "service" }), destDir: directory,
      }),
    });
    return { ...result, mode: "agentic" };
  }
  // Legacy discovery has no revision contract. Clear a previous JWT snapshot
  // before writing its legacy replacement so readers never use the old source.
  if (await readKnowledgeSnapshot(destDir)) {
    await clearKnowledgeSnapshot(destDir, "legacy", "legacy");
  }
  const result = await prefetchKnowledgePack({
    discoveryUrl: discoveryUrlFromMcpUrl((src.mcpUrl ?? src.graphqlUrl) as string),
    destDir,
  });
  if (result.ok) await removeKnowledgeSnapshot(destDir);
  return { ...result, mode: "legacy" };
}
