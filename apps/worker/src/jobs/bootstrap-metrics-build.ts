import {
  and,
  customer_profile,
  db,
  eq,
  metric,
  onboarding_wizard,
  organization,
  processing_job,
} from "@neko/db";
import { enqueue, QUEUE } from "@neko/db/jobs";
import { updateProgress } from "../progress.js";
import { createWorkerHarnessObserver } from "../telemetry.js";
import {
  ensureHostConfigProvisioned,
  runBootstrapMetricsWriter,
} from "@neko/llm";

/**
 * bootstrap_metrics_build job handler.
 *
 * Reads the current customer_profile (must have business_profile and
 * industry_insights) and seeds the metric table with 4 starter dashboard cards
 * per CXO seat the user selected in onboarding.
 *
 * Ensure semantics: this job ONLY seeds personas that have no metric rows yet.
 * A persona with any existing rows (active or inactive) is considered already
 * bootstrapped and is skipped entirely.
 */
export async function runBootstrapMetricsBuild(jobId: string, orgId: string) {
  await updateProgress(jobId, "Loading profile");

  const [orgRows, profileRows, wizardRows] = await Promise.all([
    db()
      .select({ name: organization.name })
      .from(organization)
      .where(eq(organization.id, orgId))
      .limit(1),
    db()
      .select({
        id: customer_profile.id,
        business_profile: customer_profile.business_profile,
        industry_insights: customer_profile.industry_insights,
      })
      .from(customer_profile)
      .where(
        and(
          eq(customer_profile.org_id, orgId),
          eq(customer_profile.is_current, true),
        ),
      )
      .limit(1),
    db()
      .select({ active_seats: onboarding_wizard.active_seats })
      .from(onboarding_wizard)
      .where(eq(onboarding_wizard.org_id, orgId))
      .limit(1),
  ]);

  const profile = profileRows[0];
  if (!profile) {
    throw new Error(
      `no current customer_profile for org ${orgId} — run business_profile_build first`,
    );
  }
  if (!profile.business_profile) {
    throw new Error(`customer_profile ${profile.id} has no business_profile`);
  }
  const seats = wizardRows[0]?.active_seats ?? [];
  if (seats.length === 0) {
    throw new Error(
      `no active_seats on onboarding_wizard for org ${orgId} — wizard not submitted?`,
    );
  }
  const orgName = orgRows[0]?.name ?? orgId;

  // Load every existing metric for the org.
  const existingRows = await db()
    .select({ role: metric.role, slug: metric.slug })
    .from(metric)
    .where(eq(metric.org_id, orgId));
  const seededRoles = new Set(existingRows.map((m) => m.role));
  const existing = new Set(existingRows.map((m) => `${m.role}::${m.slug}`));

  const seatsNeedingSeed = seats.filter((role) => !seededRoles.has(role));
  const seatsAlreadySeeded = seats.filter((role) => seededRoles.has(role));

  if (seatsNeedingSeed.length === 0) {
    console.log(
      `[bootstrap-metrics] org=${orgId} all ${seats.length} seat(s) already bootstrapped — nothing to do`,
    );
    await updateProgress(jobId, "Bootstrap metrics already present");
    return;
  }
  if (seatsAlreadySeeded.length > 0) {
    console.log(
      `[bootstrap-metrics] org=${orgId} skipping already-seeded seats: ${seatsAlreadySeeded.join(", ")}`,
    );
  }

  await updateProgress(jobId, "Generating cards");

  await ensureHostConfigProvisioned(orgId);
  const { metrics } = await runBootstrapMetricsWriter({
    orgId,
    orgName,
    businessProfile: profile.business_profile,
    industryInsights: profile.industry_insights ?? "",
    seats: seatsNeedingSeed,
    jobId,
    onProgress: async (note) => {
      await updateProgress(jobId, note);
    },
    observer: createWorkerHarnessObserver(jobId).observer,
  });

  await updateProgress(jobId, "Saving cards");

  let inserted = 0;
  let skipped = 0;
  const newMetricIds: string[] = [];
  for (const m of metrics) {
    const key = `${m.role}::${m.slug}`;
    if (existing.has(key)) {
      skipped++;
      continue;
    }
    const ins = await db()
      .insert(metric)
      .values({
        org_id: orgId,
        role: m.role,
        slug: m.slug,
        source: "bootstrap",
        title: m.title,
        description: m.why,
        why: m.why,
        chart_hint: m.chart_hint,
        created_by_job: jobId,
        // Mark pending so the dashboard shows skeletons until the chained
        // metric_refresh jobs (enqueued below) succeed or fail.
        last_refresh_status: "pending",
      })
      .returning({ id: metric.id });
    const newId = ins[0]?.id;
    if (newId) newMetricIds.push(newId);
    inserted++;
  }

  console.log(
    `[bootstrap-metrics] org=${orgId} inserted=${inserted} skipped=${skipped} (of ${metrics.length} generated)`,
  );

  // Chain a metric_refresh job per newly inserted card.
  for (const metricId of newMetricIds) {
    const ins = await db()
      .insert(processing_job)
      .values({
        org_id: orgId,
        kind: "metric_refresh",
        status: "queued",
        trigger: "chain_after_bootstrap_metrics_build",
        trigger_payload: { metricId },
      })
      .returning({ id: processing_job.id });
    const nextJobId = ins[0]?.id;
    if (!nextJobId) continue;
    await enqueue(QUEUE.METRIC_REFRESH, { processingJobId: nextJobId, orgId });
  }
  console.log(
    `[bootstrap-metrics] chained ${newMetricIds.length} metric_refresh job(s)`,
  );
}
