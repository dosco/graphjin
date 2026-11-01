import { NextRequest, NextResponse } from "next/server";
import { and, data_source_secret, db, desc, eq } from "@neko/db";
import { maybeEncryptSecret } from "@neko/llm/secrets";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// OL5 — named connection secrets for chat-first source config. This is the
// ONLY path a secret value travels: the admin form. The agent references
// the name; the worker resolves the value at apply. Values are enc:v1 at
// rest and never returned. Admin only.

export async function GET() {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") {
    return NextResponse.json({ error: "admin only" }, { status: 403 });
  }
  const orgId = await getOrgId();
  const rows = await db()
    .select({
      name: data_source_secret.name,
      description: data_source_secret.description,
      updatedAt: data_source_secret.updated_at,
    })
    .from(data_source_secret)
    .where(eq(data_source_secret.org_id, orgId))
    .orderBy(desc(data_source_secret.updated_at));
  return NextResponse.json({
    secrets: rows.map((r) => ({
      name: r.name,
      description: r.description,
      updatedAt: r.updatedAt.toISOString(),
    })),
  });
}

export async function PUT(request: NextRequest) {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") {
    return NextResponse.json({ error: "admin only" }, { status: 403 });
  }
  const body = await request.json().catch(() => ({}));
  const name = typeof body.name === "string" ? body.name.trim() : "";
  const value = typeof body.value === "string" ? body.value : "";
  if (!/^[A-Za-z0-9._-]+$/.test(name) || name.length > 128) {
    return NextResponse.json(
      { error: "name must be letters, digits, . _ - (no spaces)" },
      { status: 400 },
    );
  }
  if (!value) {
    return NextResponse.json({ error: "value is required" }, { status: 400 });
  }
  const orgId = await getOrgId();
  const now = new Date();
  await db()
    .insert(data_source_secret)
    .values({
      org_id: orgId,
      name,
      value_enc: maybeEncryptSecret(value),
      description: typeof body.description === "string" ? body.description : null,
      updated_at: now,
    })
    .onConflictDoUpdate({
      target: [data_source_secret.org_id, data_source_secret.name],
      set: {
        value_enc: maybeEncryptSecret(value),
        ...(typeof body.description === "string"
          ? { description: body.description }
          : {}),
        updated_at: now,
      },
    });
  return NextResponse.json({ ok: true, name });
}

export async function DELETE(request: NextRequest) {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") {
    return NextResponse.json({ error: "admin only" }, { status: 403 });
  }
  const url = new URL(request.url);
  const name = (url.searchParams.get("name") ?? "").trim();
  if (!name) {
    return NextResponse.json({ error: "name is required" }, { status: 400 });
  }
  const orgId = await getOrgId();
  await db()
    .delete(data_source_secret)
    .where(
      and(
        eq(data_source_secret.org_id, orgId),
        eq(data_source_secret.name, name),
      ),
    );
  return NextResponse.json({ ok: true });
}
