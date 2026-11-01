import { NextResponse } from "next/server";
import { abortRun } from "@/lib/neko-run-registry";
import { getOrgId } from "@/lib/db";
import {
  appendWorkRunEvent,
  cancelWorkRunIfActive,
} from "@/lib/work-store";
import { getAuthorizedWorkRun } from "@/lib/work-thread-auth";

type RouteContext = {
  params: Promise<{ runId: string }>;
};

const INTERRUPTED_RUN_REASON =
  "Interrupted — the process running the agent is no longer available. Retry to run it again.";

export async function POST(_: Request, context: RouteContext) {
  const { runId } = await context.params;
  const run = await getAuthorizedWorkRun(await getOrgId(), runId);
  if (!run) {
    return NextResponse.json({ error: "Run not found" }, { status: 404 });
  }
  const aborted = abortRun(runId);
  if (aborted) {
    return NextResponse.json({ ok: true, recovered: false });
  }

  // In-process controllers disappear on a hard restart. The durable run row
  // survives, so Stop must still be able to recover it instead of returning a
  // misleading success response while the UI remains stuck indefinitely.
  const recovered = await cancelWorkRunIfActive(runId, INTERRUPTED_RUN_REASON);
  if (recovered) {
    await appendWorkRunEvent({
      orgId: run.org_id,
      threadId: run.thread_id,
      runId,
      event: { type: "done", result: { status: "cancelled" } },
    });
  }

  const alreadyTerminal = [
    "completed",
    "failed",
    "cancelled",
    "needs_input",
  ].includes(run.status);
  return NextResponse.json({
    ok: recovered || alreadyTerminal,
    recovered,
  });
}
