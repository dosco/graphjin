import { NextResponse } from "next/server";
import {
  action_request,
  and,
  briefing,
  briefing_card,
  briefing_finding_pin,
  db,
  desc,
  eq,
  gt,
  gte,
  muted_scope,
  ne,
  notInArray,
  observation,
  sql,
  workflow_definition,
  workflow_output,
  workflow_run,
} from "@neko/db";
import { summarizeBriefing } from "@neko/llm";
import { getCurrentActor } from "@/lib/actor";
import { serializeBriefingApproval } from "@/lib/briefing-findings";
import { getOrgId } from "@/lib/db";
import { watcherRunVisibility, workflowIdVisibility } from "@/lib/entitlements";

// 5-minute freshness window for the auto-generated live summary. If a row
// exists newer than this, reuse it. Otherwise compose a fresh template.
const SUMMARY_FRESHNESS_MS = 5 * 60 * 1000;

function composeSummary(args: {
  pendingApprovals: number;
  actFindings: number;
  watchFindings: number;
  goodRuns: number;
  windowHours: number;
}): string {
  const parts: string[] = [];

  if (args.actFindings > 0) {
    parts.push(
      args.actFindings === 1
        ? "One finding wants your attention."
        : `${args.actFindings} findings want your attention.`,
    );
  }

  if (args.pendingApprovals > 0) {
    parts.push(
      args.pendingApprovals === 1
        ? "One approval is queued."
        : `${args.pendingApprovals} approvals are queued.`,
    );
  }

  if (args.watchFindings > 0) {
    parts.push(
      args.watchFindings === 1
        ? "One thing is worth a look."
        : `${args.watchFindings} things are worth a look.`,
    );
  }

  if (args.goodRuns > 0 && parts.length > 0) {
    parts.push(
      args.goodRuns === 1
        ? "One other workflow ran cleanly."
        : `${args.goodRuns} other workflows ran cleanly.`,
    );
  } else if (args.goodRuns > 0) {
    parts.push(
      args.goodRuns === 1
        ? `One workflow ran cleanly in the last ${args.windowHours}h.`
        : `${args.goodRuns} workflows ran cleanly in the last ${args.windowHours}h.`,
    );
  }

  if (parts.length === 0) {
    return "Quiet shift — no workflows have run yet today.";
  }

  return parts.join(" ");
}

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// Briefing tributaries: pending approvals, mood=act findings, live summary.
// KPI cards continue to be served by the existing /api/briefing route.

const RISK_ORDER: Record<string, number> = {
  critical: 0,
  high: 1,
  medium: 2,
  low: 3,
};

const RECENT_FINDING_WINDOW_HOURS = 24;
const ACT_LIMIT = 8;
const APPROVAL_LIMIT = 8;

