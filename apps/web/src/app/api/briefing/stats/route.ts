import { NextResponse } from "next/server";
import {
  action_request,
  and,
  db,
  eq,
  gte,
  inArray,
  sql,
  workflow_definition,
  workflow_output,
  workflow_run,
} from "@neko/db";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

function startOfTodayUtc(): Date {
  const d = new Date();
  d.setUTCHours(0, 0, 0, 0);
  return d;
}

// OL9 — the Briefing stat strip: runs today · findings · pending
// approvals · daily budget %. Budget % is the *hottest* workflow's
// utilisation (runs today / daily_run_budget), the one closest to its
// brake; the strip only shows it >= 40%.
export async function GET() {
  const orgId = await getOrgId();
  const dayStart = startOfTodayUtc();

  const [runs] = await db()
    .select({ n: sql<number>`count(*)::int` })
    .from(workflow_run)
    .where(
      and(eq(workflow_run.org_id, orgId), gte(workflow_run.created_at, dayStart)),
    );

  const [findings] = await db()
    .select({ n: sql<number>`count(*)::int` })
    .from(workflow_output)
    .where(
      and(
        eq(workflow_output.org_id, orgId),
        gte(workflow_output.created_at, dayStart),
        inArray(workflow_output.mood, ["watch", "act"]),
      ),
    );

  const [approvals] = await db()
    .select({ n: sql<number>`count(*)::int` })
    .from(action_request)
    .where(
      and(
        eq(action_request.org_id, orgId),
        eq(action_request.status, "pending_approval"),
      ),
    );

  const budgeted = await db()
    .select({
      workflowId: workflow_definition.id,
      budget: workflow_definition.daily_run_budget,
      ran: sql<number>`(
        select count(*)::int from workflow_run r
        where r.workflow_id = ${workflow_definition.id}
          and r.org_id = ${orgId}
          and r.created_at >= ${dayStart}
      )`,
    })
    .from(workflow_definition)
    .where(
      and(
        eq(workflow_definition.org_id, orgId),
        sql`${workflow_definition.daily_run_budget} is not null`,
      ),
    );

  let budgetPct: number | null = null;
  for (const row of budgeted) {
    if (!row.budget || row.budget <= 0) continue;
    const pct = Math.round((row.ran / row.budget) * 100);
    if (budgetPct === null || pct > budgetPct) budgetPct = pct;
  }

  return NextResponse.json({
    runsToday: runs?.n ?? 0,
    findingsToday: findings?.n ?? 0,
    pendingApprovals: approvals?.n ?? 0,
    budgetPct,
  });
}
