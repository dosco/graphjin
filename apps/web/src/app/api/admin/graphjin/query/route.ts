import { NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { graphjinAdminQuery } from "@/lib/graphjin-admin";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const MAX_QUERY_LENGTH = 100_000;

/** Workbench: run one GraphQL request against the org's GraphJin as admin. */
export async function POST(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await request.json().catch(() => null);
  const query = typeof body?.query === "string" ? body.query.trim() : "";
  if (!query || query.length > MAX_QUERY_LENGTH) {
    return NextResponse.json({ error: "Enter a GraphQL query." }, { status: 400 });
  }
  const variables = body?.variables;
  if (variables !== undefined && (variables === null || typeof variables !== "object" || Array.isArray(variables))) {
    return NextResponse.json({ error: "Variables must be a JSON object." }, { status: 400 });
  }
  const result = await graphjinAdminQuery<unknown>(
    { orgId: await getOrgId(), userId: actor.userId },
    query,
    variables as Record<string, unknown> | undefined,
  );
  if (!result.ok) return NextResponse.json({ error: result.error }, { status: 502 });
  return NextResponse.json({ data: result.data, ...(result.errors.length ? { errors: result.errors } : {}) });
}
