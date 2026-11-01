import { NextResponse } from "next/server";
import { getOrgId } from "@/lib/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { setSsoScalekitIds } from "@/lib/sso-setup";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(request: Request) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const body = (await request.json().catch(() => null)) as Record<
    string,
    unknown
  > | null;
  const environmentId = body?.environmentId;
  const organizationId = body?.organizationId;
  const tier = body?.tier;
  if (typeof environmentId !== "string" || !environmentId) {
    return NextResponse.json(
      { error: "environmentId (string) is required" },
      { status: 400 },
    );
  }
  if (typeof organizationId !== "string" || !organizationId) {
    return NextResponse.json(
      { error: "organizationId (string) is required" },
      { status: 400 },
    );
  }
  try {
    const orgId = await getOrgId();
    await setSsoScalekitIds(orgId, {
      environmentId,
      organizationId,
      tier: typeof tier === "string" && tier ? tier : "dev",
    });
    return NextResponse.json({ ok: true });
  } catch (err) {
    return NextResponse.json(
      { error: err instanceof Error ? err.message : String(err) },
      { status: 500 },
    );
  }
}
