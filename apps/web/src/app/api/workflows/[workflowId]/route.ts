import { NextResponse } from "next/server";
import {
  action_request,
  and,
  db,
  desc,
  eq,
  gte,
  sql,
  work_run,
  workflow_definition,
  workflow_run,
} from "@neko/db";
import {
  deleteWorkflow,
  getWorkflow,
  listSubscriptionsByWorkflow,
} from "@neko/llm/workflows";
import { getOrgId } from "@/lib/db";
import { requireWorkflow } from "@/lib/entitlements";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ workflowId: string }>;
};

const RECENT_RUNS_LIMIT = 10;
const RECENT_ACTIONS_LIMIT = 10;
const SPARKLINE_DAYS = 14;

export async function GET(_request: Request, context: RouteContext) {
  const { workflowId } = await context.params;
  const deniedWorkflow = await requireWorkflow(workflowId);
  if (deniedWorkflow) return deniedWorkflow;
  const orgId = await getOrgId();

  const workflow = await getWorkflow(orgId, workflowId);
  if (!workflow) {
    return NextResponse.json({ error: "workflow not found" }, { status: 404 });
  }

  const subscriptions = await listSubscriptionsByWorkflow(orgId, workflowId);

  const recentRuns = await db()
    .select()
    .from(workflow_run)
    .where(
      and(
        eq(workflow_run.org_id, orgId),
        eq(workflow_run.workflow_id, workflowId),
      ),
    )
    .orderBy(desc(workflow_run.created_at))
    .limit(RECENT_RUNS_LIMIT);

  // Action requests joined back to workflow_run so we can scope by workflow.
  const recentActionsRows = await db()
    .select({
      id: action_request.id,
      workflowRunId: action_request.workflow_run_id,
      kind: action_request.kind,
      target: action_request.target,
      status: action_request.status,
      riskLevel: action_request.risk_level,
      summary: action_request.summary,
      approvedAt: action_request.approved_at,
      createdAt: action_request.created_at,
    })
    .from(action_request)
    .innerJoin(workflow_run, eq(action_request.workflow_run_id, workflow_run.id))
    .where(
      and(
        eq(action_request.org_id, orgId),
        eq(workflow_run.workflow_id, workflowId),
      ),
    )
    .orderBy(desc(action_request.created_at))
    .limit(RECENT_ACTIONS_LIMIT);

  const since = new Date();
  since.setUTCHours(0, 0, 0, 0);
  since.setUTCDate(since.getUTCDate() - (SPARKLINE_DAYS - 1));

  const sparkRows = await db()
    .select({
      day: sql<string>`date_trunc('day', ${workflow_run.created_at})::date::text`,
      count: sql<number>`count(*)::int`,
    })
    .from(workflow_run)
    .where(
      and(
        eq(workflow_run.org_id, orgId),
        eq(workflow_run.workflow_id, workflowId),
        gte(workflow_run.created_at, since),
      ),
    )
    .groupBy(sql`date_trunc('day', ${workflow_run.created_at})`);

  const sparkByDay = new Map<string, number>();
  for (const r of sparkRows) sparkByDay.set(r.day, r.count);
  const activitySparkline: number[] = [];
  for (let i = 0; i < SPARKLINE_DAYS; i += 1) {
    const d = new Date(since);
    d.setUTCDate(since.getUTCDate() + i);
    const key = d.toISOString().slice(0, 10);
    activitySparkline.push(sparkByDay.get(key) ?? 0);
  }

  const startOfToday = new Date();
  startOfToday.setUTCHours(0, 0, 0, 0);
  const [todayCount] = await db()
    .select({ count: sql<number>`count(*)::int` })
    .from(workflow_run)
    .where(
      and(
        eq(workflow_run.org_id, orgId),
        eq(workflow_run.workflow_id, workflowId),
        gte(workflow_run.created_at, startOfToday),
      ),
    );

  // Hours saved over the last 30 days: completed-run analysis + executed
  // action minutes, scoped to this workflow's runs.
  const since30d = new Date(Date.now() - 30 * 24 * 3600 * 1000);
  const [analysisAgg] = await db()
    .select({
      min: sql<number>`coalesce(sum(${work_run.analysis_minutes_saved}), 0)::int`,
    })
    .from(workflow_run)
    .innerJoin(work_run, eq(workflow_run.work_run_id, work_run.id))
    .where(
      and(
        eq(workflow_run.org_id, orgId),
        eq(workflow_run.workflow_id, workflowId),
        eq(work_run.status, "completed"),
        gte(workflow_run.created_at, since30d),
      ),
    );
  const [actionAgg] = await db()
    .select({
      min: sql<number>`coalesce(sum(${action_request.minutes_saved}), 0)::int`,
    })
    .from(action_request)
    .innerJoin(workflow_run, eq(action_request.workflow_run_id, workflow_run.id))
    .where(
      and(
        eq(workflow_run.org_id, orgId),
        eq(workflow_run.workflow_id, workflowId),
        eq(action_request.status, "executed"),
        gte(action_request.created_at, since30d),
      ),
    );
  const minutesSaved30d = (analysisAgg?.min ?? 0) + (actionAgg?.min ?? 0);

  return NextResponse.json({
    workflow: {
      id: workflow.id,
      name: workflow.name,
      description: workflow.description,
      goal: workflow.goal,
      systemPromptOverlay: workflow.systemPromptOverlay,
      enabled: workflow.enabled,
      status: workflow.status,
      steps: workflow.steps,
      cron: workflow.cron,
      cronTimezone: workflow.cronTimezone,
      cronEnabled: workflow.cronEnabled,
      dailyRunBudget: workflow.dailyRunBudget,
      runsToday: todayCount?.count ?? 0,
      minutesSaved30d,
      createdByThreadId: workflow.createdByThreadId,
      createdByRunId: workflow.createdByRunId,
      createdAt: workflow.createdAt.toISOString(),
      updatedAt: workflow.updatedAt.toISOString(),
    },
    subscriptions: subscriptions.map((s) => ({
      id: s.id,
      sourceKind: s.sourceKind,
      filter: s.filter,
      enabled: s.enabled,
      debounceMs: s.debounceMs,
    })),
    recentRuns: recentRuns.map((r) => ({
      id: r.id,
      status: r.status,
      triggerKind: r.trigger_kind,
      executionMode: r.execution_mode,
      queueAttempts: r.queue_attempts,
      progress: r.progress,
      telemetry: r.telemetry_summary,
      summary: r.summary,
      error: r.error,
      startedAt: r.started_at?.toISOString() ?? null,
      finishedAt: r.finished_at?.toISOString() ?? null,
      durationMs:
        r.started_at && r.finished_at
          ? r.finished_at.getTime() - r.started_at.getTime()
          : null,
      createdAt: r.created_at.toISOString(),
    })),
    recentActions: recentActionsRows.map((a) => ({
      id: a.id,
      workflowRunId: a.workflowRunId,
      kind: a.kind,
      target: a.target,
      status: a.status,
      riskLevel: a.riskLevel,
      summary: a.summary,
      approvedAt: a.approvedAt?.toISOString() ?? null,
      createdAt: a.createdAt.toISOString(),
    })),
    activitySparkline,
  });
}

