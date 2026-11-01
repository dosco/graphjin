import { NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { sourceTables } from "@/lib/groups-admin";

export async function GET(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const source = new URL(request.url).searchParams.get("source");
  if (!source) return NextResponse.json({ error: "source is required" }, { status: 400 });
  return NextResponse.json({ tables: await sourceTables(await getOrgId(), source) });
}
