import { NextResponse } from "next/server";
import {
  CompatImportError,
  importCompatWebhook,
  listCompatWebhooks,
} from "@neko/llm/workflows/compat";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { readJsonBody } from "@/lib/groups-admin";

export async function GET() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  return NextResponse.json({ webhooks: await listCompatWebhooks(await getOrgId()) });
}

export async function POST(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await readJsonBody(request);
  if (!body || typeof body.reckonWorkflowId !== "string" || typeof body.workflowId !== "string" || typeof body.token !== "string") {
    return NextResponse.json(
      { error: "reckonWorkflowId, workflowId and token are required." },
      { status: 400 },
    );
  }
  try {
    const webhook = await importCompatWebhook({
      orgId: await getOrgId(),
      actorUserId: actor.userId,
      reckonWorkflowId: body.reckonWorkflowId,
      workflowId: body.workflowId,
      token: body.token,
      params: Array.isArray(body.params) ? (body.params as string[]) : null,
      ...(typeof body.batchChunkSize === "number" ? { batchChunkSize: body.batchChunkSize } : {}),
      ...(typeof body.enabled === "boolean" ? { enabled: body.enabled } : {}),
    });
    return NextResponse.json({ webhook }, { status: 201 });
  } catch (error) {
    if (error instanceof CompatImportError) {
      return NextResponse.json({ error: error.message }, { status: 400 });
    }
    throw error;
  }
}
