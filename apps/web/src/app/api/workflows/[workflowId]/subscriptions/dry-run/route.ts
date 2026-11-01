import { NextRequest, NextResponse } from "next/server";
import {
  buildSourceChangeDryRunQuery,
  getDataSourceForOrg,
  parseSourceChangeFilter,
} from "@neko/llm/workflows";
import { graphjinQuery } from "@neko/llm/graphjin/client";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const DRY_RUN_LIMIT = 5;

/**
 * Compile a proposed source_change filter and execute it as a one-shot
 * read against the operator's data source. Lets the agent (or operator)
 * preview which rows a subscription would match before saving it.
 *
 * Body: { sourceKind: "source_change", filter: { table, where, primary_key, ... } }
 */
export async function POST(req: NextRequest) {
  const body = await req.json().catch(() => ({}));
  const sourceKind = body.sourceKind as string | undefined;

  if (sourceKind !== "source_change") {
    return NextResponse.json(
      {
        error:
          "dry-run currently supports sourceKind=\"source_change\" only",
        code: "unsupported_source_kind",
      },
      { status: 400 },
    );
  }

  const filter =
    typeof body.filter === "object" && body.filter !== null
      ? (body.filter as Record<string, unknown>)
      : {};

  const parsed = parseSourceChangeFilter(filter);
  if (!parsed) {
    return NextResponse.json(
      {
        error:
          "filter requires { table: string, primary_key: string[] } and optional where/select/version_column (identifiers only)",
        code: "invalid_filter",
      },
      { status: 400 },
    );
  }

  const limit =
    typeof body.limit === "number" && body.limit > 0 && body.limit <= 50
      ? Math.floor(body.limit)
      : DRY_RUN_LIMIT;

  const orgId = await getOrgId();
  const dataSource = await getDataSourceForOrg(orgId);
  if (!dataSource) {
    return NextResponse.json(
      {
        error:
          "no data_source configured for this org — source_change subscriptions need a configured data source",
        code: "no_data_source",
      },
      { status: 422 },
    );
  }

  const payload = buildSourceChangeDryRunQuery(filter, limit);
  if (!payload) {
    return NextResponse.json(
      { error: "failed to compile filter", code: "compile_failed" },
      { status: 400 },
    );
  }

  let result;
  try {
    result = await graphjinQuery({
      baseUrl: dataSource.graphqlUrl,
      query: payload.query,
      variables: payload.variables,
    });
  } catch (err) {
    return NextResponse.json(
      {
        error: err instanceof Error ? err.message : String(err),
        code: "graphjin_error",
        compiledQuery: payload.query,
      },
      { status: 502 },
    );
  }

  if (result.errors && result.errors.length > 0) {
    return NextResponse.json(
      {
        error: "graphjin returned errors",
        code: "graphjin_errors",
        compiledQuery: payload.query,
        graphjinErrors: result.errors,
      },
      { status: 422 },
    );
  }

  const data = (result.data ?? {}) as Record<string, unknown>;
  const raw = data[parsed.table];
  const rows: unknown[] = Array.isArray(raw) ? raw : [];

  return NextResponse.json({
    compiledQuery: payload.query,
    variables: payload.variables,
    rowCount: rows.length,
    rows,
  });
}
