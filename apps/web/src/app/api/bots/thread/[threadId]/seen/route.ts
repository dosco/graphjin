import { NextResponse } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { markBotSeen } from "@/lib/bots";
import { getOrgId } from "@/lib/db";

type Context = { params: Promise<{ threadId: string }> };

export async function POST(_request: Request, { params }: Context) {
  const [{ threadId }, orgId, actor] = await Promise.all([params, getOrgId(), getCurrentActor()]);
  if (!actor.userId) return NextResponse.json({ error: "sign in" }, { status: 401 });
  const ok = await markBotSeen(orgId, actor.userId, threadId);
  return ok ? NextResponse.json({ ok: true }) : NextResponse.json({ error: "not found" }, { status: 404 });
}
