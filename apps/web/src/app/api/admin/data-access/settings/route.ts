import { NextResponse } from "next/server";
import { getGroupGrantsEnabled } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { readJsonBody, requestWorker } from "@/lib/groups-admin";

export async function GET() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  return NextResponse.json({ enabled: await getGroupGrantsEnabled(await getOrgId()) });
}

export async function PATCH(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await readJsonBody(request);
  if (!body || typeof body.enabled !== "boolean") return NextResponse.json({ error: "enabled must be true or false" }, { status: 400 });
  const result = await requestWorker(`/admin/graphjin/group-grants/${body.enabled ? "enable" : "disable"}`, { actorUserId: actor.userId });
  return NextResponse.json(result.body, { status: result.status });
}
