import { NextRequest, NextResponse } from "next/server";
import { insertConfigChangeRow, restoreConfigPath } from "@neko/llm/config-vcs";
import { getOrgAgentRoot } from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// CV4 "Restore this version": forward-only restore of one artifact path
// to its state at a previous commit. Admin-only — the working tree is
// the team layer.
export async function POST(request: NextRequest) {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") {
    return NextResponse.json({ error: "admin only" }, { status: 403 });
  }
  const body = await request.json().catch(() => ({}));
  const sha = typeof body.sha === "string" ? body.sha : "";
  const path = typeof body.path === "string" ? body.path : "";
  if (!/^[0-9a-f]{7,40}$/i.test(sha) || !path || path.includes("..")) {
    return NextResponse.json({ error: "provide sha and path" }, { status: 400 });
  }
  const orgId = await getOrgId();
  const newSha = await restoreConfigPath({
    workspaceRoot: getOrgAgentRoot(orgId),
    sha,
    path,
  });
  if (!newSha) {
    return NextResponse.json(
      { error: "nothing to restore (unknown path or already at that version)" },
      { status: 404 },
    );
  }
  await insertConfigChangeRow({
    orgId,
    artifactKind: "config",
    artifactRef: path,
    actorUserId: actor.userId,
    commitSha: newSha,
    summary: `Restored ${path} to ${sha.slice(0, 8)}`,
    status: "restored",
  });
  return NextResponse.json({ ok: true, sha: newSha });
}
