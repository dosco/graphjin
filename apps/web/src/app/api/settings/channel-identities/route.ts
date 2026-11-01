import { NextResponse } from "next/server";
import { channel_identity, db, desc, eq } from "@neko/db";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// CH3 admin-map surface: every channel identity the org has seen, for
// linking/blocking. Admin only.
export async function GET() {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") {
    return NextResponse.json({ error: "admin only" }, { status: 403 });
  }
  const orgId = await getOrgId();
  const identities = await db()
    .select()
    .from(channel_identity)
    .where(eq(channel_identity.org_id, orgId))
    .orderBy(desc(channel_identity.updated_at))
    .limit(500);
  return NextResponse.json({ identities });
}
