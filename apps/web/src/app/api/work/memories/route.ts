import { NextRequest, NextResponse } from "next/server";
import { listWorkMemories, effectiveMemoryLayer } from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { heldItemIds } from "@/lib/entitlements";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  const url = new URL(request.url);
  const includeArchived = url.searchParams.get("includeArchived") === "true";
  const orgId = await getOrgId();
  const actor = await getCurrentActor();
  const memories = await listWorkMemories(orgId, {
    includeArchived,
    userId: await effectiveMemoryLayer(orgId, actor),
    teamMemory: await heldItemIds("team_memory"),
  });
  return NextResponse.json({ memories });
}
