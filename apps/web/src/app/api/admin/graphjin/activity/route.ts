import { NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { graphjinAdminQuery } from "@/lib/graphjin-admin";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// The only writes the activity page makes: the same owner-scoped mutations the
// GraphJin console used. Anything else goes through the workbench.
const MUTATIONS = {
  mark_seen: `mutation AdminMarkWatchEventSeen($id: String!) {
    gj_watch_event(where: { id: { eq: $id } }, update: { seen: true }) { id seen }
  }`,
  approve: `mutation AdminApproveAnnotation($id: String!) {
    gj_artifacts(where: { id: { eq: $id } }, update: { tier: "approved" }) { id tier }
  }`,
  demote: `mutation AdminDemoteAnnotation($id: String!) {
    gj_artifacts(where: { id: { eq: $id } }, update: { tier: "observed" }) { id tier }
  }`,
} as const;

type GraphjinActivityAction = keyof typeof MUTATIONS;

export async function POST(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await request.json().catch(() => null);
  const action = body?.action as string | undefined;
  const id = typeof body?.id === "string" ? body.id.trim() : "";
  if (!action || !Object.hasOwn(MUTATIONS, action) || !id) {
    return NextResponse.json({ error: "Unknown activity action." }, { status: 400 });
  }
  const result = await graphjinAdminQuery<unknown>(
    { orgId: await getOrgId(), userId: actor.userId },
    MUTATIONS[action as GraphjinActivityAction],
    { id },
  );
  if (!result.ok) return NextResponse.json({ error: result.error }, { status: 502 });
  if (result.errors.length) return NextResponse.json({ error: result.errors.join("; ") }, { status: 502 });
  return NextResponse.json({ ok: true });
}
