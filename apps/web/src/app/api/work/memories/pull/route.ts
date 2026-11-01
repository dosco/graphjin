import { NextRequest, NextResponse } from "next/server";
import { applyMemoryPull, listMemoryPullUpdates } from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { holdsItem } from "@/lib/entitlements";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// CV4 pull — "Update my context with the team's latest". Members only;
// admins are the team layer and have nothing to pull.

export async function GET() {
  const actor = await getCurrentActor();
  if (actor.role !== "member" || !actor.userId || !(await holdsItem("team_memory", "global"))) {
    return NextResponse.json({ updates: [] });
  }
  const orgId = await getOrgId();
  const updates = await listMemoryPullUpdates(orgId, actor.userId);
  return NextResponse.json({ updates });
}

export async function POST(request: NextRequest) {
  const actor = await getCurrentActor();
  if (actor.role !== "member" || !actor.userId || !(await holdsItem("team_memory", "global"))) {
    return NextResponse.json({ error: "members only" }, { status: 403 });
  }
  const body = await request.json().catch(() => ({}));
  const decisions = Array.isArray(body.decisions)
    ? body.decisions.filter(
        (d: unknown): d is { originId: string; choice: "take_theirs" | "keep_mine" } =>
          typeof d === "object" &&
          d !== null &&
          typeof (d as { originId?: unknown }).originId === "string" &&
          ((d as { choice?: unknown }).choice === "take_theirs" ||
            (d as { choice?: unknown }).choice === "keep_mine"),
      )
    : [];
  const orgId = await getOrgId();
  const result = await applyMemoryPull({
    orgId,
    userId: actor.userId,
    decisions,
  });
  return NextResponse.json({ ok: true, ...result });
}
