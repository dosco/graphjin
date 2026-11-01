import { NextResponse } from "next/server";
import { saveWorkflowSpendOverride, SpendSettingsError } from "@neko/llm/spend";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { readJsonBody } from "@/lib/groups-admin";

type RouteContext = { params: Promise<{ workflowId: string }> };

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

async function save(workflowId: string, draft: { hourlyUsd: number | null; dailyUsd: number | null }) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  if (!UUID.test(workflowId)) return NextResponse.json({ error: "Workflow not found." }, { status: 404 });
  try {
    return NextResponse.json(await saveWorkflowSpendOverride(await getOrgId(), actor.userId, workflowId, draft));
  } catch (error) {
    if (error instanceof SpendSettingsError) {
      return NextResponse.json({ error: error.message }, { status: 400 });
    }
    throw error;
  }
}

export async function PUT(request: Request, context: RouteContext) {
  const { workflowId } = await context.params;
  const body = await readJsonBody(request);
  const value = (key: string) => (body?.[key] === null || body?.[key] === undefined ? null : body[key]);
  return save(workflowId, { hourlyUsd: value("hourlyUsd") as number | null, dailyUsd: value("dailyUsd") as number | null });
}

export async function DELETE(_request: Request, context: RouteContext) {
  const { workflowId } = await context.params;
  return save(workflowId, { hourlyUsd: null, dailyUsd: null });
}
