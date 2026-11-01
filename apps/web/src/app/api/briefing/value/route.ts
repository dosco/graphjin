import { NextResponse } from "next/server";
import {
  action_execution,
  action_request,
  and,
  db,
  eq,
  gte,
  organization,
  sql,
  work_run,
} from "@neko/db";
import { getOrgId } from "@/lib/db";
import { fillDailySeries } from "@/lib/hours-saved";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const WINDOW_HOURS = 24;
const DAILY_DAYS = 7;

// Realized hours saved = executed actions + completed-run analysis. Returns
// the rolling-window total (for the dashboard line) and the cumulative total
// (the value-prop hero). Estimates are agent-produced and server-clamped at
// write time; see docs/HOURS_SAVED_PLAN.md.
export async function GET() {
  const orgId = await getOrgId();
  const since = new Date(Date.now() - WINDOW_HOURS * 3600 * 1000);

  // Per-action minutes — counted once an action has actually executed.
  const [actionAgg] = await db()
    .select({
      windowMin: sql<number>`coalesce(sum(${action_request.minutes_saved}) filter (where ${action_execution.finished_at} >= ${since}), 0)::int`,
      totalMin: sql<number>`coalesce(sum(${action_request.minutes_saved}), 0)::int`,
      windowTasks: sql<number>`count(*) filter (where ${action_execution.finished_at} >= ${since})::int`,
      firstAt: sql<string | null>`min(${action_execution.finished_at})::text`,
    })
    .from(action_request)
    .innerJoin(
      action_execution,
      eq(action_execution.action_request_id, action_request.id),
    )
    .where(
      and(
        eq(action_request.org_id, orgId),
        eq(action_request.status, "executed"),
      ),
    );

  // Per-run analysis minutes — counted for completed runs (Ask + workflow).
  const [analysisAgg] = await db()
    .select({
      windowMin: sql<number>`coalesce(sum(${work_run.analysis_minutes_saved}) filter (where ${work_run.finished_at} >= ${since}), 0)::int`,
      totalMin: sql<number>`coalesce(sum(${work_run.analysis_minutes_saved}), 0)::int`,
      windowTasks: sql<number>`count(*) filter (where ${work_run.finished_at} >= ${since} and ${work_run.analysis_minutes_saved} > 0)::int`,
      firstAt: sql<string | null>`min(${work_run.finished_at}) filter (where ${work_run.analysis_minutes_saved} > 0)::text`,
    })
    .from(work_run)
    .where(and(eq(work_run.org_id, orgId), eq(work_run.status, "completed")));

  const [org] = await db()
    .select({ createdAt: organization.created_at })
    .from(organization)
    .where(eq(organization.id, orgId))
    .limit(1);

  // Per-day minutes for the last 7 days — feeds the hero sparkline. Mirrors the
  // workflow activity-sparkline bucketing.
  const dayStart = new Date();
  dayStart.setUTCHours(0, 0, 0, 0);
  dayStart.setUTCDate(dayStart.getUTCDate() - (DAILY_DAYS - 1));

  const actionDaily = await db()
    .select({
      day: sql<string>`date_trunc('day', ${action_execution.finished_at})::date::text`,
      min: sql<number>`coalesce(sum(${action_request.minutes_saved}), 0)::int`,
    })
    .from(action_request)
    .innerJoin(
      action_execution,
      eq(action_execution.action_request_id, action_request.id),
    )
    .where(
      and(
        eq(action_request.org_id, orgId),
        eq(action_request.status, "executed"),
        gte(action_execution.finished_at, dayStart),
      ),
    )
    .groupBy(sql`date_trunc('day', ${action_execution.finished_at})`);

  const analysisDaily = await db()
    .select({
      day: sql<string>`date_trunc('day', ${work_run.finished_at})::date::text`,
      min: sql<number>`coalesce(sum(${work_run.analysis_minutes_saved}), 0)::int`,
    })
    .from(work_run)
    .where(
      and(
        eq(work_run.org_id, orgId),
        eq(work_run.status, "completed"),
        gte(work_run.finished_at, dayStart),
      ),
    )
    .groupBy(sql`date_trunc('day', ${work_run.finished_at})`);

  const byDay = new Map<string, number>();
  for (const r of [...actionDaily, ...analysisDaily]) {
    byDay.set(r.day, (byDay.get(r.day) ?? 0) + r.min);
  }
  const dailyMinutes = fillDailySeries(byDay, dayStart, DAILY_DAYS);

  // Honest "since": the first date we actually tracked a saved minute, not the
  // org's install date (which would overstate the window for orgs that predate
  // the feature). Falls back to install date when nothing is tracked yet.
  const firstTracked = [actionAgg?.firstAt, analysisAgg?.firstAt]
    .filter((s): s is string => typeof s === "string")
    .map((s) => new Date(s))
    .sort((a, b) => a.getTime() - b.getTime())[0];
  const sinceISO =
    (firstTracked ?? org?.createdAt ?? null)?.toISOString() ?? null;

  const windowMinutes = (actionAgg?.windowMin ?? 0) + (analysisAgg?.windowMin ?? 0);
  const totalMinutes = (actionAgg?.totalMin ?? 0) + (analysisAgg?.totalMin ?? 0);
  const windowTasks =
    (actionAgg?.windowTasks ?? 0) + (analysisAgg?.windowTasks ?? 0);

  return NextResponse.json({
    windowHours: WINDOW_HOURS,
    windowMinutes,
    totalMinutes,
    windowTasks,
    dailyMinutes,
    sinceISO,
  });
}
