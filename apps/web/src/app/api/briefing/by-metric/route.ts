import { NextRequest, NextResponse } from "next/server";
import {
  and,
  db,
  desc,
  eq,
  metric,
  metric_snapshot,
} from "@neko/db";
import { getOrgId } from "@/lib/db";
import { requireItem } from "@/lib/entitlements";

/**
 * GET /api/briefing/by-metric?metricId=<uuid>
 *
 * Returns the latest snapshot for a single metric, scoped to the current
 * org. Used by the chat-history hydrator: localStorage only stores
 * { id, type, text?, metricId? } per chat message and on page load we
 * rehydrate AI messages from the server here. Volatile card data
 * (headline, chart, detail) lives in the DB; the client never persists
 * its own copy.
 *
 * Response:
 *   404 if the metric doesn't exist or belongs to another org
 *   200 { metricId, title, source, payload }
 *       payload may be null when the metric exists but no snapshot has
 *       landed yet (job still queued/running) — caller can render a
 *       skeleton in that case.
 */
export async function GET(request: NextRequest) {
  const metricId = request.nextUrl.searchParams.get("metricId");
  if (!metricId) {
    return NextResponse.json({ error: "missing metricId" }, { status: 400 });
  }
  const deniedMetric = await requireItem("metric", metricId, { notFound: "metric not found" });
  if (deniedMetric) return deniedMetric;

  const orgId = await getOrgId();
  const metricRows = await db()
    .select({
      id: metric.id,
      title: metric.title,
      source: metric.source,
      last_refresh_status: metric.last_refresh_status,
      last_refresh_error: metric.last_refresh_error,
    })
    .from(metric)
    .where(and(eq(metric.id, metricId), eq(metric.org_id, orgId)))
    .limit(1);
  const m = metricRows[0];
  if (!m) {
    return NextResponse.json({ error: "metric not found" }, { status: 404 });
  }

  const snapRows = await db()
    .select({ payload: metric_snapshot.payload })
    .from(metric_snapshot)
    .where(eq(metric_snapshot.metric_id, m.id))
    .orderBy(desc(metric_snapshot.captured_at))
    .limit(1);
  const payload = (snapRows[0]?.payload as Record<string, unknown> | null) ?? null;

  return NextResponse.json({
    metricId: m.id,
    title: m.title,
    source: m.source,
    refreshStatus: m.last_refresh_status,
    refreshError: m.last_refresh_error,
    payload,
  });
}
