import { NextRequest, NextResponse } from "next/server";
import {
  action_request,
  and,
  db,
  desc,
  eq,
  workflow_definition,
  workflow_output,
  workflow_run,
} from "@neko/db";
import { getOrgId } from "@/lib/db";
import { requireWorkflowRun } from "@/lib/entitlements";
import { getWorkRunEventsAfter } from "@/lib/work-store";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// Operator-meaningful event kinds. tool_* events are hidden by default and
// only included when ?detail=tools is set, since they're noise in the
// triage view.
const TOOL_KINDS = new Set(["tool_start", "tool_delta", "tool_end"]);

type RouteContext = {
  params: Promise<{ workflowRunId: string }>;
};

export async function GET(request: NextRequest, context: RouteContext) {
  const { workflowRunId } = await context.params;
  const deniedRun = await requireWorkflowRun(workflowRunId);
  if (deniedRun) return deniedRun;
  const orgId = await getOrgId();
  const includeTools =
    new URL(request.url).searchParams.get("detail") === "tools";

  const runs = await db()
    .select()
    .from(workflow_run)
    .where(
      and(
        eq(workflow_run.org_id, orgId),
        eq(workflow_run.id, workflowRunId),
      ),
    )
    .limit(1);

  const run = runs[0];
  if (!run) {
    return NextResponse.json(
      { error: "Workflow run not found" },
      { status: 404 },
    );
  }

  const wfRows = await db()
    .select()
    .from(workflow_definition)
    .where(
      and(
        eq(workflow_definition.org_id, orgId),
        eq(workflow_definition.id, run.workflow_id),
      ),
    )
    .limit(1);
  const workflow = wfRows[0] ?? null;

  const outputs = await db()
    .select()
    .from(workflow_output)
    .where(eq(workflow_output.workflow_run_id, workflowRunId))
    .orderBy(desc(workflow_output.created_at));

  const actions = await db()
    .select()
    .from(action_request)
    .where(
      and(
        eq(action_request.org_id, orgId),
        eq(action_request.workflow_run_id, workflowRunId),
      ),
    )
    .orderBy(desc(action_request.created_at));

  const allEvents = run.work_run_id
    ? await getWorkRunEventsAfter(orgId, run.work_run_id, 0)
    : [];
  const events = includeTools
    ? allEvents
    : allEvents.filter((e) => !TOOL_KINDS.has(e.event?.type ?? ""));

  // Lineage: if subscription-triggered, surface the upstream output's title
  // and originating workflow so the operator can walk backwards.
  const lineage: {
    triggeredBySubscriptionId: string | null;
    triggeredByOutputId: string | null;
    triggeredByObservationId: string | null;
    upstream: null | {
      output: {
        id: string;
        title: string;
        scope: string | null;
        mood: string | null;
        createdAt: string;
      };
      workflow: {
        id: string;
        name: string;
      } | null;
      workflowRunId: string | null;
    };
  } = {
    triggeredBySubscriptionId: run.triggered_by_subscription_id,
    triggeredByOutputId: run.triggered_by_output_id,
    triggeredByObservationId: run.triggered_by_observation_id,
    upstream: null,
  };

  if (run.triggered_by_output_id) {
    const upstreamOutputRows = await db()
      .select()
      .from(workflow_output)
      .where(eq(workflow_output.id, run.triggered_by_output_id))
      .limit(1);
    const upstreamOutput = upstreamOutputRows[0];
    if (upstreamOutput) {
      const upstreamWfRows = upstreamOutput.workflow_run_id
        ? await db()
            .select()
            .from(workflow_run)
            .where(eq(workflow_run.id, upstreamOutput.workflow_run_id))
            .limit(1)
        : [];
      const upstreamRun = upstreamWfRows[0];
      const upstreamWfDef = upstreamRun
        ? await db()
            .select({
              id: workflow_definition.id,
              name: workflow_definition.name,
            })
            .from(workflow_definition)
            .where(eq(workflow_definition.id, upstreamRun.workflow_id))
            .limit(1)
        : [];

      lineage.upstream = {
        output: {
          id: upstreamOutput.id,
          title: upstreamOutput.title,
          scope: upstreamOutput.scope,
          mood: upstreamOutput.mood,
          createdAt: upstreamOutput.created_at.toISOString(),
        },
        workflow: upstreamWfDef[0] ?? null,
        workflowRunId: upstreamRun?.id ?? null,
      };
    }
  }

  return NextResponse.json({
    workflow: workflow
      ? {
          id: workflow.id,
          name: workflow.name,
          description: workflow.description,
          goal: workflow.goal,
        }
      : null,
    run: {
      id: run.id,
      workflowId: run.workflow_id,
      threadId: run.thread_id,
      workRunId: run.work_run_id,
      triggerKind: run.trigger_kind,
      triggerPayload: run.trigger_payload,
      executionMode: run.execution_mode,
      triggerInputPreview: run.trigger_input_preview,
      chainDepth: run.chain_depth,
      status: run.status,
      telemetry: run.telemetry_summary,
      terminalResult: run.terminal_result,
      progress: run.progress,
      queueAttempts: run.queue_attempts,
      admittedAt: run.admitted_at?.toISOString() ?? null,
      resultExpiresAt: run.result_expires_at?.toISOString() ?? null,
      artifactUrl: run.result_artifact_path
        ? `/api/workflow-runs/${run.id}/artifact`
        : null,
      summary: run.summary,
      error: run.error,
      startedAt: run.started_at?.toISOString() ?? null,
      finishedAt: run.finished_at?.toISOString() ?? null,
      createdAt: run.created_at.toISOString(),
      updatedAt: run.updated_at.toISOString(),
    },
    outputs: outputs.map((o) => ({
      id: o.id,
      kind: o.kind,
      title: o.title,
      body: o.body,
      payload: o.payload,
      artifactPath: o.artifact_path,
      scope: o.scope,
      topic: o.topic,
      mood: o.mood,
      timeWindowStart: o.time_window_start?.toISOString() ?? null,
      timeWindowEnd: o.time_window_end?.toISOString() ?? null,
      freshnessTtlSeconds: o.freshness_ttl_seconds,
      createdAt: o.created_at.toISOString(),
    })),
    actions: actions.map((a) => ({
      id: a.id,
      kind: a.kind,
      target: a.target,
      payload: a.payload,
      scope: a.scope,
      riskLevel: a.risk_level,
      status: a.status,
      summary: a.summary,
      approvedAt: a.approved_at?.toISOString() ?? null,
      rejectionReason: a.rejection_reason,
      createdAt: a.created_at.toISOString(),
    })),
    events: events.map((e) => ({
      id: e.id,
      // Legacy alias — the runs UI still reads `seq` for grouping/keys.
      // Will be removed once the UI migrates to `id`.
      seq: e.id,
      type: e.event?.type ?? "unknown",
      event: e.event,
      createdAt: e.createdAt.toISOString(),
    })),
    lineage,
  });
}
