import { NextRequest, NextResponse } from "next/server";
import {
  action_policy,
  action_request,
  and,
  db,
  desc,
  eq,
  inArray,
  ne,
  observation,
  or,
  sql,
  workflow_definition,
  workflow_run,
} from "@neko/db";
import { actionOutcome } from "@/lib/action-outcome";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { actionRequestVisibility } from "@/lib/entitlements";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// Risk levels sort highest-urgency first; missing or unknown levels go last.
const RISK_ORDER: Record<string, number> = {
  critical: 0,
  high: 1,
  medium: 2,
  low: 3,
};

type Filter = "awaiting" | "fired" | "failed" | "rejected" | "all";

function parseFilter(value: string | null): Filter {
  if (value === "fired" || value === "failed" || value === "rejected" || value === "all") return value;
  return "awaiting";
}

// An executed request whose change-set Magento accepted but OpenNeko could
// not confirm belongs with the failures, not with the completed work.
const unconfirmedExecution = sql`exists (
  select 1 from action_execution e
  where e.action_request_id = ${action_request.id}
    and e.result->>'status' in ('reconcile_required', 'partially_applied')
    and e.created_at = (
      select max(e2.created_at) from action_execution e2
      where e2.action_request_id = ${action_request.id}
    )
)`;

function conditionForFilter(filter: Filter) {
  if (filter === "awaiting") return eq(action_request.status, "pending_approval");
  if (filter === "fired") {
    return and(
      inArray(action_request.status, ["executed", "approved"]),
      sql`not ${unconfirmedExecution}`,
    );
  }
  if (filter === "failed") {
    return or(
      eq(action_request.status, "failed"),
      and(eq(action_request.status, "executed"), unconfirmedExecution),
    );
  }
  if (filter === "rejected") return eq(action_request.status, "rejected");
  return undefined;
}

export async function GET(request: NextRequest) {
  const [orgId, actor] = await Promise.all([getOrgId(), getCurrentActor()]);
  const canReadSourceConfig = actor.role === "admin";
  const sp = new URL(request.url).searchParams;
  const countOnly = sp.get("countOnly") === "true";
  const filter = parseFilter(sp.get("filter"));

  // The nav badge always tracks pending_approval count regardless of the
  // current view — the operator should always know how many actions need
  // their call.
  if (countOnly) {
    const [row] = await db()
      .select({ count: sql<number>`count(*)::int` })
      .from(action_request)
      .where(
        and(
          eq(action_request.org_id, orgId),
          eq(action_request.status, "pending_approval"),
          ...(canReadSourceConfig
            ? []
            : [ne(action_request.kind, "source_config_admin")]),
        ),
      );
    return NextResponse.json({ count: row?.count ?? 0 });
  }

  const statusCondition = conditionForFilter(filter);

  const rows = await db()
    .select({
      id: action_request.id,
      workflowRunId: action_request.workflow_run_id,
      triggeredByObservationId: action_request.triggered_by_observation_id,
      kind: action_request.kind,
      target: action_request.target,
      payload: action_request.payload,
      riskLevel: action_request.risk_level,
      summary: action_request.summary,
      scope: action_request.scope,
      status: action_request.status,
      minutesSaved: action_request.minutes_saved,
      approvedAt: action_request.approved_at,
      approvedByUserId: action_request.approved_by_user_id,
      policyId: action_request.policy_id,
      rejectionReason: action_request.rejection_reason,
      failureReason: action_request.failure_reason,
      createdAt: action_request.created_at,
      runStartedAt: workflow_run.started_at,
      runCreatedAt: workflow_run.created_at,
      workflowId: workflow_definition.id,
      workflowName: workflow_definition.name,
      observationTitle: observation.title,
      policyName: action_policy.name,
      executionResultStatus: sql<string | null>`(
        select coalesce(e.result->>'status', e.status)
        from action_execution e
        where e.action_request_id = ${action_request.id}
        order by e.created_at desc
        limit 1
      )`,
      executionError: sql<string | null>`(
        select e.error
        from action_execution e
        where e.action_request_id = ${action_request.id}
        order by e.created_at desc
        limit 1
      )`,
    })
    .from(action_request)
    // LEFT joins: chat-proposed admin actions (plugin/user/channel/data-source/
    // source-config) carry no workflow_run_id — inner joins dropped them here
    // while the badge counted them, leaving them unapprovable in the UI.
    .leftJoin(workflow_run, eq(action_request.workflow_run_id, workflow_run.id))
    .leftJoin(
      workflow_definition,
      eq(workflow_run.workflow_id, workflow_definition.id),
    )
    .leftJoin(
      observation,
      eq(action_request.triggered_by_observation_id, observation.id),
    )
    .leftJoin(
      action_policy,
      eq(action_request.policy_id, action_policy.id),
    )
    .where(
      and(
        eq(action_request.org_id, orgId),
        ...(statusCondition ? [statusCondition] : []),
        ...(canReadSourceConfig
          ? []
          : [ne(action_request.kind, "source_config_admin")]),
      ),
    )
    .orderBy(desc(action_request.created_at))
    .limit(filter === "awaiting" ? 200 : 100);

  // For the awaiting tab, sort by risk first then time desc — the operator
  // wants critical at the top. Other tabs are time-ordered (already from SQL).
  const sorted =
    filter === "awaiting"
      ? [...rows].sort((a, b) => {
          const ra = RISK_ORDER[a.riskLevel ?? ""] ?? 99;
          const rb = RISK_ORDER[b.riskLevel ?? ""] ?? 99;
          if (ra !== rb) return ra - rb;
          return b.createdAt.getTime() - a.createdAt.getTime();
        })
      : rows;

  const actionVisible = await actionRequestVisibility();
  return NextResponse.json({
    actions: sorted.filter((r) => actionVisible(r)).map((r) => ({
      id: r.id,
      workflowRunId: r.workflowRunId,
      workflow: r.workflowId ? { id: r.workflowId, name: r.workflowName } : null,
      triggeredByObservation: r.observationTitle
        ? { title: r.observationTitle }
        : null,
      kind: r.kind,
      target: r.target,
      payload: r.payload,
      riskLevel: r.riskLevel,
      summary: r.summary,
      scope: r.scope,
      status: r.status,
      minutesSaved: r.minutesSaved ?? null,
      approvedAt: r.approvedAt?.toISOString() ?? null,
      approverKind: r.approvedByUserId
        ? ("operator" as const)
        : r.policyId
          ? ("policy" as const)
          : r.approvedAt
            ? ("auto" as const)
            : null,
      approverLabel: r.approvedByUserId ?? r.policyName ?? null,
      rejectionReason: r.rejectionReason,
      failureReason: r.failureReason,
      outcome: actionOutcome(r.status, r.executionResultStatus),
      executionError: r.executionError,
      runAt: (r.runStartedAt ?? r.runCreatedAt ?? r.createdAt).toISOString(),
      createdAt: r.createdAt.toISOString(),
    })),
    count: sorted.length,
    filter,
  });
}
