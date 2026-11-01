import type { SubscriptionSourceKind } from "./store";

export type WorkflowOutputFilter = {
  scope?: string;
  topic?: string;
  mood?: string | string[];
  kinds?: string[];
  org_id?: string;
};

export type SubscriptionQueryPayload = {
  query: string;
  variables: Record<string, unknown>;
};

export function buildSubscriptionQuery(args: {
  sourceKind: SubscriptionSourceKind;
  filter: Record<string, unknown>;
  orgId: string;
}): SubscriptionQueryPayload | null {
  if (args.sourceKind === "workflow_output") {
    return buildWorkflowOutputSubscription(args.filter, args.orgId);
  }
  if (args.sourceKind === "source_change") {
    return buildSourceChangeSubscription(args.filter);
  }
  return null;
}

function buildWorkflowOutputSubscription(
  filter: Record<string, unknown>,
  orgId: string,
): SubscriptionQueryPayload {
  const f = filter as WorkflowOutputFilter;
  const where: Record<string, unknown> = { org_id: { eq: orgId } };
  if (f.scope) where.scope = { eq: f.scope };
  if (f.topic) where.topic = { eq: f.topic };
  if (f.mood) {
    where.mood = Array.isArray(f.mood) ? { in: f.mood } : { eq: f.mood };
  }
  if (f.kinds && f.kinds.length > 0) {
    where.kind = { in: f.kinds };
  }

  const query = `subscription WorkflowOutputMatch($where: workflow_outputWhereInput) {
  workflow_output(where: $where, order_by: { created_at: desc }, limit: 1) {
    id
    org_id
    workflow_run_id
    kind
    scope
    topic
    mood
    title
    created_at
  }
}`;

  return { query, variables: { where } };
}

export type WorkflowOutputMatch = {
  id: string;
  org_id: string;
  workflow_run_id: string;
  kind: string;
  scope: string | null;
  topic: string | null;
  mood: string | null;
  title: string;
  created_at: string;
};

export function parseWorkflowOutputMatch(
  payload: { data: unknown } | null | undefined,
): WorkflowOutputMatch | null {
  if (!payload || !payload.data || typeof payload.data !== "object") {
    return null;
  }
  const data = payload.data as { workflow_output?: unknown };
  const rows = data.workflow_output;
  if (!Array.isArray(rows) || rows.length === 0) return null;
  const row = rows[0] as Record<string, unknown>;
  if (!row.id || typeof row.id !== "string") return null;
  return {
    id: String(row.id),
    org_id: String(row.org_id),
    workflow_run_id: String(row.workflow_run_id),
    kind: String(row.kind),
    scope: row.scope == null ? null : String(row.scope),
    topic: row.topic == null ? null : String(row.topic),
    mood: row.mood == null ? null : String(row.mood),
    title: String(row.title ?? ""),
    created_at: String(row.created_at),
  };
}

// ─── source_change ──────────────────────────────────────────────────────────
// Subscriptions over the operator's data-source DB. Filter is a passthrough to
// GraphJin: `table` + `where` go straight into the compiled subscription. Org
// isolation is enforced by which data_source row the manager subscribes to
// (the operator's tables have no org_id column we could inject).

export type JsonScalar = string | number | boolean | null;

export type SourceChangeFilter = {
  table: string;
  where?: Record<string, unknown>;
  select?: string[];
  primary_key: string[];
  version_column?: string;
};

export type SourceChangeMatch = {
  table: string;
  primary_key: Record<string, JsonScalar>;
  snapshot: Record<string, unknown>;
  version_token: string | null;
};

function buildSourceChangeSubscription(
  filter: Record<string, unknown>,
): SubscriptionQueryPayload | null {
  return buildSourceChangeOperation(filter, {
    kind: "subscription",
    operationName: "SourceChangeMatch",
    limit: 1,
  });
}

/**
 * Build a one-shot read against the data source using the same filter as the
 * subscription, for the dry-run endpoint. Same projection, ordering, and where
 * clause; just a `query` instead of a `subscription` and a configurable limit.
 */
export function buildSourceChangeDryRunQuery(
  filter: Record<string, unknown>,
  limit = 5,
): SubscriptionQueryPayload | null {
  return buildSourceChangeOperation(filter, {
    kind: "query",
    operationName: "SourceChangeDryRun",
    limit,
  });
}

