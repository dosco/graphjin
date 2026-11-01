import { NextResponse } from "next/server";
import { deleteDataAccessRule } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { scheduleGroupGrantsApply } from "@/lib/groups-admin";

export async function DELETE(_request: Request, context: { params: Promise<{ ruleId: string }> }) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const { ruleId } = await context.params;
  const removed = await deleteDataAccessRule(await getOrgId(), ruleId);
  if (!removed) return NextResponse.json({ error: "rule not found" }, { status: 404 });
  await scheduleGroupGrantsApply();
  return NextResponse.json({ ok: true });
}
