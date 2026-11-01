import { NextResponse } from "next/server";
import { getSpendSettings, saveSpendLimits, SpendSettingsError, type SpendLimitsUsd } from "@neko/llm/spend";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { readJsonBody } from "@/lib/groups-admin";

export async function GET() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  return NextResponse.json(await getSpendSettings(await getOrgId()));
}

export async function PUT(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await readJsonBody(request);
  if (!body) return NextResponse.json({ error: "A JSON body is required." }, { status: 400 });
  try {
    return NextResponse.json(await saveSpendLimits(await getOrgId(), actor.userId, body as Partial<SpendLimitsUsd>));
  } catch (error) {
    if (error instanceof SpendSettingsError) {
      return NextResponse.json({ error: error.message }, { status: 400 });
    }
    throw error;
  }
}
