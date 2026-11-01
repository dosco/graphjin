import { NextResponse } from "next/server";
import { ITEM_TYPES, isItemType } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { ITEM_TYPE_LABELS, listItemOptions } from "@/lib/groups-admin";

export async function GET(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const type = new URL(request.url).searchParams.get("type");
  if (!type) return NextResponse.json({ types: ITEM_TYPES.map((t) => ({ type: t, ...ITEM_TYPE_LABELS[t] })) });
  if (!isItemType(type)) return NextResponse.json({ error: "unknown item type" }, { status: 400 });
  try {
    return NextResponse.json({ items: await listItemOptions(await getOrgId(), type) });
  } catch (error) {
    return NextResponse.json({ error: error instanceof Error ? error.message : String(error) }, { status: 500 });
  }
}
