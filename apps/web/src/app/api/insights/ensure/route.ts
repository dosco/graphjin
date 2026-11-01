import { NextRequest, NextResponse } from "next/server";
import {
  and,
  customer_profile,
  db,
  eq,
  inArray,
  processing_job,
} from "@neko/db";
import { enqueue, QUEUE } from "@neko/db/jobs";
import { getOrgId } from "@/lib/db";
import { resolveResearchStatus } from "@/lib/provider-settings";

/**
 * POST /api/insights/ensure
 * Body (optional): { force?: boolean }
 *
 * Idempotently ensures an industry_insights_build job exists for the current
 * org. Used by the processing page's Next button so a broken chain (e.g. the
 * worker crashed mid-job and the chained insights job is terminally failed)
 * self-heals on user action instead of dead-ending in a polling loop.
 *
 * When `force: true`, clears the existing industry_insights column on the
 * current profile and enqueues a fresh job even if content was already
 * present. Used to regenerate a stale/malformed briefing without editing
 * the DB by hand.
 *
 * Returns one of:
 *   { state: 'ready' }          — insights already present (no force)
 *   { state: 'disabled' }       — org has research provider turned off
 *   { state: 'live', jobId }    — job already queued/running
 *   { state: 'queued', jobId }  — we just enqueued a fresh job
 */
export async function POST(request: NextRequest) {
  const body = (await request.json().catch(() => ({}))) as { force?: boolean };
  const force = body?.force === true;

  const researchStatus = await resolveResearchStatus((await getOrgId()));
  if (researchStatus === "disabled") {
    return NextResponse.json({ state: "disabled" });
  }

  const [profileRows, liveJobRows] = await Promise.all([
    db()
      .select({ industry_insights: customer_profile.industry_insights })
      .from(customer_profile)
      .where(
        and(
          eq(customer_profile.org_id, (await getOrgId())),
          eq(customer_profile.is_current, true),
        ),
      )
      .limit(1),
    db()
      .select({ id: processing_job.id })
      .from(processing_job)
      .where(
        and(
          eq(processing_job.org_id, (await getOrgId())),
          eq(processing_job.kind, "industry_insights_build"),
          inArray(processing_job.status, ["queued", "running"]),
        ),
      )
      .limit(1),
  ]);

  const hasInsights = Boolean(profileRows[0]?.industry_insights);

  if (!force && hasInsights) {
    return NextResponse.json({ state: "ready" });
  }
  if (liveJobRows.length > 0) {
    return NextResponse.json({
      state: "live",
      jobId: liveJobRows[0].id,
    });
  }

  // Force path: blank the column so the UI drops back into a loading state
  // while the new job runs. The worker will overwrite it on completion.
  if (force && hasInsights) {
    await db()
      .update(customer_profile)
      .set({ industry_insights: null })
      .where(
        and(
          eq(customer_profile.org_id, (await getOrgId())),
          eq(customer_profile.is_current, true),
        ),
      );
  }

  const inserted = await db()
    .insert(processing_job)
    .values({
      org_id: (await getOrgId()),
      kind: "industry_insights_build",
      status: "queued",
      trigger: force ? "insights_regenerate" : "processing_next_ensure",
    })
    .returning({ id: processing_job.id });
  const jobId = inserted[0]?.id;
  if (jobId) {
    await enqueue(QUEUE.INDUSTRY_INSIGHTS_BUILD, {
      processingJobId: jobId,
      orgId: (await getOrgId()),
    });
  }

  return NextResponse.json({ state: "queued", jobId });
}
