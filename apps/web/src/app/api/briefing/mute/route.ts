import { NextResponse } from "next/server";
import { and, db, eq, gt, muted_scope } from "@neko/db";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const DURATIONS_MS: Record<string, number> = {
  "1h": 60 * 60 * 1000,
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
};

export async function GET() {
  const orgId = await getOrgId();
  const rows = await db()
    .select({
      scope: muted_scope.scope,
      mutedUntil: muted_scope.muted_until,
    })
    .from(muted_scope)
    .where(
      and(eq(muted_scope.org_id, orgId), gt(muted_scope.muted_until, new Date())),
    );
  return NextResponse.json({
    mutes: rows.map((r) => ({
      scope: r.scope,
      mutedUntil: r.mutedUntil.toISOString(),
    })),
  });
}

export async function POST(request: Request) {
  const orgId = await getOrgId();
  const body = await request.json().catch(() => ({}));
  const scope = typeof body.scope === "string" ? body.scope.trim() : "";
  const durationMs = DURATIONS_MS[String(body.duration)];
  if (!scope) {
    return NextResponse.json({ error: "scope is required" }, { status: 400 });
  }
  if (!durationMs) {
    return NextResponse.json(
      { error: "duration must be one of 1h | 24h | 7d" },
      { status: 400 },
    );
  }
  const mutedUntil = new Date(Date.now() + durationMs);
  await db()
    .insert(muted_scope)
    .values({ org_id: orgId, scope, muted_until: mutedUntil })
    .onConflictDoUpdate({
      target: [muted_scope.org_id, muted_scope.scope],
      set: { muted_until: mutedUntil },
    });
  return NextResponse.json({ ok: true, scope, mutedUntil: mutedUntil.toISOString() });
}

export async function DELETE(request: Request) {
  const orgId = await getOrgId();
  const url = new URL(request.url);
  const scope = url.searchParams.get("scope")?.trim() ?? "";
  if (!scope) {
    return NextResponse.json({ error: "scope is required" }, { status: 400 });
  }
  await db()
    .delete(muted_scope)
    .where(and(eq(muted_scope.org_id, orgId), eq(muted_scope.scope, scope)));
  return NextResponse.json({ ok: true });
}
