import { NextRequest, NextResponse } from "next/server";
import {
  and,
  db,
  desc,
  eq,
  inArray,
  sql,
  workflow_definition,
  workflow_run,
} from "@neko/db";
import { getOrgId } from "@/lib/db";
import { workflowVisibility } from "@/lib/entitlements";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type StatusFilter = "active" | "completed" | "failed" | "all";

function parseStatusFilter(value: string | null): StatusFilter {
  if (
    value === "active" ||
    value === "completed" ||
    value === "failed" ||
    value === "all"
  ) {
    return value;
  }
  return "all";
}

function statusesForFilter(filter: StatusFilter): string[] | null {
  if (filter === "active") {
    return ["queued", "running", "needs_input", "waiting_approval"];
  }
  if (filter === "completed") return ["completed"];
  if (filter === "failed") return ["failed", "cancelled"];
  return null;
}

function parseLimit(value: string | null): number {
  const n = Number.parseInt(value ?? "", 10);
  if (!Number.isFinite(n) || n <= 0) return 100;
  return Math.min(n, 200);
}

export async function GET(request: NextRequest) {
  const orgId = await getOrgId();
  const sp = new URL(request.url).searchParams;
  const filter = parseStatusFilter(sp.get("status"));
  const limit = parseLimit(sp.get("limit"));
  const statuses = statusesForFilter(filter);
  const statusCondition = statuses
    ? statuses.length === 1
      ? eq(workflow_run.status, statuses[0])
      : inArray(workflow_run.status, statuses)
    : undefined;

  const visible = await workflowVisibility();
  const fetched = await db()
    .select({
      id: workflow_run.id,
      workflowId: workflow_run.workflow_id,
      workflowName: workflow_definition.name,
      workflowOwnerUserId: workflow_definition.owner_user_id,
      triggerKind: workflow_run.trigger_kind,
      executionMode: workflow_run.execution_mode,
      chainDepth: workflow_run.chain_depth,
      status: workflow_run.status,
      queueAttempts: workflow_run.queue_attempts,
      progress: workflow_run.progress,
      telemetry: workflow_run.telemetry_summary,
      summary: workflow_run.summary,
      error: workflow_run.error,
      startedAt: workflow_run.started_at,
      finishedAt: workflow_run.finished_at,
      createdAt: workflow_run.created_at,
      updatedAt: workflow_run.updated_at,
      outputCount: sql<number>`(
        select count(*)::int
        from workflow_output o
        where o.workflow_run_id = ${workflow_run.id}
      )`,
      actionCount: sql<number>`(
        select count(*)::int
        from action_request a
        where a.workflow_run_id = ${workflow_run.id}
      )`,
      pendingActionCount: sql<number>`(
        select count(*)::int
        from action_request a
        where a.workflow_run_id = ${workflow_run.id}
          and a.status = 'pending_approval'
      )`,
    })
    .from(workflow_run)
    .innerJoin(
      workflow_definition,
      eq(workflow_run.workflow_id, workflow_definition.id),
    )
    .where(
      statusCondition
        ? and(eq(workflow_run.org_id, orgId), statusCondition)
        : eq(workflow_run.org_id, orgId),
    )
    .orderBy(desc(workflow_run.created_at))
    .limit(Math.max(limit * 5, 500));
  const rows = fetched
    .filter((r) => visible({ id: r.workflowId, ownerUserId: r.workflowOwnerUserId }))
    .slice(0, limit);

  return NextResponse.json({
    runs: rows.map((r) => ({
      id: r.id,
      workflow: {
        id: r.workflowId,
        name: r.workflowName,
      },
      triggerKind: r.triggerKind,
      executionMode: r.executionMode,
      chainDepth: r.chainDepth,
      status: r.status,
      queueAttempts: r.queueAttempts,
      progress: r.progress,
      telemetry: r.telemetry,
      summary: r.summary,
      error: r.error,
      startedAt: r.startedAt?.toISOString() ?? null,
      finishedAt: r.finishedAt?.toISOString() ?? null,
      durationMs:
        r.startedAt && r.finishedAt
          ? r.finishedAt.getTime() - r.startedAt.getTime()
          : null,
      createdAt: r.createdAt.toISOString(),
      updatedAt: r.updatedAt.toISOString(),
      outputCount: r.outputCount,
      actionCount: r.actionCount,
      pendingActionCount: r.pendingActionCount,
    })),
    status: filter,
  });
}
