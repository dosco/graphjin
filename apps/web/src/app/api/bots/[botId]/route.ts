import { NextRequest, NextResponse } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { setBotInstructions } from "@/lib/bots";
import { getOrgId } from "@/lib/db";

export async function PATCH(request: NextRequest, { params }: { params: Promise<{ botId: string }> }) {
  const [{ botId }, orgId, actor] = await Promise.all([params, getOrgId(), getCurrentActor()]);
  if (!actor.userId) return NextResponse.json({ error: "sign in" }, { status: 401 });
  const body = await request.json().catch(() => ({}));
  if (typeof body.instructions !== "string") {
    return NextResponse.json({ error: "Send the instructions as text." }, { status: 400 });
  }
  const updated = await setBotInstructions(orgId, actor.userId, botId, body.instructions);
  if (!updated) return NextResponse.json({ error: "not found" }, { status: 404 });
  return NextResponse.json({ bot: updated });
}
