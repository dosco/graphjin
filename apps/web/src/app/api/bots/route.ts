import { NextRequest, NextResponse } from "next/server";
import { nekoCoats } from "@neko/avatars";
import { getCurrentActor } from "@/lib/actor";
import { BOT_PATTERNS, createBot, listBots } from "@/lib/bots";
import { getOrgId } from "@/lib/db";

export async function GET() {
  const [orgId, actor] = await Promise.all([getOrgId(), getCurrentActor()]);
  if (!actor.userId) return NextResponse.json({ error: "sign in" }, { status: 401 });
  return NextResponse.json({ bots: await listBots(orgId, actor.userId) });
}

export async function POST(request: NextRequest) {
  const [orgId, actor] = await Promise.all([getOrgId(), getCurrentActor()]);
  if (!actor.userId) return NextResponse.json({ error: "sign in" }, { status: 401 });
  const body = await request.json().catch(() => ({}));
  const name = typeof body.name === "string" ? body.name.trim() : "";
  const coat = nekoCoats.some((c) => c.name === body.coat) ? (body.coat as string) : null;
  const pattern = BOT_PATTERNS.find((p) => p === body.pattern) ?? null;
  if (!name || !coat || !pattern) {
    return NextResponse.json({ error: "Give the bot a name, a coat and a pattern." }, { status: 400 });
  }
  const created = await createBot(orgId, actor.userId, { name, coat, pattern });
  return NextResponse.json({ bot: created }, { status: 201 });
}
