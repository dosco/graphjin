import { NextResponse } from "next/server";
import { getOrgId } from "@/lib/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getSsoSetupStatus } from "@/lib/sso-setup";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  try {
    const orgId = await getOrgId();
    return NextResponse.json(await getSsoSetupStatus(orgId));
  } catch (err) {
    return NextResponse.json(
      { error: err instanceof Error ? err.message : String(err) },
      { status: 500 },
    );
  }
}
