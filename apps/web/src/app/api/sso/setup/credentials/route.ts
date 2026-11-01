import { NextResponse } from "next/server";
import { getOrgId } from "@/lib/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { setSsoSignInCredentials } from "@/lib/sso-setup";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(request: Request) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const body = (await request.json().catch(() => null)) as Record<
    string,
    unknown
  > | null;
  const environmentUrl = body?.environmentUrl;
  const clientId = body?.clientId;
  const clientSecret = body?.clientSecret;
  if (typeof environmentUrl !== "string" || !environmentUrl) {
    return NextResponse.json(
      { error: "environmentUrl (string) is required" },
      { status: 400 },
    );
  }
  if (typeof clientId !== "string" || !clientId) {
    return NextResponse.json(
      { error: "clientId (string) is required" },
      { status: 400 },
    );
  }
  if (typeof clientSecret !== "string" || !clientSecret) {
    return NextResponse.json(
      { error: "clientSecret (string) is required" },
      { status: 400 },
    );
  }
  try {
    const orgId = await getOrgId();
    await setSsoSignInCredentials(orgId, {
      environmentUrl,
      clientId,
      clientSecret,
    });
    return NextResponse.json({ ok: true });
  } catch (err) {
    return NextResponse.json(
      { error: err instanceof Error ? err.message : String(err) },
      { status: 500 },
    );
  }
}
