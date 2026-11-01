import { NextResponse } from "next/server";
import { addLocalGroupMember, removeLocalGroupMember } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { groupErrorResponse, readJsonBody } from "@/lib/groups-admin";

type Context = { params: Promise<{ groupId: string }> };

async function memberRequest(request: Request, context: Context, add: boolean) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const { groupId } = await context.params;
  const body = await readJsonBody(request);
  if (!body || typeof body.userId !== "string") return NextResponse.json({ error: "userId is required" }, { status: 400 });
  try {
    const orgId = await getOrgId();
    if (add) {
      await addLocalGroupMember(orgId, groupId, body.userId);
      return NextResponse.json({ ok: true });
    }
    return NextResponse.json(await removeLocalGroupMember(orgId, groupId, body.userId));
  } catch (error) {
    return groupErrorResponse(error);
  }
}

export async function POST(request: Request, context: Context) {
  return memberRequest(request, context, true);
}

export async function DELETE(request: Request, context: Context) {
  return memberRequest(request, context, false);
}
