import type { ActionExecutePayload } from "@neko/db/jobs";
import { executeApprovedActionRequest } from "@neko/llm/workflows";
import { getActionRequest } from "@neko/llm/workflows";
import { appendWorkRunEvent, getWorkThreadForRun } from "@neko/llm/work";

export async function runActionExecute(
  payload: ActionExecutePayload,
): Promise<void> {
  const result = await executeApprovedActionRequest(
    payload.orgId,
    payload.actionRequestId,
  );
  if (!result.ok) {
    console.warn(
      `[action-execute] action_request=${payload.actionRequestId} status=${result.outcome?.status ?? "failed"}: ${result.error ?? "see execution result"}`,
    );
  }

  // If this action_request originated from a /work tool call, push a
  // terminal action_request_result event into the run so the chat UI
  // renders the outcome inline. Workflow-runner-emitted requests
  // (work_run_id is null) skip this — their results land in /actions.
  const request = await getActionRequest(payload.orgId, payload.actionRequestId);
  if (!request?.workRunId) return;
  const thread = await getWorkThreadForRun(payload.orgId, request.workRunId);
  if (!thread) return;
  await appendWorkRunEvent({
    orgId: payload.orgId,
    threadId: thread.id,
    runId: request.workRunId,
    event: {
      type: "action_request_result",
      action_request_id: request.id,
      kind: request.kind,
      status: result.outcome?.status ?? (result.ok ? "succeeded" : "failed"),
      ...(result.outcome ? { outcome: result.outcome } : {}),
      ...(result.error ? { error: result.error } : {}),
    },
  });
}
