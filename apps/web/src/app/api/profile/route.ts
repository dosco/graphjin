import { NextRequest, NextResponse } from "next/server";
import {
  and,
  customer_profile,
  db,
  eq,
  inArray,
  processing_job,
} from "@neko/db";
import { getOrgId } from "@/lib/db";
import { resolveResearchStatus } from "@/lib/provider-settings";

const MAX_FIELD_LENGTH = 200_000;

/**
 * GET /api/profile
 *
 * Returns the current business profile and industry insights.
 * Called by the processing page during the onboarding reveal flow.
 */
export async function GET() {
  const researchStatus = await resolveResearchStatus((await getOrgId()));

  const [profileRows, jobRows] = await Promise.all([
    db()
      .select({
        business_profile: customer_profile.business_profile,
        industry_insights: customer_profile.industry_insights,
        company_note: customer_profile.company_note,
      })
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

  const profile = profileRows[0];
  if (!profile) {
    return NextResponse.json({ error: "no profile" }, { status: 404 });
  }

  return NextResponse.json({
    businessProfile: profile.business_profile ?? "",
    industryInsights: profile.industry_insights ?? "",
    companyNote: profile.company_note ?? "",
    industryInsightsStatus:
      profile.industry_insights
        ? "ready"
        : researchStatus === "disabled"
          ? "disabled"
          : jobRows.length > 0
            ? "processing"
            : "pending",
  });
}

/**
 * PATCH /api/profile
 * Body: { businessProfile?: string; industryInsights?: string }
 *
 * Updates the current customer_profile row for the org. Only fields present in
 * the body (and of type string) are written, so omitting a key preserves the
 * existing column value. Used by the inline editor on the business-profile
 * page; sent as a debounced save after the user pauses typing or clicks out.
 */
export async function PATCH(request: NextRequest) {
  const body = (await request.json().catch(() => null)) as
    | { businessProfile?: unknown; industryInsights?: unknown }
    | null;
  if (!body || typeof body !== "object") {
    return NextResponse.json({ error: "invalid body" }, { status: 400 });
  }

  const update: { business_profile?: string; industry_insights?: string; updated_at: Date } = {
    updated_at: new Date(),
  };
  if (typeof body.businessProfile === "string") {
    if (body.businessProfile.length > MAX_FIELD_LENGTH) {
      return NextResponse.json({ error: "businessProfile too large" }, { status: 413 });
    }
    update.business_profile = body.businessProfile;
  }
  if (typeof body.industryInsights === "string") {
    if (body.industryInsights.length > MAX_FIELD_LENGTH) {
      return NextResponse.json({ error: "industryInsights too large" }, { status: 413 });
    }
    update.industry_insights = body.industryInsights;
  }
  if (update.business_profile === undefined && update.industry_insights === undefined) {
    return NextResponse.json({ error: "no fields to update" }, { status: 400 });
  }

  const orgId = await getOrgId();
  const rows = await db()
    .update(customer_profile)
    .set(update)
    .where(
      and(
        eq(customer_profile.org_id, orgId),
        eq(customer_profile.is_current, true),
      ),
    )
    .returning({
      business_profile: customer_profile.business_profile,
      industry_insights: customer_profile.industry_insights,
    });

  if (rows.length === 0) {
    return NextResponse.json({ error: "no profile" }, { status: 404 });
  }

  return NextResponse.json({
    businessProfile: rows[0].business_profile ?? "",
    industryInsights: rows[0].industry_insights ?? "",
  });
}
