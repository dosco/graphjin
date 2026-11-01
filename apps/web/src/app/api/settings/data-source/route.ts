import { NextRequest, NextResponse } from "next/server";
import { provisionHostConfig } from "@neko/llm";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import {
  getDataSourceSettings,
  saveDataSourceDraft,
} from "@/lib/data-source-settings";

export async function GET() {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const payload = await getDataSourceSettings((await getOrgId()));
  return NextResponse.json(payload);
}

export async function PUT(request: NextRequest) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  try {
    const body = await request.json();
    const saved = await saveDataSourceDraft((await getOrgId()), {
      graphqlUrl: String(body.graphqlUrl ?? ""),
      mcpUrl: body.mcpUrl == null ? null : String(body.mcpUrl),
      label: body.label == null ? null : String(body.label),
    });
    // Re-provision host config so the next agent run picks up the new
    // GraphJin endpoint without a worker restart.
    await provisionHostConfig((await getOrgId()));
    return NextResponse.json(saved);
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    return NextResponse.json({ error: message }, { status: 400 });
  }
}
