import { NextRequest, NextResponse } from "next/server";
import {
  and,
  db,
  desc,
  eq,
  metric,
  metric_snapshot,
  processing_job,
} from "@neko/db";
import { getOrgId } from "@/lib/db";

/**
 * GET /api/briefing/status?jobId=X
 *
 * Polls a metric_refresh job's status. Returns:
 *   - running:   { status, progress }
 *   - succeeded: { status, payload } (full MetricAgentResult from snapshot)
 *   - failed:    { status, error }
 *   - queued:    { status }
 */
export async function GET(request: NextRequest) {
  const jobId = request.nextUrl.searchParams.get("jobId");
  if (!jobId) {
    return NextResponse.json({ error: "missing jobId" }, { status: 400 });
  }

  const jobRows = await db()
    .select({
      status: processing_job.status,
      progress: processing_job.progress,
      error: processing_job.error,
      trigger_payload: processing_job.trigger_payload,
    })
    .from(processing_job)
    .where(
      and(eq(processing_job.id, jobId), eq(processing_job.org_id, (await getOrgId()))),
    )
    .limit(1);

  const job = jobRows[0] as
    | {
        status: string;
        progress: { message?: string } | null;
        error: string | null;
        trigger_payload: { slug?: string } | null;
      }
    | undefined;
  if (!job) {
    return NextResponse.json({ error: "job not found" }, { status: 404 });
  }

  if (job.status === "succeeded") {
    // Find the snapshot written by this job via the metric row it created.
    // For chat metrics: metric.created_by_job = jobId.
    // For bootstrap cards: trigger_payload.metricId → metric_snapshot.
    const metricRows = await db()
      .select({ id: metric.id, title: metric.title, source: metric.source })
      .from(metric)
      .where(eq(metric.created_by_job, jobId))
      .limit(1);
    const metricRow = metricRows[0];
    let payload: Record<string, unknown> | null = null;
    if (metricRow) {
      const snapRows = await db()
        .select({
          status: metric_snapshot.status,
          payload: metric_snapshot.payload,
        })
        .from(metric_snapshot)
        .where(eq(metric_snapshot.metric_id, metricRow.id))
        .orderBy(desc(metric_snapshot.captured_at))
        .limit(1);
      payload = (snapRows[0]?.payload as Record<string, unknown> | null) ?? null;
    }
    return NextResponse.json({
      status: "succeeded",
      metricId: metricRow?.id ?? null,
      title: metricRow?.title ?? null,
      source: metricRow?.source ?? null,
      payload,
    });
  }

  if (job.status === "failed") {
    return NextResponse.json({
      status: "failed",
      error: job.error ?? "unknown error",
    });
  }

  return NextResponse.json({
    status: job.status,
    progress: job.progress,
  });
}
