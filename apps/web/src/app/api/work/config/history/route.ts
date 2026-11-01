import { NextRequest, NextResponse } from "next/server";
import { listConfigHistory } from "@neko/llm/config-vcs";
import { getOrgAgentRoot } from "@neko/llm/work";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// CV4 "Version history": plain-English commit list for the org config
// repo, optionally scoped to one artifact path (skills/…, workflows/…,
// memory/…).
export async function GET(request: NextRequest) {
  const url = new URL(request.url);
  const path = url.searchParams.get("path") ?? undefined;
  const limit = Math.min(Number(url.searchParams.get("limit")) || 50, 200);
  const orgId = await getOrgId();
  const history = await listConfigHistory(getOrgAgentRoot(orgId), path, limit);
  return NextResponse.json({ history });
}
