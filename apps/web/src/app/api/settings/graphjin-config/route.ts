import { NextRequest, NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import {
  getGraphjinConfigSettingsPayload,
  saveGraphjinConfigSettingsDraft,
} from "@/lib/graphjin-config-settings";

export async function GET() {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;

  const orgId = await getOrgId();
  return NextResponse.json(await getGraphjinConfigSettingsPayload(orgId));
}

export async function PATCH(request: NextRequest) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;

  try {
    const body = (await request.json()) as { sourceConfigEnabled?: unknown };
    const orgId = await getOrgId();
    const saved = await saveGraphjinConfigSettingsDraft(orgId, {
      sourceConfigEnabled:
        typeof body.sourceConfigEnabled === "boolean"
          ? body.sourceConfigEnabled
          : undefined,
    });
    return NextResponse.json({ settings: saved, source: "org" });
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    return NextResponse.json({ error: message }, { status: 400 });
  }
}
