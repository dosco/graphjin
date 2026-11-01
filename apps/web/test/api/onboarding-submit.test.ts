/**
 * /api/onboarding/submit contract tests. Asserts the wizard row is
 * upserted and a business_profile_build job is enqueued.
 */

import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  and,
  db,
  eq,
  onboarding_wizard,
  organization,
  pool,
  processing_job,
} from "@neko/db";
import { callRoute } from "../_helpers/route";

const {
  mockGetOrgId,
  mockEnqueue,
  mockHasPrimaryProviderSetup,
  mockRequireAgentRuntimeReady,
} = vi.hoisted(() => ({
  mockGetOrgId: vi.fn(),
  mockEnqueue: vi.fn(),
  mockHasPrimaryProviderSetup: vi.fn(),
  mockRequireAgentRuntimeReady: vi.fn(),
}));

vi.mock("@/lib/db", async () => {
  const actual = await vi.importActual<typeof import("@/lib/db")>("@/lib/db");
  return { ...actual, getOrgId: mockGetOrgId };
});

vi.mock("@neko/db/jobs", async () => {
  const actual = await vi.importActual<typeof import("@neko/db/jobs")>("@neko/db/jobs");
  return { ...actual, enqueue: mockEnqueue };
});

vi.mock("@/lib/provider-settings", () => ({
  hasPrimaryProviderSetup: mockHasPrimaryProviderSetup,
}));

vi.mock("@/lib/agent-runtime-readiness", async () => {
  const actual = await vi.importActual<
    typeof import("@/lib/agent-runtime-readiness")
  >("@/lib/agent-runtime-readiness");
  return {
    ...actual,
    requireAgentRuntimeReady: mockRequireAgentRuntimeReady,
  };
});

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[api/onboarding/submit] skipping: Postgres unreachable.");
}

