import "server-only";

import { data_source, db, desc, eq } from "@neko/db";

export type DataSourceRow = {
  id: string;
  org_id: string;
  kind: string;
  graphql_url: string;
  mcp_url: string | null;
  label: string | null;
};

export type PublicDataSource = {
  source: "org" | "unset";
  kind: string;
  graphqlUrl: string;
  mcpUrl: string;
  label: string;
};

export type DataSourceDraft = {
  graphqlUrl: string;
  mcpUrl?: string | null;
  label?: string | null;
};

const DEFAULT_KIND = "graphjin";

// These probes run from a Route Handler and target operator-configured services,
// including Docker DNS names. Next's request-scoped fetch wrapper can reject
// those names even when the Node runtime and container network can resolve them.
// Marking the calls as internal bypasses that wrapper's cache/instrumentation
// path while retaining the platform fetch implementation.
const INTERNAL_FETCH = {
  next: { internal: true },
} as RequestInit;

async function loadRow(orgId: string): Promise<DataSourceRow | null> {
  const rows = await db()
    .select({
      id: data_source.id,
      org_id: data_source.org_id,
      kind: data_source.kind,
      graphql_url: data_source.graphql_url,
      mcp_url: data_source.mcp_url,
      label: data_source.label,
    })
    .from(data_source)
    .where(eq(data_source.org_id, orgId))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);
  return (rows[0] as DataSourceRow | undefined) ?? null;
}

function publicFromRow(row: DataSourceRow | null): PublicDataSource {
  if (!row) {
    return {
      source: "unset",
      kind: DEFAULT_KIND,
      graphqlUrl: "",
      mcpUrl: "",
      label: "primary",
    };
  }
  return {
    source: "org",
    kind: row.kind,
    graphqlUrl: row.graphql_url,
    mcpUrl: row.mcp_url ?? "",
    label: row.label ?? "primary",
  };
}

export async function getDataSourceSettings(orgId: string): Promise<PublicDataSource> {
  const row = await loadRow(orgId);
  return publicFromRow(row);
}

export function validateDraft(draft: DataSourceDraft): string[] {
  const errors: string[] = [];
  const graphqlUrl = draft.graphqlUrl?.trim() ?? "";
  if (!graphqlUrl) {
    errors.push("GraphQL URL is required.");
  } else {
    try {
      const url = new URL(graphqlUrl);
      if (url.protocol !== "http:" && url.protocol !== "https:") {
        errors.push("GraphQL URL must use http or https.");
      }
    } catch {
      errors.push("GraphQL URL is not a valid URL.");
    }
  }

  const mcpUrl = draft.mcpUrl?.trim();
  if (mcpUrl) {
    try {
      const url = new URL(mcpUrl);
      if (url.protocol !== "http:" && url.protocol !== "https:") {
        errors.push("MCP URL must use http or https.");
      }
    } catch {
      errors.push("MCP URL is not a valid URL.");
    }
  }
  return errors;
}

export async function saveDataSourceDraft(
  orgId: string,
  draft: DataSourceDraft,
): Promise<PublicDataSource> {
  const errors = validateDraft(draft);
  if (errors.length > 0) throw new Error(errors.join(" "));

  const existing = await loadRow(orgId);
  const graphqlUrl = draft.graphqlUrl.trim();
  const mcpUrl = draft.mcpUrl?.trim() ? draft.mcpUrl.trim() : null;
  const label = draft.label?.trim() ? draft.label.trim() : "primary";

  if (existing) {
    await db()
      .update(data_source)
      .set({
        kind: existing.kind,
        graphql_url: graphqlUrl,
        mcp_url: mcpUrl,
        label,
        updated_at: new Date(),
      })
      .where(eq(data_source.id, existing.id));
  } else {
    await db().insert(data_source).values({
      org_id: orgId,
      kind: DEFAULT_KIND,
      graphql_url: graphqlUrl,
      mcp_url: mcpUrl,
      label,
    });
  }

  return publicFromRow({
    id: existing?.id ?? "",
    org_id: orgId,
    kind: existing?.kind ?? DEFAULT_KIND,
    graphql_url: graphqlUrl,
    mcp_url: mcpUrl,
    label,
  });
}

export async function hasDataSourceSetup(orgId: string): Promise<boolean> {
  const row = await loadRow(orgId);
  if (!row) return false;
  return validateDraft({
    graphqlUrl: row.graphql_url,
    mcpUrl: row.mcp_url,
    label: row.label,
  }).length === 0;
}

export async function testDataSourceDraft(
  draft: DataSourceDraft,
): Promise<{ graphqlOk: true; mcpOk: boolean | null }> {
  const errors = validateDraft(draft);
  if (errors.length > 0) throw new Error(errors.join(" "));

  const gqlRes = await fetch(draft.graphqlUrl.trim(), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ query: "{ __typename }" }),
    cache: "no-store",
    ...INTERNAL_FETCH,
  }).catch((e) => {
    throw new Error(`GraphQL URL unreachable: ${e instanceof Error ? e.message : String(e)}`);
  });
  const gqlJson = (await gqlRes.json().catch(() => null)) as
    | { data?: unknown; errors?: unknown }
    | null;
  if (!gqlJson || typeof gqlJson !== "object") {
    throw new Error(`GraphQL URL did not return JSON (status ${gqlRes.status}).`);
  }
  if (!("data" in gqlJson) && !("errors" in gqlJson)) {
    throw new Error("GraphQL URL did not return a valid GraphQL response.");
  }

  const mcpUrl = draft.mcpUrl?.trim();
  if (!mcpUrl) return { graphqlOk: true, mcpOk: null };

  const mcpRes = await fetch(mcpUrl, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json, text/event-stream",
    },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: 1,
      method: "tools/list",
    }),
    cache: "no-store",
    ...INTERNAL_FETCH,
  }).catch(() => null);
  return { graphqlOk: true, mcpOk: !!mcpRes && mcpRes.ok };
}