// Serialize a JSON value to a GraphQL literal: object keys unquoted, string
// values quoted/escaped, scalars verbatim. GraphJin where-clauses key on
// columns / operators / nested tables and use quoted strings for values,
// including column references like `{ col: "product.reorderpoint" }`.
function toGraphqlLiteral(value: unknown): string {
  if (value === null || value === undefined) return "null";
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  if (typeof value === "string") return JSON.stringify(value);
  if (Array.isArray(value)) {
    return `[${value.map(toGraphqlLiteral).join(", ")}]`;
  }
  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>).map(
      ([k, v]) => `${k}: ${toGraphqlLiteral(v)}`,
    );
    return `{ ${entries.join(", ")} }`;
  }
  return "null";
}

function buildSourceChangeOperation(
  filter: Record<string, unknown>,
  opts: {
    kind: "subscription" | "query";
    operationName: string;
    limit: number;
  },
): SubscriptionQueryPayload | null {
  const parsed = parseSourceChangeFilter(filter);
  if (!parsed) return null;

  const orderCol = parsed.version_column ?? parsed.primary_key[0];
  const selectCols = uniqueOrdered([
    ...parsed.primary_key,
    ...(parsed.select ?? []),
    ...(parsed.version_column ? [parsed.version_column] : []),
  ]);

  const projection = selectCols.map((c) => `    ${c}`).join("\n");

  // GraphJin compiles `where` at parse time, so it must be inlined into the
  // query literal — passing it as a `$where` variable is rejected ("value for
  // argument 'where' must be null or an object") even for plain literals. The
  // agent still authors a JSON filter; we serialize it to GraphQL here.
  const args: string[] = [];
  if (parsed.where && Object.keys(parsed.where).length > 0) {
    args.push(`where: ${toGraphqlLiteral(parsed.where)}`);
  }
  args.push(`order_by: { ${orderCol}: desc }`);
  args.push(`limit: ${opts.limit}`);

  const query = `${opts.kind} ${opts.operationName} {
  ${parsed.table}(${args.join(", ")}) {
${projection}
  }
}`;

  return { query, variables: {} };
}

/** Validates a SourceChangeFilter's shape; returns null when invalid. */
export function parseSourceChangeFilter(
  filter: Record<string, unknown>,
): SourceChangeFilter | null {
  if (!filter || typeof filter !== "object") return null;
  const f = filter as Partial<SourceChangeFilter>;
  if (typeof f.table !== "string" || f.table.length === 0) return null;
  if (!isIdentifier(f.table)) return null;
  if (!Array.isArray(f.primary_key) || f.primary_key.length === 0) return null;
  if (!f.primary_key.every((c) => typeof c === "string" && isIdentifier(c))) {
    return null;
  }
  if (f.where != null && (typeof f.where !== "object" || Array.isArray(f.where))) {
    return null;
  }
  if (
    f.select != null &&
    (!Array.isArray(f.select) ||
      !f.select.every((c) => typeof c === "string" && isIdentifier(c)))
  ) {
    return null;
  }
  if (
    f.version_column != null &&
    (typeof f.version_column !== "string" || !isIdentifier(f.version_column))
  ) {
    return null;
  }
  return {
    table: f.table,
    where: f.where ?? {},
    select: f.select,
    primary_key: f.primary_key,
    version_column: f.version_column,
  };
}

export function parseSourceChangeMatch(
  payload: { data: unknown } | null | undefined,
  filter: SourceChangeFilter,
): SourceChangeMatch | null {
  if (!payload || !payload.data || typeof payload.data !== "object") return null;
  const data = payload.data as Record<string, unknown>;
  const rows = data[filter.table];
  if (!Array.isArray(rows) || rows.length === 0) return null;
  const row = rows[0];
  if (!row || typeof row !== "object" || Array.isArray(row)) return null;
  const rowObj = row as Record<string, unknown>;

  const pk: Record<string, JsonScalar> = {};
  for (const col of filter.primary_key) {
    const v = rowObj[col];
    if (v === undefined) return null;
    pk[col] = v as JsonScalar;
  }

  const versionCol = filter.version_column;
  const version_token =
    versionCol && rowObj[versionCol] != null ? String(rowObj[versionCol]) : null;

  return {
    table: filter.table,
    primary_key: pk,
    snapshot: rowObj,
    version_token,
  };
}

const IDENT_RE = /^[a-zA-Z_][a-zA-Z0-9_]*$/;

function isIdentifier(s: string): boolean {
  return IDENT_RE.test(s);
}

function uniqueOrdered(xs: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const x of xs) {
    if (seen.has(x)) continue;
    seen.add(x);
    out.push(x);
  }
  return out;
}
