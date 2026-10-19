import { NextRequest, NextResponse } from "next/server";
import { provisionHostConfig } from "@neko/llm";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import {
  getProviderSettingsPayload,
  saveProviderDraft,
} from "@/lib/provider-settings";

export async function GET() {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const payload = await getProviderSettingsPayload((await getOrgId()));
  return NextResponse.json(payload);
}

export async function PUT(request: NextRequest) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  try {
    const body = await request.json();
    const saved = await saveProviderDraft((await getOrgId()), body);
    // Saving the primary provider re-provisions the agent settings so the
    // next agent run picks them up without a restart.
    if (body?.scope === "primary") {
      await provisionHostConfig((await getOrgId()));
    }
    return NextResponse.json(saved);
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    return NextResponse.json({ error: message }, { status: 400 });
  }
}
