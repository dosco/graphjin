import { NextResponse } from "next/server";
import {
  deleteUserGroup,
  getUserGroup,
  listDataAccessRules,
  listGroupItemGrants,
  listGroupMembers,
  listIdpGroupRules,
  updateUserGroup,
} from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { groupErrorResponse, readJsonBody, scheduleGroupGrantsApply } from "@/lib/groups-admin";

type Context = { params: Promise<{ groupId: string }> };

export async function GET(_request: Request, context: Context) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const { groupId } = await context.params;
  const orgId = await getOrgId();
  const group = await getUserGroup(orgId, groupId).catch(() => null);
  if (!group) return NextResponse.json({ error: "group not found" }, { status: 404 });
  const [members, grants, rules, idpRules] = await Promise.all([
    listGroupMembers(orgId, groupId),
    listGroupItemGrants(orgId, groupId),
    listDataAccessRules(orgId, groupId),
    listIdpGroupRules(orgId),
  ]);
  return NextResponse.json({ group, members, grants, rules, idpRules: idpRules.filter((r) => r.userGroupId === groupId) });
}

export async function PATCH(request: Request, context: Context) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const { groupId } = await context.params;
  const body = await readJsonBody(request);
  if (!body) return NextResponse.json({ error: "invalid JSON body" }, { status: 400 });
  try {
    const group = await updateUserGroup(await getOrgId(), groupId, {
      ...(typeof body.name === "string" ? { name: body.name } : {}),
      ...(typeof body.description === "string" || body.description === null ? { description: body.description as string | null } : {}),
    });
    return NextResponse.json({ group });
  } catch (error) {
    return groupErrorResponse(error);
  }
}

export async function DELETE(_request: Request, context: Context) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const { groupId } = await context.params;
  try {
    await deleteUserGroup(await getOrgId(), groupId);
    await scheduleGroupGrantsApply();
    return NextResponse.json({ ok: true });
  } catch (error) {
    return groupErrorResponse(error);
  }
}
