import { getAuthProvider } from "@/lib/auth";
import { NextResponse } from "next/server";
import {
  and,
  customer_profile,
  db,
  desc,
  eq,
  inArray,
  onboarding_wizard,
  operator_profile,
  processing_job,
  sql,
} from "@neko/db";
import {
  getOrgId,
  type CurrentStage,
  type MetricsProgress,
  type OnboardingStatus,
  type StageKind,
} from "@/lib/db";
import { getCurrentActor } from "@/lib/actor";
import { isDemoMode, DEMO_SEATS } from "@/lib/demo-mode";
import { profileBuildFailureMessage } from "@/lib/onboarding-failure";

const STAGE_KINDS: StageKind[] = [
  "business_profile_build",
  "industry_insights_build",
  "bootstrap_metrics_build",
  "metric_refresh",
];

function isStageKind(s: string): s is StageKind {
  return (STAGE_KINDS as string[]).includes(s);
}

/**
 * Look up the most recent in-flight chain step (queued or running) and
 * derive a {kind, message} for the UI's stage strip. Reads
 * processing_job.progress.message that the worker writes via
 * apps/worker/src/progress.ts. Returns undefined when nothing is in flight.
 */
async function loadCurrentStage(orgId: string): Promise<CurrentStage | undefined> {
  const rows = await db()
    .select({
      kind: processing_job.kind,
      progress: processing_job.progress,
    })
    .from(processing_job)
    .where(
      and(
        eq(processing_job.org_id, orgId),
        inArray(processing_job.status, ["queued", "running"]),
        inArray(processing_job.kind, STAGE_KINDS),
      ),
    )
    .orderBy(desc(processing_job.created_at))
    .limit(1);
  const r = rows[0];
  if (!r || !isStageKind(r.kind)) return undefined;
  const progress = (r.progress ?? {}) as { message?: unknown };
  const message =
    typeof progress.message === "string" && progress.message.trim().length > 0
      ? progress.message
      : null;
  return { kind: r.kind, message };
}

/**
 * Count metric_refresh jobs in flight for the org.
 *
 * Single-tenant simplification: we don't try to scope by "this onboarding
 * run" via timestamps — at most one bootstrap chain runs at a time per
 * org, so the queued/running/recently-completed metric_refresh jobs are
 * all relevant. completed/failed counts include only those tied to the
 * latest bootstrap_metrics_build run; jobs older than that are excluded
 * by joining through created_at >= bootstrap.finished_at.
 */
async function loadMetricsProgress(orgId: string): Promise<MetricsProgress | undefined> {
  // Most-recent bootstrap_metrics_build for this org. If none, no progress
  // to report — the user hasn't reached that stage yet.
  const bootstrapRows = await db()
    .select({ created_at: processing_job.created_at })
    .from(processing_job)
    .where(
      and(
        eq(processing_job.org_id, orgId),
        eq(processing_job.kind, "bootstrap_metrics_build"),
      ),
    )
    .orderBy(desc(processing_job.created_at))
    .limit(1);
  const since = bootstrapRows[0]?.created_at;
  if (!since) return undefined;

  // Aggregate by status. Running + queued count toward "total"; the
  // dashboard banner shows completed+failed against total.
  const counts = await db()
    .select({
      status: processing_job.status,
      n: sql<number>`count(*)::int`.as("n"),
    })
    .from(processing_job)
    .where(
      and(
        eq(processing_job.org_id, orgId),
        eq(processing_job.kind, "metric_refresh"),
        sql`${processing_job.created_at} >= ${since}`,
      ),
    )
    .groupBy(processing_job.status);

  const buckets = { queued: 0, running: 0, succeeded: 0, failed: 0, cancelled: 0 } as Record<string, number>;
  for (const row of counts) buckets[row.status] = (buckets[row.status] ?? 0) + Number(row.n);
  const total = buckets.queued + buckets.running + buckets.succeeded + buckets.failed + buckets.cancelled;
  if (total === 0) return undefined;
  return { total, completed: buckets.succeeded, failed: buckets.failed };
}


/**
 * GET /api/onboarding/status
 *
 * Decides which screen to show:
 *  - needs_wizard : no profile, no in-flight job, no recent failure → /onboarding
 *  - needs_persona: org profile exists but the signed-in user has no persona
 *  - processing   : profile build job is running → /business-profile
 *  - ready        : a current profile exists → /
 *  - failed       : the most recent profile build failed and there's no
 *                   newer in-flight job → /onboarding with a toast. The UI
 *                   surfaces a safe failure message instead of silently
 *                   bouncing back to the wizard.
 */
