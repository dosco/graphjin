import { NextResponse } from "next/server";
import { getSemanticSearchSetting, saveSemanticSearchSetting } from "@neko/llm/semantic-search";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** The deployment-wide "Semantic search" switch. Admins only. */
export async function GET() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  return NextResponse.json(await getSemanticSearchSetting());
}

export async function PUT(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await request.json().catch(() => null);
  if (typeof body?.enabled !== "boolean") {
    return NextResponse.json({ error: "enabled must be true or false." }, { status: 400 });
  }
  return NextResponse.json(await saveSemanticSearchSetting(await getOrgId(), body.enabled));
}
