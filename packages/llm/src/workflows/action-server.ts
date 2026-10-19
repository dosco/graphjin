import { createMcpServer, defineMcpTool } from "../mcp-server";
import {
  enqueue as defaultEnqueue,
  QUEUE,
} from "@neko/db/jobs";
import type { AgentEvent } from "../agent-backend";
import {
  inProcessControlPlane,
  type AgentControlPlane,
} from "../work/control-plane";
import {
  createActionRequest,
  listEnabledPolicies as defaultListEnabledPolicies,
  type ActionRequestRecord,
  type ActionScope,
  type RiskLevel,
} from "./action-store";
import {
  ACTION_REQUEST_SCHEMA,
  type ActionRequestPayload,
} from "./fence-schemas";
import {
  evaluateActionPolicy,
  type PolicyDecision,
} from "./policy-engine";
import { withActionTargets } from "./action-targets";
import { clampActionMinutes } from "./value";

export type WorkflowActionContext = {
  orgId: string;
  workflowRunId: string;
  workRunId: string;
  triggeredByObservationId?: string | null;
  emit: (event: AgentEvent) => Promise<void> | void;
  /** In-process on host; broker-backed inside the agent sandbox. */
  controlPlane?: AgentControlPlane;
  /** DI for tests. */
  listPolicies?: typeof defaultListEnabledPolicies;
  enqueue?: typeof defaultEnqueue;
};

const REQUEST_DESCRIPTION = [
  "Propose a state-changing operation. Workflows decide; actions mutate.",
  "Route every real-world or internal state change through this tool so",
  "policy can gate it — that's how the workflow stays auditable and how",
  "the operator can step in before something significant happens.",
  "",
  "Inputs:",
  "  scope: 'external' for outbound mutations (send_message,",
  "    mutate_record, open_pr, run_command, ...). 'internal' for",
  "    policy-governed writes inside OpenNeko itself (memory_write,",
  "    briefing_create, schedule_workflow). At scale these need policy",
  "    gating too, even though they look routine.",
  "  kind: short identifier of the operation. Free-form; the executor",
  "    registry routes by this string.",
  "  target: resource identifier (account id, slack channel, repo name,",
  "    memory id, etc.). Policies use this for allow/deny matching.",
  "  payload: full operation payload — exactly what the executor needs.",
  "  risk_level: internal tag policy uses to route. low | medium | high |",
  "    critical. Do NOT repeat this value in messages back to the",
  "    operator — it's noise from their point of view.",
  "  summary: one-sentence human-readable description for the approval",
  "    queue. Name WHAT will change and WHY — the operator may read it",
  "    before approving.",
  "",
  "The tool returns one of:",
  "  ok: true, status: 'queued_for_execution'   — auto-approved",
  "  ok: true, status: 'pending_approval'       — operator must approve",
  "  ok: false, reason: ...                      — denied by policy",
  "",
  "On a denial, surface the reason to the operator and stop. The denial",
  "is from policy and re-attempting won't change the answer.",
].join("\n");

export type HandleActionRequestResult =
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

/**
 * Shared handler. The MCP tool and the fence-fallback path both route
 * here so policy evaluation, the action_request row, the emit event,
 * and the queue enqueue all happen in exactly one place.
 */