export async function GET() {
  if (isDemoMode()) {
    return NextResponse.json({
      state: "ready",
      profileVersion: 0,
      seats: [...DEMO_SEATS],
      mode: "shared",
    } satisfies OnboardingStatus);
  }

  const orgId = await getOrgId();

  let profileRows: Array<{ id: string; version: number }>;
  let jobRows: Array<{ id: string; status: string }>;
  let wizardRows: Array<{ active_seats: string[] | null }>;
  let lastJobRows: Array<{ id: string; status: string; error: string | null }>;
  try {
    [profileRows, jobRows, wizardRows, lastJobRows] = await Promise.all([
      db()
        .select({ id: customer_profile.id, version: customer_profile.version })
        .from(customer_profile)
        .where(
          and(
            eq(customer_profile.org_id, orgId),
            eq(customer_profile.is_current, true),
          ),
        )
        .limit(1),
      db()
        .select({ id: processing_job.id, status: processing_job.status })
        .from(processing_job)
        .where(
          and(
            eq(processing_job.org_id, orgId),
            eq(processing_job.kind, "business_profile_build"),
            inArray(processing_job.status, ["queued", "running"]),
          ),
        )
        .orderBy(desc(processing_job.created_at))
        .limit(1),
      db()
        .select({ active_seats: onboarding_wizard.active_seats })
        .from(onboarding_wizard)
        .where(eq(onboarding_wizard.org_id, orgId))
        .limit(1),
      db()
        .select({
          id: processing_job.id,
          status: processing_job.status,
          error: processing_job.error,
        })
        .from(processing_job)
        .where(
          and(
            eq(processing_job.org_id, orgId),
            eq(processing_job.kind, "business_profile_build"),
          ),
        )
        .orderBy(desc(processing_job.created_at))
        .limit(1),
    ]);
  } catch (err) {
    const message = err instanceof Error ? err.message : "Database unavailable";
    return NextResponse.json(
      { state: "db_error", message } satisfies OnboardingStatus,
      { status: 503 },
    );
  }

  let status: OnboardingStatus;
  if (profileRows.length > 0) {
    const actor = await getCurrentActor();
    // A persisted solo identity does not opt an existing workspace into SSO personas.
    const personaUserId = (await getAuthProvider()) ? actor.userId : null;
    if (personaUserId) {
      const ownPersona = await db()
        .select({ roleTemplate: operator_profile.role_template })
        .from(operator_profile)
        .where(
          and(
            eq(operator_profile.org_id, orgId),
            eq(operator_profile.user_id, personaUserId),
          ),
        )
        .limit(1);
      if (!ownPersona[0]?.roleTemplate.trim()) {
        return NextResponse.json({
          state: "needs_persona",
        } satisfies OnboardingStatus);
      }
    }

    // Even after the profile is ready, metric_refresh jobs may still be
    // running in the background. Surface that count so the dashboard can
    // render its "X of Y still loading" banner.
    const metricsProgress = await loadMetricsProgress(orgId).catch(() => undefined);
    status = {
      state: "ready",
      profileVersion: profileRows[0].version,
      seats: personaUserId ? [] : (wizardRows[0]?.active_seats ?? []),
      mode: personaUserId ? "personal" : "shared",
      ...(metricsProgress ? { metricsProgress } : {}),
    };
  } else if (jobRows.length > 0) {
    const [currentStage, metricsProgress] = await Promise.all([
      loadCurrentStage(orgId).catch(() => undefined),
      loadMetricsProgress(orgId).catch(() => undefined),
    ]);
    status = {
      state: "processing",
      jobId: jobRows[0].id,
      ...(currentStage ? { currentStage } : {}),
      ...(metricsProgress ? { metricsProgress } : {}),
    };
  } else if (lastJobRows.length > 0 && lastJobRows[0].status === "failed") {
    status = {
      state: "failed",
      jobId: lastJobRows[0].id,
      message: profileBuildFailureMessage(lastJobRows[0].error),
    };
  } else {
    status = { state: "needs_wizard" };
  }
  return NextResponse.json(status);
}
