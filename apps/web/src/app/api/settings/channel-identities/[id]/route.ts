import { NextRequest, NextResponse } from "next/server";
import { and, app_user, channel_identity, db, eq, isNull } from "@neko/db";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";

type RouteContext = {
  params: Promise<{ id: string }>;
};

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// CH3 admin-map verbs: link a channel identity to an app_user, unlink
// it back to anonymous, or block it outright.
export async function PATCH(request: NextRequest, context: RouteContext) {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") {
    return NextResponse.json({ error: "admin only" }, { status: 403 });
  }
  const { id } = await context.params;
  const body = await request.json().catch(() => ({}));
  const action = typeof body.action === "string" ? body.action : "";
  const orgId = await getOrgId();
  const now = new Date();

  const [identity] = await db()
    .select()
    .from(channel_identity)
    .where(and(eq(channel_identity.org_id, orgId), eq(channel_identity.id, id)))
    .limit(1);
  if (!identity) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  if (action === "link") {
    const appUserId = typeof body.appUserId === "string" ? body.appUserId : "";
    const [user] = await db()
      .select({ id: app_user.id })
      .from(app_user)
      .where(
        and(
          eq(app_user.org_id, orgId),
          eq(app_user.id, appUserId),
          isNull(app_user.disabled_at),
        ),
      )
      .limit(1);
    if (!user) {
      return NextResponse.json({ error: "unknown or disabled user" }, { status: 400 });
    }
    const [updated] = await db()
      .update(channel_identity)
      .set({
        app_user_id: user.id,
        status: "linked",
        verified_at: now,
        updated_at: now,
      })
      .where(eq(channel_identity.id, identity.id))
      .returning();
    return NextResponse.json({ identity: updated });
  }

  if (action === "unlink" || action === "block" || action === "unblock") {
    const [updated] = await db()
      .update(channel_identity)
      .set({
        ...(action === "block"
          ? { status: "blocked" }
          : { app_user_id: null, status: "unverified", verified_at: null }),
        updated_at: now,
      })
      .where(eq(channel_identity.id, identity.id))
      .returning();
    return NextResponse.json({ identity: updated });
  }

  return NextResponse.json(
    { error: "action must be link|unlink|block|unblock" },
    { status: 400 },
  );
}
