import { NextRequest, NextResponse } from "next/server";
import {
  WORK_MEMORY_SCOPES,
  acceptPendingWorkMemory,
  declinePendingWorkMemory,
  getPendingWorkMemory,
  type WorkMemoryScope,
} from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import {
  getAuthorizedWorkRun,
  getAuthorizedWorkThread,
} from "@/lib/work-thread-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(request: NextRequest) {
  const body = await request.json().catch(() => ({}));
  const id = typeof body.id === "string" ? body.id : "";
  const action = body.action;
  if (!id) {
    return NextResponse.json({ error: "id is required" }, { status: 400 });
  }

  const orgId = await getOrgId();
  const actor = await getCurrentActor();
  const pending = await getPendingWorkMemory(orgId, id);
  const authorized = pending?.threadId
    ? await getAuthorizedWorkThread(orgId, pending.threadId, actor)
    : pending?.runId
      ? await getAuthorizedWorkRun(orgId, pending.runId, actor)
      : null;
  if (!pending || !authorized) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  try {
    if (action === "accept") {
      const scope =
        typeof body.scope === "string" &&
        WORK_MEMORY_SCOPES.includes(body.scope as WorkMemoryScope)
          ? (body.scope as WorkMemoryScope)
          : undefined;
      const result = await acceptPendingWorkMemory({
        orgId,
        id,
        text: typeof body.text === "string" ? body.text : undefined,
        scope,
        scopeId: typeof body.scopeId === "string" ? body.scopeId : undefined,
        pinned: typeof body.pinned === "boolean" ? body.pinned : undefined,
      });
      return NextResponse.json({ ok: true, ...result });
    }

    if (action === "decline") {
      const reason = typeof body.reason === "string" ? body.reason : undefined;
      const pending = await declinePendingWorkMemory(orgId, id, reason);
      return NextResponse.json({ ok: true, pending });
    }

    return NextResponse.json(
      { error: "action must be accept or decline" },
      { status: 400 },
    );
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return NextResponse.json({ error: message }, { status: 400 });
  }
}
