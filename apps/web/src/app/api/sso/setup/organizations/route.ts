import { NextRequest, NextResponse } from "next/server";
import { getOrgId } from "@/lib/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { listSsoOrganizations } from "@/lib/sso-setup";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const environmentId = request.nextUrl.searchParams.get("environmentId") ?? "";
  if (!environmentId) {
    return NextResponse.json(
      { error: "environmentId query parameter is required" },
      { status: 400 },
    );
  }
  try {
    const orgId = await getOrgId();
    return NextResponse.json({
      organizations: await listSsoOrganizations(orgId, environmentId),
    });
  } catch (err) {
    return NextResponse.json(
      { error: err instanceof Error ? err.message : String(err) },
      { status: 500 },
    );
  }
}
