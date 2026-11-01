import { NextResponse } from "next/server";
import { createUserGroup, listUserGroups } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { groupErrorResponse, readJsonBody } from "@/lib/groups-admin";

export async function GET() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  return NextResponse.json({ groups: await listUserGroups(await getOrgId()) });
}

export async function POST(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await readJsonBody(request);
  if (!body || typeof body.name !== "string") return NextResponse.json({ error: "name is required" }, { status: 400 });
  try {
    const group = await createUserGroup(await getOrgId(), {
      name: body.name,
      description: typeof body.description === "string" ? body.description : null,
    });
    return NextResponse.json({ group }, { status: 201 });
  } catch (error) {
    return groupErrorResponse(error);
  }
}