export async function PATCH(request: Request, context: RouteContext) {
  const { workflowId } = await context.params;
  const deniedWorkflow = await requireWorkflow(workflowId);
  if (deniedWorkflow) return deniedWorkflow;
  const orgId = await getOrgId();
  const body = await request.json().catch(() => ({}));

  const patch: {
    enabled?: boolean;
    cron_enabled?: boolean;
    paused_until?: Date | null;
  } = {};
  if (typeof body.enabled === "boolean") {
    patch.enabled = body.enabled;
    // Re-enabling clears any pause-for-today timer.
    if (body.enabled) patch.paused_until = null;
  }
  if (typeof body.cronEnabled === "boolean") patch.cron_enabled = body.cronEnabled;
  // OL7 "pause for today": park until the next UTC midnight; the cron
  // sweep re-enables it.
  if (body.pauseForToday === true) {
    const until = new Date();
    until.setUTCHours(24, 0, 0, 0);
    patch.enabled = false;
    patch.paused_until = until;
  }
  if (Object.keys(patch).length === 0) {
    return NextResponse.json({ error: "no recognized fields" }, { status: 400 });
  }

  const result = await db()
    .update(workflow_definition)
    .set({ ...patch, updated_at: new Date() })
    .where(
      and(
        eq(workflow_definition.org_id, orgId),
        eq(workflow_definition.id, workflowId),
      ),
    )
    .returning({ id: workflow_definition.id });

  if (result.length === 0) {
    return NextResponse.json({ error: "workflow not found" }, { status: 404 });
  }

  return NextResponse.json({ ok: true });
}

export async function DELETE(_request: Request, context: RouteContext) {
  const { workflowId } = await context.params;
  const deniedWorkflow = await requireWorkflow(workflowId);
  if (deniedWorkflow) return deniedWorkflow;
  const orgId = await getOrgId();

  const deleted = await deleteWorkflow(orgId, workflowId);
  if (!deleted) {
    return NextResponse.json({ error: "workflow not found" }, { status: 404 });
  }

  return NextResponse.json({ ok: true });
}
