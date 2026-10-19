import { NextRequest, NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import {
  getInstallPolicyPayload,
  saveInstallPolicyDraft,
  type InstallPolicyDraft,
} from "@/lib/install-policy-settings";

export async function GET() {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const orgId = await getOrgId();
  const payload = await getInstallPolicyPayload(orgId);
  return NextResponse.json(payload);
}

export async function PATCH(request: NextRequest) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  try {
    const body = (await request.json()) as InstallPolicyDraft;
    const orgId = await getOrgId();
    const saved = await saveInstallPolicyDraft(orgId, {
      allowGitUrlInstalls:
        typeof body.allowGitUrlInstalls === "boolean" ? body.allowGitUrlInstalls : undefined,
    });
    return NextResponse.json({ policy: saved, source: "org" });
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    return NextResponse.json({ error: message }, { status: 400 });
  }
}
