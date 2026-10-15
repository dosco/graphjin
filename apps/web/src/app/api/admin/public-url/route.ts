import { NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { getPublicUrlSetting, normalizePublicUrl, savePublicUrl } from "@/lib/public-url-settings";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  return NextResponse.json(await getPublicUrlSetting(await getOrgId()));
}

export async function PUT(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await request.json().catch(() => null);
  const input = typeof body?.url === "string" ? body.url.trim() : "";
  const url = input ? normalizePublicUrl(input) : null;
  if (input && !url) {
    return NextResponse.json(
      { error: "Enter an http or https address without a query, such as https://openneko.example.com." },
      { status: 400 },
    );
  }
  const orgId = await getOrgId();
  await savePublicUrl(orgId, url);
  return NextResponse.json(await getPublicUrlSetting(orgId));
}
