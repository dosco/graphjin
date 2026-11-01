import {
  and,
  customer_profile,
  data_source,
  db,
  desc,
  eq,
  onboarding_wizard,
  organization,
  processing_job,
} from "@neko/db";
import { enqueue, QUEUE } from "@neko/db/jobs";
import { updateProgress } from "../progress.js";
import { createWorkerHarnessObserver } from "../telemetry.js";
import {
  ensureHostConfigProvisioned,
  resolveResearchProviderConfig,
  runProfiler,
} from "@neko/llm";

/**
 * business_profile_build job handler.
 *
 * 1. Load the org's data source (mcp_url) and wizard company_note.
 * 2. Run the profiler agent through Ax, query via the native broker, and
 *    synthesizes a markdown business profile.
 * 3. Retire the previous current customer_profile, insert the new one.
 * 4. Chain: when research is on, enqueue an industry_insights_build job.
 *    The industry researcher consumes the just-built business_profile and
 *    updates the same customer_profile row in place. Metrics are not built
 *    here; bootstrap_metrics_build runs only when something asks for it.
 */
export async function runBusinessProfileBuild(jobId: string, orgId: string) {
  // 1. Load inputs from the metadata DB.
  const [orgRows, sourceRows, wizardRows] = await Promise.all([
    db()
      .select({ name: organization.name })
      .from(organization)
      .where(eq(organization.id, orgId))
      .limit(1),
    db()
      .select({
        mcp_url: data_source.mcp_url,
        graphql_url: data_source.graphql_url,
      })
      .from(data_source)
      .where(eq(data_source.org_id, orgId))
      .orderBy(desc(data_source.is_default), data_source.created_at)
      .limit(1),
    db()
      .select({ company_note: onboarding_wizard.company_note })
      .from(onboarding_wizard)
      .where(eq(onboarding_wizard.org_id, orgId))
      .limit(1),
  ]);

  const source = sourceRows[0];
  if (!source?.mcp_url) {
    throw new Error(`no mcp_url configured for org ${orgId}`);
  }
  const orgName = orgRows[0]?.name ?? orgId;
  const companyNote = wizardRows[0]?.company_note ?? "";

  await updateProgress(jobId, "Reading data source");

  // 2. Run the profiler through Ax — the same runtime the metric agent uses.
  await ensureHostConfigProvisioned(orgId);
  const { businessProfile } = await runProfiler({
    orgId,
    mcpUrl: source.mcp_url,
    orgName,
    companyNote,
    jobId,
    onProgress: async (note) => {
      await updateProgress(jobId, note);
    },
    observer: createWorkerHarnessObserver(jobId).observer,
  });

  await updateProgress(jobId, "Saving profile");

  // 3. Retire the old current profile.
  await db()
    .update(customer_profile)
    .set({ is_current: false })
    .where(
      and(
        eq(customer_profile.org_id, orgId),
        eq(customer_profile.is_current, true),
      ),
    );

  // 4. Pick the next version number.
  const versionRows = await db()
    .select({ version: customer_profile.version })
    .from(customer_profile)
    .where(eq(customer_profile.org_id, orgId))
    .orderBy(desc(customer_profile.version))
    .limit(1);
  const nextVersion = (versionRows[0]?.version ?? 0) + 1;

  // 5. Insert the new profile.
  await db().insert(customer_profile).values({
    org_id: orgId,
    version: nextVersion,
    is_current: true,
    company_note: companyNote,
    business_profile: businessProfile,
    built_by_job: jobId,
  });

  // 6. Chain industry insights when research is on.
  const research = await resolveResearchProviderConfig(orgId);
  if (research.enabled && research.provider !== "disabled") {
    await chainNext(orgId, "industry_insights_build", QUEUE.INDUSTRY_INSIGHTS_BUILD, "chain_after_business_profile_build");
  } else {
    console.log(
      `[business_profile_build] research disabled for org ${orgId}; skipping industry_insights_build`,
    );
  }
}

async function chainNext(
  orgId: string,
  kind: "industry_insights_build",
  queue: (typeof QUEUE)[keyof typeof QUEUE],
  trigger: string,
) {
  const inserted = await db()
    .insert(processing_job)
    .values({
      org_id: orgId,
      kind,
      status: "queued",
      trigger,
    })
    .returning({ id: processing_job.id });
  const nextJobId = inserted[0]?.id;
  if (!nextJobId) {
    console.warn(`[business_profile_build] failed to enqueue ${kind} for org ${orgId}`);
    return;
  }
  await enqueue(queue, { processingJobId: nextJobId, orgId });
  console.log(`[business_profile_build] chained ${kind} job ${nextJobId} for org ${orgId}`);
}
