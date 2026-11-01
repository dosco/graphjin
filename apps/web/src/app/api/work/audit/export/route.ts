import { NextRequest, NextResponse } from "next/server";
import { exportAuditChain, verifyAuditChain } from "@neko/llm/workflows";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// SEC10 SIEM export: NDJSON of the tamper-evident audit chain, with the
// chain verification result in headers so the importer can refuse a
// broken chain. Admin only.
export async function GET(request: NextRequest) {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") {
    return NextResponse.json({ error: "admin only" }, { status: 403 });
  }
  const url = new URL(request.url);
  const sinceSeq = Math.max(0, Number(url.searchParams.get("since")) || 0);
  const orgId = await getOrgId();
  const verification = await verifyAuditChain(orgId);
  const body = await exportAuditChain(orgId, sinceSeq);
  return new NextResponse(body, {
    status: 200,
    headers: {
      "content-type": "application/x-ndjson",
      "x-audit-chain-ok": String(verification.ok),
      "x-audit-chain-length": String(verification.length),
      ...(verification.brokenAtSeq
        ? { "x-audit-chain-broken-at": String(verification.brokenAtSeq) }
        : {}),
    },
  });
}
