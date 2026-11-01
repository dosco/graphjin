import { NextRequest, NextResponse } from "next/server";
import { adoptWorkflowForTeam } from "@neko/llm/workflows";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { requireWorkflow } from "@/lib/entitlements";

type RouteContext = {
  params: Promise<{ workflowId: string }>;
};

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// CV4 "Adopt for the team": admin copies a member's personal workflow
// into the team layer (content cherry-pick — never a ref merge).
export async function POST(_request: NextRequest, context: RouteContext) {
  const { workflowId } = await context.params;
  const deniedWorkflow = await requireWorkflow(workflowId);
  if (deniedWorkflow) return deniedWorkflow;
  const actor = await getCurrentActor();
  if (actor.role !== "admin") {
    return NextResponse.json({ error: "admin only" }, { status: 403 });
  }
  const orgId = await getOrgId();
  try {
    const workflow = await adoptWorkflowForTeam(orgId, workflowId, actor.userId);
    return NextResponse.json({ workflow });
  } catch (err) {
    return NextResponse.json(
      { error: err instanceof Error ? err.message : "adopt failed" },
      { status: 404 },
    );
  }
}
