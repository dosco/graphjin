import { NextRequest, NextResponse } from "next/server";
import { and, data_source, db, eq, ne } from "@neko/db";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// ADM2 — multi-source registry. The agent proposes registry changes via
// chat; connection details (URLs) are entered HERE, the secure form
// path — they never pass through model context. Admin only.

export async function GET() {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") {
    return NextResponse.json({ error: "admin only" }, { status: 403 });
  }
  const orgId = await getOrgId();
  const rows = await db()
    .select({
      id: data_source.id,
      name: data_source.name,
      label: data_source.label,
      kind: data_source.kind,
      graphqlUrl: data_source.graphql_url,
      mcpUrl: data_source.mcp_url,
      subscriptionUrl: data_source.subscription_url,
      authMode: data_source.auth_mode,
      isDefault: data_source.is_default,
      enabled: data_source.enabled,
      updatedAt: data_source.updated_at,
    })
    .from(data_source)
    .where(eq(data_source.org_id, orgId));
  return NextResponse.json({ sources: rows });
}

export async function PUT(request: NextRequest) {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") {
    return NextResponse.json({ error: "admin only" }, { status: 403 });
  }
  const body = await request.json().catch(() => ({}));
  const name = typeof body.name === "string" ? body.name.trim() : "";
  if (!/^[a-z0-9][a-z0-9-]*$/.test(name) || name.length > 64) {
    return NextResponse.json(
      { error: "name must be lowercase letters, digits, dashes" },
      { status: 400 },
    );
  }
  const authMode = body.authMode === "jwt" ? "jwt" : "none";
  const SOURCE_KINDS = ["graphjin", "database", "api", "files", "code"];
  const kind =
    typeof body.kind === "string" && SOURCE_KINDS.includes(body.kind)
      ? body.kind
      : "graphjin";
  const orgId = await getOrgId();
  const now = new Date();
  const values = {
    label: typeof body.label === "string" ? body.label : null,
    graphql_url: typeof body.graphqlUrl === "string" ? body.graphqlUrl : "",
    mcp_url: typeof body.mcpUrl === "string" ? body.mcpUrl : null,
    subscription_url:
      typeof body.subscriptionUrl === "string" ? body.subscriptionUrl : null,
    auth_mode: authMode,
    enabled: body.enabled !== false,
    updated_at: now,
  };
  const [existing] = await db()
    .select({ id: data_source.id })
    .from(data_source)
    .where(and(eq(data_source.org_id, orgId), eq(data_source.name, name)))
    .limit(1);
  const [row] = existing
    ? await db()
        .update(data_source)
        .set(values)
        .where(eq(data_source.id, existing.id))
        .returning()
    : await db()
        .insert(data_source)
        .values({ org_id: orgId, kind, name, ...values })
        .returning();
  if (body.isDefault === true && !row.is_default) {
    await db()
      .update(data_source)
      .set({ is_default: false, updated_at: now })
      .where(and(eq(data_source.org_id, orgId), ne(data_source.id, row.id)));
    await db()
      .update(data_source)
      .set({ is_default: true, updated_at: now })
      .where(eq(data_source.id, row.id));
  }
  return NextResponse.json({ ok: true, id: row.id, name });
}
