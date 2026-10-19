import { NextRequest, NextResponse } from "next/server";
import { provisionHostConfig } from "@neko/llm";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import {
  getAgentSettingsPayload,
  saveAgentSettingsDraft,
} from "@/lib/agent-backend-settings";

export async function GET() {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const payload = await getAgentSettingsPayload((await getOrgId()));
  return NextResponse.json(payload);
}

export async function PUT(request: NextRequest) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  try {
    const body = await request.json();
    if (!body || typeof body !== "object" || Array.isArray(body)) {
      throw new Error("Agent settings payload must be an object");
    }
    const unsupported = Object.keys(body).filter((key) => key !== "globalCap");
    if (unsupported.length > 0) {
      throw new Error(`Unsupported agent setting: ${unsupported.join(", ")}`);
    }
    const saved = await saveAgentSettingsDraft((await getOrgId()), {
      globalCap: body.globalCap,
    });
    // Reprovision so the agent sandbox uses the latest provider settings.
    await provisionHostConfig((await getOrgId()));
    return NextResponse.json(saved);
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    return NextResponse.json({ error: message }, { status: 400 });
  }
}
