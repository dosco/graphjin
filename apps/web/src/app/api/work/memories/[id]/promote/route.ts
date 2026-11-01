import { NextRequest, NextResponse } from "next/server";
import { getWorkMemory, promoteWorkMemoryToOrg } from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";

type RouteContext = {
  params: Promise<{ id: string }>;
};

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// CV2: admin pulls a member's personal memory into the team layer.
export async function POST(_request: NextRequest, context: RouteContext) {
  const { id } = await context.params;
  const actor = await getCurrentActor();
  if (actor.role !== "admin") {
    return NextResponse.json({ error: "admin only" }, { status: 403 });
  }
  const orgId = await getOrgId();
  const target = await getWorkMemory(orgId, id);
  if (!target) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  try {
    const memory = await promoteWorkMemoryToOrg({
      orgId,
      memoryId: id,
      promotedBy: actor.userId ?? "admin",
    });
    return NextResponse.json({ memory });
  } catch (err) {
    return NextResponse.json(
      { error: err instanceof Error ? err.message : "promote failed" },
      { status: 404 },
    );
  }
}
