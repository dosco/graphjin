import { NextRequest, NextResponse } from "next/server";
import {
  and,
  db,
  desc,
  eq,
  inArray,
  metric,
  processing_job,
} from "@neko/db";
import { enqueue, QUEUE } from "@neko/db/jobs";
import { getOrgId } from "@/lib/db";
import { requireItem } from "@/lib/entitlements";

export async function POST(request: NextRequest) {
  let body: { metricId?: string };
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "invalid JSON body" }, { status: 400 });
  }
  const metricId = body.metricId;
  if (!metricId || typeof metricId !== "string") {
    return NextResponse.json(
      { error: "metricId is required" },
      { status: 400 },
    );
  }
  const deniedMetric = await requireItem("metric", metricId, { notFound: "metric not found" });
  if (deniedMetric) return deniedMetric;

  const orgId = await getOrgId();

  const metricRows = await db()
    .select({ id: metric.id })
    .from(metric)
    .where(and(eq(metric.id, metricId), eq(metric.org_id, orgId)))
    .limit(1);
  if (!metricRows[0]) {
    return NextResponse.json({ error: "metric not found" }, { status: 404 });
  }

  const inFlight = await db()
    .select({ id: processing_job.id })
    .from(processing_job)
    .where(
      and(
        eq(processing_job.org_id, orgId),
        eq(processing_job.kind, "metric_refresh"),
        inArray(processing_job.status, ["queued", "running"]),
      ),
    )
    .orderBy(desc(processing_job.created_at))
    .limit(20);

  for (const job of inFlight) {
    const row = await db()
      .select({ trigger_payload: processing_job.trigger_payload })
      .from(processing_job)
      .where(eq(processing_job.id, job.id))
      .limit(1);
    const payload = row[0]?.trigger_payload as { metricId?: string } | null;
    if (payload?.metricId === metricId) {
      await db()
        .update(metric)
        .set({
          last_refresh_status: "pending",
          last_refresh_error: null,
          last_refresh_job_id: job.id,
          updated_at: new Date(),
        })
        .where(eq(metric.id, metricId));
      return NextResponse.json({ jobId: job.id, alreadyRunning: true });
    }
  }

  const inserted = await db()
    .insert(processing_job)
    .values({
      org_id: orgId,
      kind: "metric_refresh",
      status: "queued",
      trigger: "retry",
      trigger_payload: { metricId },
    })
    .returning({ id: processing_job.id });
  const jobId = inserted[0]?.id;
  if (!jobId) {
    return NextResponse.json(
      { error: "failed to enqueue retry" },
      { status: 500 },
    );
  }

  await db()
    .update(metric)
    .set({
      last_refresh_status: "pending",
      last_refresh_error: null,
      last_refresh_job_id: jobId,
      updated_at: new Date(),
    })
    .where(eq(metric.id, metricId));

  try {
    await enqueue(QUEUE.METRIC_REFRESH, { processingJobId: jobId, orgId });
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    const now = new Date();
    await db()
      .update(processing_job)
      .set({ status: "failed", error: `enqueue failed: ${msg}`, finished_at: now, updated_at: now })
      .where(eq(processing_job.id, jobId));
    await db()
      .update(metric)
      .set({ last_refresh_status: "failed", last_refresh_error: `enqueue failed: ${msg}`, updated_at: now })
      .where(eq(metric.id, metricId));
    return NextResponse.json(
      { error: `enqueue failed: ${msg}` },
      { status: 500 },
    );
  }

  return NextResponse.json({ jobId, alreadyRunning: false });
}
