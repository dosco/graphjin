import { NextRequest, NextResponse } from "next/server";
import {
  and,
  db,
  eq,
  inArray,
  onboarding_wizard,
  organization,
  processing_job,
  sql,
} from "@neko/db";
import { enqueue, QUEUE } from "@neko/db/jobs";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import {
  AGENT_RUNTIME_UNAVAILABLE_CODE,
  AgentRuntimeUnavailableError,
  requireAgentRuntimeReady,
} from "@/lib/agent-runtime-readiness";
import { hasPrimaryProviderSetup } from "@/lib/provider-settings";

export async function POST(request: NextRequest) {
  // Same gate as /settings/finish: onboarding writes org-level state and
  // kicks off a profile build — not something an unauthenticated visitor
  // may do once SSO is live.
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;

  const body = await request.json();
  const {
    companyName,
    companyNote,
    fiscalYearStartMonth,
    activeSeats,
    priorities,
  } = body as {
    companyName: string;
    companyNote: string;
    fiscalYearStartMonth: number;
    activeSeats: string[];
    priorities: string[];
  };

  const trimmedName = typeof companyName === "string" ? companyName.trim() : "";
  if (!trimmedName) {
    return NextResponse.json(
      { error: "companyName is required" },
      { status: 400 },
    );
  }

  const orgId = await getOrgId();

  if (!(await hasPrimaryProviderSetup(orgId))) {
    return NextResponse.json(
      {
        error:
          "The primary model provider is not configured. Open Agent settings, save a provider, then try again.",
        code: "primary_provider_unavailable",
      },
      { status: 409 },
    );
  }

  try {
    await requireAgentRuntimeReady(orgId);
  } catch (error) {
    if (error instanceof AgentRuntimeUnavailableError) {
      return NextResponse.json(
        { error: error.message, code: AGENT_RUNTIME_UNAVAILABLE_CODE },
        { status: 503 },
      );
    }
    throw error;
  }

  // One transaction for the whole submit: the wizard-row replace was a
  // delete + insert in separate statements (org_id is the PK — two
  // concurrent submits hit a duplicate-key 500 or lost the row), and
  // nothing stopped a second submit from stacking a second profile-build
  // chain. The advisory lock serializes double-clicks and client retries;
  // the in-flight check makes the second one a no-op.
  const outcome = await db().transaction(async (tx) => {
    await tx.execute(
      sql`select pg_advisory_xact_lock(hashtext(${"openneko.onboarding:" + orgId}))`,
    );
    const inflight = await tx
      .select({ id: processing_job.id })
      .from(processing_job)
      .where(
        and(
          eq(processing_job.org_id, orgId),
          eq(processing_job.kind, "business_profile_build"),
          inArray(processing_job.status, ["queued", "running"]),
        ),
      )
      .limit(1);
    if (inflight[0]) return { inflightJobId: inflight[0].id };

    await tx
      .update(organization)
      .set({ name: trimmedName, updated_at: new Date() })
      .where(eq(organization.id, orgId));

    await tx.delete(onboarding_wizard).where(eq(onboarding_wizard.org_id, orgId));
    await tx.insert(onboarding_wizard).values({
      org_id: orgId,
      company_note: companyNote,
      fiscal_year_start_month: fiscalYearStartMonth,
      active_seats: activeSeats,
      priorities,
      step: "submitting",
      submitted_at: new Date(),
    });

    const inserted = await tx
      .insert(processing_job)
      .values({
        org_id: orgId,
        kind: "business_profile_build",
        status: "queued",
        trigger: "wizard_submit",
      })
      .returning({ id: processing_job.id });
    return { jobId: inserted[0]?.id };
  });

  if ("inflightJobId" in outcome) {
    // A concurrent submit already queued the build; report it as ours.
    return NextResponse.json({ ok: true, jobId: outcome.inflightJobId });
  }
  const jobId = outcome.jobId;
  if (!jobId) {
    return NextResponse.json(
      { error: "failed to record job" },
      { status: 500 },
    );
  }
  try {
    // The UI already waits through the profiler's bounded agent attempt.
    // Hidden queue retries make onboarding look stuck after a known failure.
    await enqueue(
      QUEUE.BUSINESS_PROFILE_BUILD,
      {
        processingJobId: jobId,
        orgId,
      },
      { retryLimit: 0 },
    );
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    await db()
      .update(processing_job)
      .set({
        status: "failed",
        error: `enqueue failed: ${msg}`,
        finished_at: new Date(),
        updated_at: new Date(),
      })
      .where(eq(processing_job.id, jobId));
    return NextResponse.json(
      { error: `enqueue failed: ${msg}` },
      { status: 500 },
    );
  }

  return NextResponse.json({ jobId });
}
