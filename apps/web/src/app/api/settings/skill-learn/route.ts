import { NextRequest, NextResponse } from "next/server";
import {
  getSkillLearnOrgSettings,
  setSkillLearnOrgEnabled,
} from "@neko/llm/work";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";

export async function GET() {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const payload = await getSkillLearnOrgSettings(await getOrgId());
  return NextResponse.json(payload);
}

export async function PATCH(request: NextRequest) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  try {
    const body = await request.json();
    if (!body || typeof body !== "object" || Array.isArray(body)) {
      throw new Error("Skill learning payload must be an object");
    }
    const record = body as Record<string, unknown>;
    if (typeof record.enabled !== "boolean") {
      throw new Error("enabled must be a boolean");
    }
    const unsupported = Object.keys(record).filter((key) => key !== "enabled");
    if (unsupported.length > 0) {
      throw new Error(`Unsupported skill learning setting: ${unsupported.join(", ")}`);
    }
    const saved = await setSkillLearnOrgEnabled({
      orgId: await getOrgId(),
      enabled: record.enabled,
    });
    return NextResponse.json(saved);
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    return NextResponse.json({ error: message }, { status: 400 });
  }
}
