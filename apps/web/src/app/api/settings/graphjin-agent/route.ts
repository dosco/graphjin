import { NextRequest, NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import {
  getGraphjinAgentSettingsPayload,
  saveGraphjinAgentSettings,
} from "@/lib/graphjin-agent-settings";

export async function GET() {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  return NextResponse.json(await getGraphjinAgentSettingsPayload(await getOrgId()));
}

export async function PUT(request: NextRequest) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  try {
    const body = await request.json();
    if (!body || typeof body !== "object" || Array.isArray(body)) {
      throw new Error("GraphJin agent settings payload must be an object");
    }
    return NextResponse.json(await saveGraphjinAgentSettings(await getOrgId(), body));
  } catch (e) {
    return NextResponse.json({ error: e instanceof Error ? e.message : String(e) }, { status: 400 });
  }
}
