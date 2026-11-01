import { NextResponse } from "next/server";
import {
  action_execution,
  action_policy,
  action_request,
  and,
  db,
  desc,
  eq,
  gte,
  observation,
  workflow_definition,
  workflow_run,
} from "@neko/db";
import { getOrgId } from "@/lib/db";
import { watcherRunVisibility, workflowIdVisibility } from "@/lib/entitlements";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const RECENT_WINDOW_HOURS = 24;
const LIMIT = 8;

export async function GET() {
  const orgId = await getOrgId();
  const since = new Date(Date.now() - RECENT_WINDOW_HOURS * 3600 * 1000);

  // External + executed actions in the window. Internal actions (memory_write,
  // briefing_create, schedule_workflow) stay silent — those are bookkeeping,
  // not "the system did something on your behalf in the world."
  const rows = await db()
    .select({
      id: action_request.id,
      kind: action_request.kind,
      target: action_request.target,
      payload: action_request.payload,
      summary: action_request.summary,
      scope: action_request.scope,
      riskLevel: action_request.risk_level,
      status: action_request.status,
      minutesSaved: action_request.minutes_saved,
      minutesSavedBasis: action_request.minutes_saved_basis,
      approvedAt: action_request.approved_at,
      approvedByUserId: action_request.approved_by_user_id,
      policyId: action_request.policy_id,
      executedAt: action_execution.finished_at,
      executionStatus: action_execution.status,
      workflowRunId: action_request.workflow_run_id,
      workflowId: workflow_definition.id,
      workflowName: workflow_definition.name,
      policyName: action_policy.name,
      trigger: observation.title,
    })
    .from(action_request)
    .innerJoin(
      action_execution,
      eq(action_execution.action_request_id, action_request.id),
    )
    .leftJoin(
      workflow_run,
      eq(action_request.workflow_run_id, workflow_run.id),
    )
    .leftJoin(
      workflow_definition,
      eq(workflow_run.workflow_id, workflow_definition.id),
    )
    .leftJoin(
      action_policy,
      eq(action_request.policy_id, action_policy.id),
    )
    .leftJoin(
      observation,
      eq(action_request.triggered_by_observation_id, observation.id),
    )
    .where(
      and(
        eq(action_request.org_id, orgId),
        eq(action_request.scope, "external"),
        eq(action_request.status, "executed"),
        gte(action_execution.finished_at, since),
      ),
    )
    .orderBy(desc(action_execution.finished_at))
    .limit(LIMIT);

  const workflowVisible = await workflowIdVisibility();
  const watcherVisible = await watcherRunVisibility(rows.map((r) => r.workflowRunId));
  return NextResponse.json({
    receipts: rows.filter((r) => workflowVisible(r.workflowId) && watcherVisible(r.workflowRunId)).map((r) => ({
      id: r.id,
      kind: r.kind,
      target: r.target,
      payload: r.payload,
      summary: r.summary,
      scope: r.scope,
      riskLevel: r.riskLevel,
      status: r.status,
      executedAt: r.executedAt?.toISOString() ?? null,
      executionStatus: r.executionStatus,
      minutesSaved: r.minutesSaved ?? null,
      minutesSavedBasis: r.minutesSavedBasis ?? null,
      approverKind: r.approvedByUserId
        ? ("operator" as const)
        : r.policyId
          ? ("policy" as const)
          : ("auto" as const),
      approverLabel: r.approvedByUserId ?? r.policyName ?? null,
      workflowRunId: r.workflowRunId,
      workflow: r.workflowId
        ? { id: r.workflowId, name: r.workflowName ?? "" }
        : null,
      trigger: r.trigger,
    })),
    windowHours: RECENT_WINDOW_HOURS,
  });
}