describeIfDb("/api/onboarding/submit", () => {
  let orgId: string;
  let POST: typeof import("@/app/api/onboarding/submit/route").POST;

  beforeAll(async () => {
    const mod = await import("@/app/api/onboarding/submit/route");
    POST = mod.POST;
  });

  beforeEach(async () => {
    orgId = uniqueOrgId("api-onboarding");
    await createTestOrg(orgId);
    mockGetOrgId.mockResolvedValue(orgId);
    mockEnqueue.mockResolvedValue("queue-id-stub");
    mockHasPrimaryProviderSetup.mockResolvedValue(true);
    mockRequireAgentRuntimeReady.mockResolvedValue(undefined);
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
    vi.clearAllMocks();
  });

  afterAll(async () => {
    await pool().end();
  });

  it("writes organization.name + onboarding_wizard row and enqueues business_profile_build", async () => {
    const res = await callRoute(POST, {
      method: "POST",
      body: {
        companyName: "AdventureWorks Cycles",
        companyNote: "We sell bikes",
        fiscalYearStartMonth: 7,
        activeSeats: ["CEO", "CFO"],
        priorities: ["Defend wholesale margins"],
      },
    });
    expect(res.status).toBe(200);
    const jobId = (res.body as { jobId: string }).jobId;
    expect(jobId).toBeTruthy();

    // organization.name updated
    const orgs = await db()
      .select({ name: organization.name })
      .from(organization)
      .where(eq(organization.id, orgId));
    expect(orgs[0]?.name).toBe("AdventureWorks Cycles");

    // wizard row landed
    const wizards = await db()
      .select({
        note: onboarding_wizard.company_note,
        fy: onboarding_wizard.fiscal_year_start_month,
        seats: onboarding_wizard.active_seats,
        priorities: onboarding_wizard.priorities,
      })
      .from(onboarding_wizard)
      .where(eq(onboarding_wizard.org_id, orgId));
    expect(wizards[0]).toMatchObject({
      note: "We sell bikes",
      fy: 7,
      seats: ["CEO", "CFO"],
      priorities: ["Defend wholesale margins"],
    });

    // processing_job row + enqueue both happened
    const jobs = await db()
      .select({ kind: processing_job.kind, status: processing_job.status })
      .from(processing_job)
      .where(eq(processing_job.org_id, orgId));
    expect(jobs).toHaveLength(1);
    expect(jobs[0].kind).toBe("business_profile_build");
    expect(jobs[0].status).toBe("queued");
    expect(mockEnqueue).toHaveBeenCalledWith(
      "business_profile_build",
      expect.objectContaining({ orgId, processingJobId: jobId }),
      { retryLimit: 0 },
    );
    expect(mockRequireAgentRuntimeReady).toHaveBeenCalledWith(orgId);
  });

  it("re-submission replaces the wizard row (no duplicate primary key)", async () => {
    const submit = (note: string) =>
      callRoute(POST, {
        method: "POST",
        body: {
          companyName: "AdventureWorks Cycles",
          companyNote: note,
          fiscalYearStartMonth: 1,
          activeSeats: ["CEO"],
          priorities: [],
        },
      });
    await submit("first");
    // The first submit's build must have settled before a re-submit takes
    // effect; an in-flight build suppresses re-submits (tested below).
    await db()
      .update(processing_job)
      .set({ status: "completed" })
      .where(eq(processing_job.org_id, orgId));
    await submit("second");

    const rows = await db()
      .select({ note: onboarding_wizard.company_note })
      .from(onboarding_wizard)
      .where(eq(onboarding_wizard.org_id, orgId));
    expect(rows).toHaveLength(1);
    expect(rows[0].note).toBe("second");
  });

  it("suppresses a concurrent submit while a build is in flight", async () => {
    const submit = (note: string) =>
      callRoute(POST, {
        method: "POST",
        body: {
          companyName: "AdventureWorks Cycles",
          companyNote: note,
          fiscalYearStartMonth: 1,
          activeSeats: ["CEO"],
          priorities: [],
        },
      });
    const first = await submit("first");
    const firstBody = first.body as { jobId?: string };
    // Double-click / client retry while the first build is still queued:
    // no second chain, no wizard-row churn, same job reported back.
    const second = await submit("second");
    const secondBody = second.body as { jobId?: string };
    expect(second.status).toBe(200);
    expect(secondBody.jobId).toBe(firstBody.jobId);

    const rows = await db()
      .select({ note: onboarding_wizard.company_note })
      .from(onboarding_wizard)
      .where(eq(onboarding_wizard.org_id, orgId));
    expect(rows).toHaveLength(1);
    expect(rows[0].note).toBe("first");

    const jobs = await db()
      .select({ id: processing_job.id })
      .from(processing_job)
      .where(
        and(
          eq(processing_job.org_id, orgId),
          eq(processing_job.kind, "business_profile_build"),
        ),
      );
    expect(jobs).toHaveLength(1);
  });

  it("rejects an empty companyName with 400", async () => {
    const res = await callRoute(POST, {
      method: "POST",
      body: {
        companyName: "   ",
        companyNote: "We sell bikes",
        fiscalYearStartMonth: 7,
        activeSeats: ["CEO"],
        priorities: [],
      },
    });
    expect(res.status).toBe(400);
    const wizards = await db()
      .select({ id: onboarding_wizard.org_id })
      .from(onboarding_wizard)
      .where(eq(onboarding_wizard.org_id, orgId));
    expect(wizards).toHaveLength(0);
  });

  it("does not write or enqueue work when the agent runtime is unavailable", async () => {
    const { AgentRuntimeUnavailableError } = await import(
      "@/lib/agent-runtime-readiness"
    );
    mockRequireAgentRuntimeReady.mockRejectedValueOnce(
      new AgentRuntimeUnavailableError(),
    );

    const res = await callRoute(POST, {
      method: "POST",
      body: {
        companyName: "AdventureWorks Cycles",
        companyNote: "We sell bikes",
        fiscalYearStartMonth: 7,
        activeSeats: ["CEO"],
        priorities: [],
      },
    });

    expect(res.status).toBe(503);
    expect(res.body).toMatchObject({ code: "agent_runtime_unavailable" });
    expect(mockEnqueue).not.toHaveBeenCalled();
    const jobs = await db()
      .select({ id: processing_job.id })
      .from(processing_job)
      .where(eq(processing_job.org_id, orgId));
    expect(jobs).toHaveLength(0);
    const wizards = await db()
      .select({ id: onboarding_wizard.org_id })
      .from(onboarding_wizard)
      .where(eq(onboarding_wizard.org_id, orgId));
    expect(wizards).toHaveLength(0);
  });
});
