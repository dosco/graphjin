import { NextRequest, NextResponse } from "next/server";
import { listPendingWorkMemories, listWorkThreads } from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import {
  getAuthorizedWorkRun,
  getAuthorizedWorkThread,
} from "@/lib/work-thread-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  const url = new URL(request.url);
  const threadId = url.searchParams.get("threadId");
  const runId = url.searchParams.get("runId");
  const orgId = await getOrgId();
  const actor = await getCurrentActor();
  if (threadId && !(await getAuthorizedWorkThread(orgId, threadId, actor))) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  if (runId && !(await getAuthorizedWorkRun(orgId, runId, actor))) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  const pending = await listPendingWorkMemories({
    orgId,
    threadId,
    runId,
    status: "proposed",
  });
  if (threadId || runId) return NextResponse.json({ pending });

  const ownedThreadIds = new Set(
    (await listWorkThreads(orgId, "web", actor.userId)).map((thread) => thread.id),
  );
  return NextResponse.json({
    pending: pending.filter(
      (item) => item.threadId && ownedThreadIds.has(item.threadId),
    ),
  });
}
