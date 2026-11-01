import type { AgentEvent } from "../agent-backend";
import { createMcpServer, defineMcpTool } from "../mcp-server";
import type { AgentControlPlane } from "../work/control-plane";
import type { ActionScope, RiskLevel } from "./action-store";
import {
  ACTION_REQUEST_SCHEMA,
  type ActionRequestPayload,
} from "./fence-schemas";
import { clampActionMinutes } from "./value";

export type WorkflowActionToolContext = {
  orgId: string;
  workflowRunId: string;
  workRunId: string;
  triggeredByObservationId?: string | null;
  emit: (event: AgentEvent) => Promise<void> | void;
  controlPlane: AgentControlPlane;
};

export type WorkflowActionToolResult =
  | {
      ok: true;
      decision: "auto_approved" | "needs_approval";
      status: "queued_for_execution" | "pending_approval";
      actionRequestId: string;
      policy: string;
      reason?: string;
    }
  | {
      ok: false;
      decision: "denied";
      reason: string;
      policy?: string;
    };

const REQUEST_DESCRIPTION = [
  "Propose a state-changing operation. Workflows decide; actions mutate.",
  "Route every real-world or internal state change through this tool so",
  "policy can gate it and preserve an auditable approval path.",
  "Use scope 'external' for outbound mutations and 'internal' for",
  "policy-governed OpenNeko writes. Include the complete operation payload,",
  "a concise human-readable summary, and an honest risk estimate.",
  "A denial is final for this run; surface its reason instead of retrying.",
].join(" ");

export async function handleWorkflowActionViaControlPlane(
  ctx: WorkflowActionToolContext,
  args: ActionRequestPayload,
): Promise<WorkflowActionToolResult> {
  const decision = await ctx.controlPlane.evaluateActionPolicy({
    orgId: ctx.orgId,
    scope: args.scope as ActionScope,
    kind: args.kind,
    target: args.target ?? null,
    payload: args.payload ?? null,
    riskLevel: (args.risk_level as RiskLevel | undefined) ?? null,
  });

  if (decision.decision === "deny") {
    return {
      ok: false,
      decision: "denied",
      reason: decision.reason,
      policy: decision.policy.name,
    };
  }
  if (decision.decision === "no_policy") {
    return {
      ok: false,
      decision: "denied",
      reason:
        "no policy matches this scope/kind — refuse for safety. Ask the operator to define a policy first.",
    };
  }

  const status =
    decision.decision === "allow" ? "approved" : "pending_approval";
  const request = await ctx.controlPlane.createActionRequest({
    orgId: ctx.orgId,
    workflowRunId: ctx.workflowRunId,
    workRunId: ctx.workRunId,
    requestedByRunId: ctx.workflowRunId,
    triggeredByObservationId: ctx.triggeredByObservationId ?? null,
    policyId: decision.policy.id,
    scope: args.scope as ActionScope,
    kind: args.kind,
    target: args.target ?? null,
    payload: args.payload ?? {},
    riskLevel: (args.risk_level as RiskLevel | undefined) ?? null,
    summary: args.summary,
    minutesSaved: clampActionMinutes(args.minutes_saved),
    minutesSavedBasis: args.basis ?? null,
    status,
  });

  await ctx.emit({
    type: "action_request_emit",
    action_request_id: request.id,
    kind: args.kind,
    scope: args.scope,
    risk_level: args.risk_level,
    decision: request.status === "approved" ? "auto_approved" : "pending_approval",
    ...(args.summary ? { summary: args.summary } : {}),
  });

  if (request.status === "approved") {
    await ctx.controlPlane.enqueueActionExecute({
      orgId: ctx.orgId,
      actionRequestId: request.id,
    });
    return {
      ok: true,
      decision: "auto_approved",
      status: "queued_for_execution",
      actionRequestId: request.id,
      policy: decision.policy.name,
    };
  }

  return {
    ok: true,
    decision: "needs_approval",
    status: "pending_approval",
    actionRequestId: request.id,
    policy: decision.policy.name,
    reason: decision.reason,
  };
}

export function buildWorkflowActionServer(ctx: WorkflowActionToolContext) {
  const requestAction = defineMcpTool(
    "request",
    REQUEST_DESCRIPTION,
    ACTION_REQUEST_SCHEMA.shape,
    async (args) => ({
      content: [
        {
          type: "text" as const,
          text: JSON.stringify(
            await handleWorkflowActionViaControlPlane(ctx, args),
          ),
        },
      ],
    }),
  );

  return createMcpServer({
    name: "neko_action",
    version: "1.0.0",
    tools: [requestAction],
  });
}