export async function handleActionRequest(
  ctx: WorkflowActionContext,
  args: ActionRequestPayload,
): Promise<HandleActionRequestResult> {
  if (ctx.controlPlane) return handleActionRequestViaControlPlane(ctx, args);

  const listPolicies = ctx.listPolicies ?? defaultListEnabledPolicies;
  const enqueue = ctx.enqueue ?? defaultEnqueue;

  const policies = await listPolicies(ctx.orgId);
  const decision = evaluateActionPolicy(
    await withActionTargets(
      ctx.orgId,
      {
        scope: args.scope as ActionScope,
        kind: args.kind,
        target: args.target ?? null,
        riskLevel: (args.risk_level as RiskLevel | undefined) ?? null,
      },
      args.payload,
    ),
    policies,
  );

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

  const request = await createActionRequest({
    orgId: ctx.orgId,
    workflowRunId: ctx.workflowRunId,
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

  await emitActionRequestEvent(ctx, request, decision);

  if (request.status === "approved") {
    await enqueue(QUEUE.ACTION_EXECUTE, {
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

async function handleActionRequestViaControlPlane(
  ctx: WorkflowActionContext,
  args: ActionRequestPayload,
): Promise<HandleActionRequestResult> {
  const controlPlane = ctx.controlPlane ?? inProcessControlPlane;
  const decision = await controlPlane.evaluateActionPolicy({
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
  const request = await controlPlane.createActionRequest({
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
    await controlPlane.enqueueActionExecute({
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

/**
 * /work fence-emission context — the agent may emit `neko_action_request`
 * fences inline in its prose; the runtime parses them after the turn
 * ends and routes each through this handler. Mirrors
 * handleActionRequest but writes to the action_request row's
 * work_run_id column instead of workflow_run_id, and emits the
 * intent/summary fields the inline approval card in /work consumes.
 */
export type WorkActionContext = {
  orgId: string;
  workRunId: string;
  threadId: string;
  /** Defaults to "external" when the agent's fence omits it. */
  emit: (event: AgentEvent) => Promise<void> | void;
  listPolicies?: typeof defaultListEnabledPolicies;
  enqueue?: typeof defaultEnqueue;
};

export type HandleWorkActionRequestResult =
  | { ok: false; decision: "denied"; reason: string; policy?: string }
  | {
      ok: true;
      decision: "auto_approved" | "needs_approval";
      actionRequestId: string;
      policy: string;
      status: "queued_for_execution" | "pending_approval";
    };

export async function handleWorkActionRequest(
  ctx: WorkActionContext,
  args: ActionRequestPayload,
  agentIntent?: string,
): Promise<HandleWorkActionRequestResult> {
  const listPolicies = ctx.listPolicies ?? defaultListEnabledPolicies;
  const enqueue = ctx.enqueue ?? defaultEnqueue;

  const policies = await listPolicies(ctx.orgId);
  const decision = evaluateActionPolicy(
    await withActionTargets(
      ctx.orgId,
      {
        scope: args.scope as ActionScope,
        kind: args.kind,
        target: args.target ?? null,
        riskLevel: (args.risk_level as RiskLevel | undefined) ?? null,
      },
      args.payload,
    ),
    policies,
  );

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
        "no policy matches this scope/kind — refuse for safety. Operator must define a rule in /admin/rules.",
    };
  }

  const status =
    decision.decision === "allow" ? "approved" : "pending_approval";
  const intent =
    typeof agentIntent === "string" && agentIntent.length > 0
      ? agentIntent
      : args.summary;

  const request = await createActionRequest({
    orgId: ctx.orgId,
    workRunId: ctx.workRunId,
    policyId: decision.policy.id,
    scope: args.scope as ActionScope,
    kind: args.kind,
    target: args.target ?? null,
    payload: args.payload ?? {},
    riskLevel: (args.risk_level as RiskLevel | undefined) ?? null,
    summary: args.summary,
    intent: intent ?? null,
    minutesSaved: clampActionMinutes(args.minutes_saved),
    minutesSavedBasis: args.basis ?? null,
    status,
  });

  await ctx.emit({
    type: "action_request_emit",
    action_request_id: request.id,
    kind: request.kind,
    scope: request.scope,
    decision: request.status === "approved" ? "auto_approved" : "pending_approval",
    ...(intent ? { intent, summary: intent } : args.summary ? { summary: args.summary } : {}),
    ...(request.riskLevel ? { risk_level: request.riskLevel } : {}),
  });

  if (request.status === "approved") {
    await enqueue(QUEUE.ACTION_EXECUTE, {
      orgId: ctx.orgId,
      actionRequestId: request.id,
    });
    return {
      ok: true,
      decision: "auto_approved",
      actionRequestId: request.id,
      policy: decision.policy.name,
      status: "queued_for_execution",
    };
  }

  return {
    ok: true,
    decision: "needs_approval",
    actionRequestId: request.id,
    policy: decision.policy.name,
    status: "pending_approval",
  };
}

export function buildWorkflowActionServer(ctx: WorkflowActionContext) {
  const requestAction = defineMcpTool(
    "request",
    REQUEST_DESCRIPTION,
    ACTION_REQUEST_SCHEMA.shape,
    async (args) => jsonOk(await handleActionRequest(ctx, args)),
  );

  return createMcpServer({
    name: "neko_action",
    version: "1.0.0",
    tools: [requestAction],
  });
}

async function emitActionRequestEvent(
  ctx: WorkflowActionContext,
  request: ActionRequestRecord,
  decision: PolicyDecision,
): Promise<void> {
  if (decision.decision === "deny" || decision.decision === "no_policy") return;
  await ctx.emit({
    type: "action_request_emit",
    action_request_id: request.id,
    kind: request.kind,
    scope: request.scope,
    risk_level: request.riskLevel ?? undefined,
    decision: decision.decision === "allow" ? "auto_approved" : "pending_approval",
    ...(request.summary ? { summary: request.summary } : {}),
  });
}

function jsonOk(payload: Record<string, unknown>) {
  return {
    content: [
      {
        type: "text" as const,
        text: JSON.stringify(payload),
      },
    ],
  };
}
