import { NextRequest, NextResponse } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { cleanBotPatch, deleteBot, getBotDetail, updateBot } from "@/lib/bots";
import { getOrgId } from "@/lib/db";

type Context = { params: Promise<{ botId: string }> };

export async function GET(_request: NextRequest, { params }: Context) {
  const [{ botId }, orgId, actor] = await Promise.all([params, getOrgId(), getCurrentActor()]);
  if (!actor.userId) return NextResponse.json({ error: "sign in" }, { status: 401 });
  const detail = await getBotDetail(orgId, actor.userId, botId);
  if (!detail) return NextResponse.json({ error: "not found" }, { status: 404 });
  return NextResponse.json(detail);
}

export async function PATCH(request: NextRequest, { params }: Context) {
  const [{ botId }, orgId, actor] = await Promise.all([params, getOrgId(), getCurrentActor()]);
  if (!actor.userId) return NextResponse.json({ error: "sign in" }, { status: 401 });
  const patch = cleanBotPatch(await request.json().catch(() => ({})));
  if (typeof patch === "string") return NextResponse.json({ error: patch }, { status: 400 });
  const updated = await updateBot(orgId, actor.userId, botId, patch);
  if (!updated) return NextResponse.json({ error: "not found" }, { status: 404 });
  return NextResponse.json({ bot: updated });
}

export async function DELETE(_request: NextRequest, { params }: Context) {
  const [{ botId }, orgId, actor] = await Promise.all([params, getOrgId(), getCurrentActor()]);
  if (!actor.userId) return NextResponse.json({ error: "sign in" }, { status: 401 });
  const result = await deleteBot(orgId, actor.userId, botId);
  if (!result) return NextResponse.json({ error: "not found" }, { status: 404 });
  if (result === "running") {
    return NextResponse.json({ error: "This bot is working. Stop the run or wait for it to finish, then delete the bot." }, { status: 409 });
  }
  return NextResponse.json({ ok: true });
}
