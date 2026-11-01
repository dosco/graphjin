import { NextRequest, NextResponse } from "next/server";
import { and, db, eq, metric } from "@neko/db";
import { getOrgId } from "@/lib/db";
import { requireItem } from "@/lib/entitlements";

/**
 * PATCH /api/briefing/pin
 *
 * Toggle a metric's briefing membership by flipping `active`.
 * Body: { metricId: string, active: boolean }
 */
export async function PATCH(request: NextRequest) {
  const { metricId, active } = (await request.json()) as {
    metricId: string;
    active: boolean;
  };

  if (!metricId || typeof active !== "boolean") {
    return NextResponse.json({ error: "metricId and active required" }, { status: 400 });
  }
  const deniedMetric = await requireItem("metric", metricId, { notFound: "metric not found" });
  if (deniedMetric) return deniedMetric;

  await db()
    .update(metric)
    .set({ active })
    .where(and(eq(metric.id, metricId), eq(metric.org_id, (await getOrgId()))));

  return NextResponse.json({ ok: true });
}
