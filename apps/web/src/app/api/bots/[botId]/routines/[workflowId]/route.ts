import { NextRequest, NextResponse } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { listBotRoutineRuns, setBotRoutineEnabled } from "@/lib/bots";
import { getOrgId } from "@/lib/db";

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ botId: string; workflowId: string }> },
) {
  const [{ botId, workflowId }, orgId, actor] = await Promise.all([params, getOrgId(), getCurrentActor()]);
  if (!actor.userId) return NextResponse.json({ error: "sign in" }, { status: 401 });
  const before = request.nextUrl.searchParams.get("before");
  const page = await listBotRoutineRuns(orgId, actor.userId, botId, workflowId, before);
  if (!page) return NextResponse.json({ error: "not found" }, { status: 404 });
  return NextResponse.json(page);
}

export async function PATCH(
  request: NextRequest,
  { params }: { params: Promise<{ botId: string; workflowId: string }> },
) {
  const [{ botId, workflowId }, orgId, actor] = await Promise.all([params, getOrgId(), getCurrentActor()]);
  if (!actor.userId) return NextResponse.json({ error: "sign in" }, { status: 401 });
  const body = await request.json().catch(() => ({}));
  if (typeof body.enabled !== "boolean") {
    return NextResponse.json({ error: "Send enabled as true or false." }, { status: 400 });
  }
  const ok = await setBotRoutineEnabled(orgId, actor.userId, botId, workflowId, body.enabled);
  if (!ok) return NextResponse.json({ error: "not found" }, { status: 404 });
  return NextResponse.json({ ok: true });
}
