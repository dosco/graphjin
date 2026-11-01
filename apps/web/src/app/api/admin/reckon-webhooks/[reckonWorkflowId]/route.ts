import { NextResponse } from "next/server";
import { removeCompatWebhook } from "@neko/llm/workflows/compat";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";

type RouteContext = { params: Promise<{ reckonWorkflowId: string }> };

export async function DELETE(_request: Request, context: RouteContext) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const { reckonWorkflowId } = await context.params;
  const removed = await removeCompatWebhook({
    orgId: await getOrgId(),
    actorUserId: actor.userId,
    reckonWorkflowId,
  });
  if (!removed) return NextResponse.json({ error: "Webhook not found." }, { status: 404 });
  return NextResponse.json({ ok: true });
}
