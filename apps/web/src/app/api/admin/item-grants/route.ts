import { NextResponse } from "next/server";
import { grantItem, grantItems, isItemType, revokeItem, whoHolds } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { groupErrorResponse, readJsonBody, scheduleGroupGrantsApply } from "@/lib/groups-admin";

const GRAPHJIN_TYPES = new Set(["data_source", "api_operation"]);

export async function GET(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const url = new URL(request.url);
  const itemType = url.searchParams.get("itemType");
  const itemId = url.searchParams.get("itemId");
  if (!isItemType(itemType) || !itemId) return NextResponse.json({ error: "itemType and itemId are required" }, { status: 400 });
  return NextResponse.json({ holders: await whoHolds(await getOrgId(), itemType, itemId) });
}

async function change(request: Request, grant: boolean) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await readJsonBody(request);
  if (!body || typeof body.groupId !== "string" || !isItemType(body.itemType) || (
    grant ? typeof body.itemId !== "string" && !Array.isArray(body.itemIds) : typeof body.itemId !== "string"
  )) {
    return NextResponse.json({ error: "groupId, itemType and itemId or itemIds are required" }, { status: 400 });
  }
  try {
    const orgId = await getOrgId();
    const input = { groupId: body.groupId, itemType: body.itemType, actorUserId: actor.userId };
    const result = grant
      ? Array.isArray(body.itemIds)
        ? await grantItems(orgId, { ...input, itemIds: body.itemIds })
        : await grantItem(orgId, { ...input, itemId: body.itemId as string })
      : await revokeItem(orgId, { ...input, itemId: body.itemId as string });
    if (GRAPHJIN_TYPES.has(body.itemType)) await scheduleGroupGrantsApply();
    return NextResponse.json(result);
  } catch (error) {
    return groupErrorResponse(error);
  }
}

export async function POST(request: Request) {
  return change(request, true);
}

export async function DELETE(request: Request) {
  return change(request, false);
}
