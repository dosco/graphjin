import { NextResponse } from "next/server";
import { acknowledgeSpendAlert, getSpendSettings } from "@neko/llm/spend";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";

type RouteContext = { params: Promise<{ alertId: string }> };

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export async function DELETE(_request: Request, context: RouteContext) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const { alertId } = await context.params;
  const orgId = await getOrgId();
  if (!UUID.test(alertId) || !(await acknowledgeSpendAlert(orgId, alertId, actor.userId))) {
    return NextResponse.json({ error: "Alert not found or already acknowledged." }, { status: 404 });
  }
  return NextResponse.json(await getSpendSettings(orgId));
}
