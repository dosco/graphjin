import { NextResponse } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { botStarters, getBotByThread } from "@/lib/bots";
import { getOrgId } from "@/lib/db";

export async function GET(_request: Request, { params }: { params: Promise<{ threadId: string }> }) {
  const [{ threadId }, orgId, actor] = await Promise.all([params, getOrgId(), getCurrentActor()]);
  if (!actor.userId) return NextResponse.json({ error: "sign in" }, { status: 401 });
  const found = await getBotByThread(orgId, actor.userId, threadId);
  if (!found) return NextResponse.json({ bot: null, starters: [] });
  const starters = found.instructions ? [] : botStarters(found.name);
  return NextResponse.json({ bot: found, starters });
}