export async function GET() {
  const [orgId, actor] = await Promise.all([getOrgId(), getCurrentActor()]);
  const canReadSourceConfig = actor.role === "admin";

  // 0. Active scope mutes (OL7) — matching finding cards are filtered
  // out of every tributary below until muted_until passes.
  const muteRows = await db()
    .select({ scope: muted_scope.scope, mutedUntil: muted_scope.muted_until })
    .from(muted_scope)
    .where(
      and(eq(muted_scope.org_id, orgId), gt(muted_scope.muted_until, new Date())),
    );
  const mutedScopes = muteRows.map((m) => m.scope);
  const notMuted = mutedScopes.length
    ? notInArray(workflow_output.scope, mutedScopes)
    : undefined;

  // 1. Pending approvals — sorted risk desc, time desc.
  const approvalsRaw = await db()
    .select({
      id: action_request.id,
      workflowRunId: action_request.workflow_run_id,
      kind: action_request.kind,
      target: action_request.target,
      summary: action_request.summary,
      riskLevel: action_request.risk_level,
      createdAt: action_request.created_at,
      workflowId: workflow_definition.id,
      workflowName: workflow_definition.name,
      pendingApprovalCount: sql<number>`count(*) over()::int`,
    })
    .from(action_request)
    // Chat-proposed admin actions do not carry a workflow_run_id. Keep them
    // in the briefing just like /api/approvals does.
    .leftJoin(workflow_run, eq(action_request.workflow_run_id, workflow_run.id))
    .leftJoin(
      workflow_definition,
      eq(workflow_run.workflow_id, workflow_definition.id),
    )
    .where(
      and(
        eq(action_request.org_id, orgId),
        eq(action_request.status, "pending_approval"),
        ...(canReadSourceConfig
          ? []
          : [ne(action_request.kind, "source_config_admin")]),
      ),
    )
    .orderBy(desc(action_request.created_at))
    .limit(APPROVAL_LIMIT);

  const approvals = approvalsRaw.sort((a, b) => {
    const ra = RISK_ORDER[a.riskLevel ?? ""] ?? 99;
    const rb = RISK_ORDER[b.riskLevel ?? ""] ?? 99;
    if (ra !== rb) return ra - rb;
    return b.createdAt.getTime() - a.createdAt.getTime();
  });
  const pendingApprovalCount = approvals[0]?.pendingApprovalCount ?? 0;

  // 2. Recent mood=act findings — workflow_outputs from the last N hours.
  const since = new Date(Date.now() - RECENT_FINDING_WINDOW_HOURS * 3600 * 1000);
  const actRaw = await db()
    .select({
      id: workflow_output.id,
      workflowRunId: workflow_output.workflow_run_id,
      kind: workflow_output.kind,
      title: workflow_output.title,
      body: workflow_output.body,
      scope: workflow_output.scope,
      mood: workflow_output.mood,
      seenCount: workflow_output.seen_count,
      lastSeenAt: workflow_output.last_seen_at,
      createdAt: workflow_output.created_at,
      workflowId: workflow_definition.id,
      workflowName: workflow_definition.name,
    })
    .from(workflow_output)
    .innerJoin(workflow_run, eq(workflow_output.workflow_run_id, workflow_run.id))
    .innerJoin(
      workflow_definition,
      eq(workflow_run.workflow_id, workflow_definition.id),
    )
    .where(
      and(
        eq(workflow_output.org_id, orgId),
        eq(workflow_output.mood, "act"),
        gte(workflow_output.created_at, since),
        notMuted,
      ),
    )
    .orderBy(desc(workflow_output.created_at))
    .limit(ACT_LIMIT);

  // 3. mood=watch findings (Worth knowing) — same window, lower urgency.
  const watchRaw = await db()
    .select({
      id: workflow_output.id,
      workflowRunId: workflow_output.workflow_run_id,
      kind: workflow_output.kind,
      title: workflow_output.title,
      body: workflow_output.body,
      scope: workflow_output.scope,
      mood: workflow_output.mood,
      seenCount: workflow_output.seen_count,
      lastSeenAt: workflow_output.last_seen_at,
      createdAt: workflow_output.created_at,
      workflowId: workflow_definition.id,
      workflowName: workflow_definition.name,
    })
    .from(workflow_output)
    .innerJoin(workflow_run, eq(workflow_output.workflow_run_id, workflow_run.id))
    .innerJoin(
      workflow_definition,
      eq(workflow_run.workflow_id, workflow_definition.id),
    )
    .where(
      and(
        eq(workflow_output.org_id, orgId),
        eq(workflow_output.mood, "watch"),
        gte(workflow_output.created_at, since),
        notMuted,
      ),
    )
    .orderBy(desc(workflow_output.created_at))
    .limit(ACT_LIMIT);

  // 4. Operator-pinned findings — the curation layer. Joins through the pin
  // table so each card carries pinId + the output content + the workflow
  // for byline rendering. Pinned items show before everything else on
  // the Briefing.
  const pinnedRows = await db()
    .select({
      pinId: briefing_finding_pin.id,
      pinnedAt: briefing_finding_pin.pinned_at,
      sortOrder: briefing_finding_pin.sort_order,
      outputId: workflow_output.id,
      workflowRunId: workflow_output.workflow_run_id,
      kind: workflow_output.kind,
      title: workflow_output.title,
      body: workflow_output.body,
      scope: workflow_output.scope,
      mood: workflow_output.mood,
      outputCreatedAt: workflow_output.created_at,
      workflowId: workflow_definition.id,
      workflowName: workflow_definition.name,
    })
    .from(briefing_finding_pin)
    .innerJoin(
      workflow_output,
      eq(briefing_finding_pin.output_id, workflow_output.id),
    )
    .innerJoin(
      workflow_run,
      eq(workflow_output.workflow_run_id, workflow_run.id),
    )
    .innerJoin(
      workflow_definition,
      eq(workflow_run.workflow_id, workflow_definition.id),
    )
    .where(eq(briefing_finding_pin.org_id, orgId))
    .orderBy(
      briefing_finding_pin.sort_order,
      desc(briefing_finding_pin.pinned_at),
    );

  // 4b. Elevated observations (OL2) — first-class briefing cards keyed
  // on source_observation_id; they may have no producing output at all.
  const elevatedRows = await db()
    .select({
      cardId: briefing_card.id,
      observationId: briefing_card.source_observation_id,
      title: briefing_card.title,
      body: briefing_card.body,
      mood: briefing_card.mood,
      createdAt: briefing_card.created_at,
      consumerWorkflowId: observation.consumer_workflow_id,
      workflowName: workflow_definition.name,
    })
    .from(briefing_card)
    .innerJoin(observation, eq(briefing_card.source_observation_id, observation.id))
    .leftJoin(
      workflow_definition,
      eq(observation.consumer_workflow_id, workflow_definition.id),
    )
    .where(
      and(eq(briefing_card.org_id, orgId), eq(briefing_card.status, "active")),
    )
    .orderBy(desc(briefing_card.created_at))
    .limit(ACT_LIMIT);

  // 5. Roll-up count for quiet section (mood=good in window).
  const [goodCountRow] = await db()
    .select({ count: sql<number>`count(*)::int` })
    .from(workflow_output)
    .where(
      and(
        eq(workflow_output.org_id, orgId),
        eq(workflow_output.mood, "good"),
        gte(workflow_output.created_at, since),
      ),
    );

  // 5. Live summary paragraph — auto-generated template, v1 fallback before
  // the dedicated internal workflow is seeded. Cached for 5 minutes; refreshed
  // on stale read or when activity shifts. Stored under role="_summary" so
  // it's distinct from the legacy role-keyed daily briefings.
  const SUMMARY_ROLE = "_summary";
  const [existingSummary] = await db()
    .select({
      id: briefing.id,
      summaryMd: briefing.summary_md,
      createdAt: briefing.created_at,
      forDate: briefing.for_date,
    })
    .from(briefing)
    .where(and(eq(briefing.org_id, orgId), eq(briefing.role, SUMMARY_ROLE)))
    .orderBy(desc(briefing.created_at))
    .limit(1);

  let latestSummary = existingSummary ?? null;
  const isStale =
    !latestSummary ||
    Date.now() - latestSummary.createdAt.getTime() > SUMMARY_FRESHNESS_MS;

  if (isStale) {
    let summary: string;
    try {
      summary = await summarizeBriefing(
        {
          pendingApprovals: pendingApprovalCount,
          actFindings: actRaw.length,
          watchFindings: watchRaw.length,
          goodRuns: goodCountRow?.count ?? 0,
          pinnedCount: pinnedRows.length,
          windowHours: RECENT_FINDING_WINDOW_HOURS,
          topApprovalTitle: approvals[0]?.summary ?? approvals[0]?.kind ?? null,
          topActTitle: actRaw[0]?.title ?? null,
        },
        orgId,
      );
      if (!summary) throw new Error("empty summary");
    } catch (err) {
      console.warn("[briefing/findings] LLM summary failed, falling back:", err);
      summary = composeSummary({
        pendingApprovals: pendingApprovalCount,
        actFindings: actRaw.length,
        watchFindings: watchRaw.length,
        goodRuns: goodCountRow?.count ?? 0,
        windowHours: RECENT_FINDING_WINDOW_HOURS,
      });
    }

    const today = new Date().toISOString().slice(0, 10);

    try {
      // Upsert: one row per (org, _summary, date). In-place updates keep
      // the row count small over time.
      const [upserted] = await db()
        .insert(briefing)
        .values({
          org_id: orgId,
          role: SUMMARY_ROLE,
          for_date: today,
          profile_version: 1,
          summary_md: summary,
          insights: [],
        })
        .onConflictDoUpdate({
          target: [briefing.org_id, briefing.role, briefing.for_date],
          set: { summary_md: summary, updated_at: new Date() },
        })
        .returning({
          id: briefing.id,
          summaryMd: briefing.summary_md,
          createdAt: briefing.created_at,
          forDate: briefing.for_date,
        });
      if (upserted) latestSummary = upserted;
    } catch (err) {
      console.warn("[briefing/findings] summary upsert failed:", err);
    }
  }

  const workflowVisibleById = await workflowIdVisibility();
  const watcherVisible = await watcherRunVisibility([
    ...actRaw.map((o) => o.workflowRunId),
    ...watchRaw.map((o) => o.workflowRunId),
    ...pinnedRows.map((p) => p.workflowRunId),
  ]);
  const workflowVisible = (workflowId: string | null | undefined, runId?: string | null) =>
    workflowVisibleById(workflowId) && watcherVisible(runId);
  return NextResponse.json({
    summary: latestSummary
      ? {
          id: latestSummary.id,
          summaryMd: latestSummary.summaryMd,
          createdAt: latestSummary.createdAt.toISOString(),
        }
      : null,
    awaitingYou: {
      approvals: approvals.filter((a) => workflowVisible(a.workflowId)).map(serializeBriefingApproval),
      actFindings: actRaw.filter((o) => workflowVisible(o.workflowId, o.workflowRunId)).map((o) => ({
        id: o.id,
        kind: "finding" as const,
        workflowRunId: o.workflowRunId,
        workflow: { id: o.workflowId, name: o.workflowName },
        title: o.title,
        body: o.body,
        scope: o.scope,
        mood: o.mood,
        outputKind: o.kind,
        seenCount: o.seenCount,
        lastSeenAt: o.lastSeenAt.toISOString(),
        createdAt: o.createdAt.toISOString(),
      })),
    },
    pinned: pinnedRows.filter((p) => workflowVisible(p.workflowId, p.workflowRunId)).map((p) => ({
      id: p.outputId,
      pinId: p.pinId,
      kind: "finding" as const,
      workflowRunId: p.workflowRunId,
      workflow: { id: p.workflowId, name: p.workflowName },
      title: p.title,
      body: p.body,
      scope: p.scope,
      mood: p.mood,
      outputKind: p.kind,
      createdAt: p.outputCreatedAt.toISOString(),
      pinnedAt: p.pinnedAt.toISOString(),
    })),
    worthKnowing: watchRaw.filter((o) => workflowVisible(o.workflowId, o.workflowRunId)).map((o) => ({
      id: o.id,
      kind: "finding" as const,
      workflowRunId: o.workflowRunId,
      workflow: { id: o.workflowId, name: o.workflowName },
      title: o.title,
      body: o.body,
      scope: o.scope,
      mood: o.mood,
      outputKind: o.kind,
      seenCount: o.seenCount,
      lastSeenAt: o.lastSeenAt.toISOString(),
      createdAt: o.createdAt.toISOString(),
    })),
    elevated: elevatedRows.filter((c) => workflowVisible(c.consumerWorkflowId)).map((c) => ({
      id: c.cardId,
      kind: "elevated" as const,
      observationId: c.observationId,
      workflow: c.consumerWorkflowId
        ? { id: c.consumerWorkflowId, name: c.workflowName ?? "workflow" }
        : null,
      title: c.title,
      body: c.body,
      mood: c.mood,
      createdAt: c.createdAt.toISOString(),
    })),
    mutedScopes,
    quiet: {
      goodOutputs: goodCountRow?.count ?? 0,
      windowHours: RECENT_FINDING_WINDOW_HOURS,
    },
  });
}
