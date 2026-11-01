import { NextResponse } from "next/server";
import { deleteIdpGroupRule } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { groupErrorResponse } from "@/lib/groups-admin";

export async function DELETE(_request: Request, context: { params: Promise<{ ruleId: string }> }) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const { ruleId } = await context.params;
  try {
    await deleteIdpGroupRule(await getOrgId(), ruleId);
    return NextResponse.json({ ok: true });
  } catch (error) {
    return groupErrorResponse(error);
  }
}
