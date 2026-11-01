import {
  and,
  customer_profile,
  db,
  eq,
  organization,
} from "@neko/db";
import { updateProgress } from "../progress.js";
import { ensureHostConfigProvisioned, runIndustryResearcher } from "@neko/llm";

/**
 * industry_insights_build job handler.
 *
 * Consumes the org's *current* customer_profile (which must already have a
 * business_profile populated by a prior business_profile_build run) and uses
 * Perplexity sonar-deep-research to write the industry_insights column on
 * that same row in place. No new customer_profile version is created — the
 * profile is the durable artifact and industry_insights is one of its fields.
 */
export async function runIndustryInsightsBuild(jobId: string, orgId: string) {
  // 1. Load the current customer_profile + org name.
  const [orgRows, profileRows] = await Promise.all([
    db()
      .select({ name: organization.name })
      .from(organization)
      .where(eq(organization.id, orgId))
      .limit(1),
    db()
      .select({
        id: customer_profile.id,
        version: customer_profile.version,
        business_profile: customer_profile.business_profile,
        company_note: customer_profile.company_note,
      })
      .from(customer_profile)
      .where(
        and(
          eq(customer_profile.org_id, orgId),
          eq(customer_profile.is_current, true),
        ),
      )
      .limit(1),
  ]);

  const profile = profileRows[0];
  if (!profile) {
    throw new Error(
      `no current customer_profile for org ${orgId} — run business_profile_build first`,
    );
  }
  if (!profile.business_profile) {
    throw new Error(
      `customer_profile ${profile.id} has no business_profile — cannot research industry`,
    );
  }
  const orgName = orgRows[0]?.name ?? orgId;
  const companyNote = profile.company_note ?? "";

  await updateProgress(jobId, "Loading business profile");

  // 2. Run the researcher (mission writer + sonar-deep-research).
  await ensureHostConfigProvisioned(orgId);
  const { industryInsights, missionCharter } = await runIndustryResearcher({
    orgId,
    orgName,
    companyNote,
    businessProfile: profile.business_profile,
    onProgress: async (note) => {
      await updateProgress(jobId, note);
    },
  });

  await updateProgress(jobId, "Saving industry insights");

  // 3. Update the same customer_profile row in place.
  await db()
    .update(customer_profile)
    .set({
      industry_insights: industryInsights || null,
      industry_insights_research_task: missionCharter || null,
    })
    .where(eq(customer_profile.id, profile.id));
}
