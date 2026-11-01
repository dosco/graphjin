/**
 * POST /api/work/threads/[threadId]/runs/[runId]/approve-action
 *
 * Approve or reject a pending action_request that the agent emitted
 * during a /work run. On approve we transition the row to "approved"
 * and enqueue the action_execute pg-boss job — the worker (which
 * holds the plugin VM runtime) actually fires the adapter. On reject
 * we transition to "rejected" and emit a terminal
 * action_request_result event so the chat UI can render the
 * outcome inline without waiting for the worker.
 *
 * The web process never executes the plugin adapter directly: only
 * the worker has the registry + sandboxes. Approval is a fast DB
 * update + a queue insert; the user sees "approved — running" until
 * the worker's job lands the action_request_result event in this
 * thread's run.
 *
 * Body: { actionRequestId: string, decision: "approve" | "reject",
 *         rejectionReason?: string }
 */

import { NextRequest, NextResponse } from "next/server";
import { enqueue, QUEUE } from "@neko/db/jobs";
import {
  approveActionRequest,
  getActionRequest,
  rejectActionRequest,
} from "@neko/llm/workflows";
import { appendWorkRunEvent } from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { getAuthorizedWorkThread } from "@/lib/work-thread-auth";

type RouteContext = {
  params: Promise<{ threadId: string; runId: string }>;
};

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(request: NextRequest, context: RouteContext) {
  const { threadId, runId } = await context.params;
  const body = await request.json().catch(() => ({}));
  const actionRequestId =
    typeof body.actionRequestId === "string" ? body.actionRequestId : "";
  const decision =
    body.decision === "approve" || body.decision === "reject"
      ? body.decision
      : null;
  const rejectionReason =
    typeof body.rejectionReason === "string" ? body.rejectionReason : null;

  if (!actionRequestId) {
    return NextResponse.json(
      { error: "actionRequestId is required" },
      { status: 400 },
    );
  }
  if (!decision) {
    return NextResponse.json(
      { error: "decision must be 'approve' or 'reject'" },
      { status: 400 },
    );
  }

  const orgId = await getOrgId();
  const actor = await getCurrentActor();
  const thread = await getAuthorizedWorkThread(orgId, threadId, actor);
  if (!thread) {
    return NextResponse.json({ error: "Thread not found" }, { status: 404 });
  }
  const req = await getActionRequest(orgId, actionRequestId);
  if (
    !req ||
    (req.kind === "source_config_admin" && actor.role !== "admin")
  ) {
    return NextResponse.json({ error: "action_request not found" }, { status: 404 });
  }
  if (req.workRunId !== runId) {
    // The request was emitted by a different run / non-/work source.
    // Reject the call rather than silently approving an unrelated row.
    return NextResponse.json(
      {
        error: "action_request does not belong to this run",
      },
      { status: 403 },
    );
  }
  if (req.status !== "pending_approval") {
    if (
      (decision === "approve" &&
        (req.status === "approved" ||
          req.status === "executed" ||
          req.status === "failed")) ||
      (decision === "reject" && req.status === "rejected")
    ) {
      return NextResponse.json({
        ok: true,
        decision:
          decision === "approve" ? "already_approved" : "already_rejected",
        action_request_id: req.id,
        status: req.status,
      });
    }
    return NextResponse.json(
      {
        error: `action_request status is "${req.status}"; only pending_approval can be approved or rejected`,
      },
      { status: 409 },
    );
  }

  const approverUserId = actor.userId;

  if (decision === "approve") {
    await approveActionRequest({
      approver: actor,
      id: req.id,
      orgId,
      approverUserId,
    });
    await enqueue(QUEUE.ACTION_EXECUTE, {
      orgId,
      actionRequestId: req.id,
    });
    return NextResponse.json({
      ok: true,
      decision: "approved",
      action_request_id: req.id,
      status: "queued_for_execution",
    });
  }

  // Reject: persist the rejection and emit a terminal result event so
  // the chat UI updates immediately — no worker round-trip needed.
  await rejectActionRequest({
    approver: actor,
    id: req.id,
    orgId,
    approverUserId,
    reason: rejectionReason ?? undefined,
  });
  await appendWorkRunEvent({
    orgId,
    threadId,
    runId,
    event: {
      type: "action_request_result",
      action_request_id: req.id,
      kind: req.kind,
      status: "rejected",
      ...(rejectionReason ? { rejection_reason: rejectionReason } : {}),
    },
  });
  return NextResponse.json({
    ok: true,
    decision: "rejected",
    action_request_id: req.id,
  });
}
