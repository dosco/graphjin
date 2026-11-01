import { NextResponse } from "next/server";
import { createIdpGroupRule, listIdpGroupRules, listIdpGroups, listUserGroups } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { groupErrorResponse, readJsonBody } from "@/lib/groups-admin";

export async function GET() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const orgId = await getOrgId();
  const [rules, idpGroups, groups] = await Promise.all([listIdpGroupRules(orgId), listIdpGroups(orgId), listUserGroups(orgId)]);
  return NextResponse.json({ rules, idpGroups, groups });
}

export async function POST(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await readJsonBody(request);
  if (!body || typeof body.ssoGroupId !== "string" || typeof body.userGroupId !== "string") {
    return NextResponse.json({ error: "ssoGroupId and userGroupId are required" }, { status: 400 });
  }
  try {
    const rule = await createIdpGroupRule(await getOrgId(), {
      ssoGroupId: body.ssoGroupId,
      userGroupId: body.userGroupId,
      createdByUserId: actor.userId,
    });
    return NextResponse.json({ rule }, { status: 201 });
  } catch (error) {
    return groupErrorResponse(error);
  }
}
