/**
 * /api/onboarding/status — verifies the enriched response shape
 * (currentStage + metricsProgress) that drives the /business-profile stage
 * strip and the dashboard's "X of Y still loading" banner.
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
  customer_profile,
  onboarding_wizard,
  getOrCreateSoloAdmin,
  db,
  operator_profile,
  pool,
  processing_job,
} from "@neko/db";
import { callRoute } from "../_helpers/route";

const { mockGetOrgId, mockGetCurrentActor, mockGetAuthProvider } = vi.hoisted(() => ({
  mockGetAuthProvider: vi.fn(),
  mockGetOrgId: vi.fn(),
  mockGetCurrentActor: vi.fn(),
}));

vi.mock("@/lib/db", async () => {
  const actual = await vi.importActual<typeof import("@/lib/db")>("@/lib/db");
  return { ...actual, getOrgId: mockGetOrgId };
});

vi.mock("@/lib/actor", () => ({
  getCurrentActor: mockGetCurrentActor,
}));

vi.mock("@/lib/auth", () => ({ getAuthProvider: mockGetAuthProvider }));

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[api/onboarding/status] skipping: Postgres unreachable.");
}

async function insertJob(
  orgId: string,
  kind:
    | "business_profile_build"
    | "industry_insights_build"
    | "bootstrap_metrics_build"
    | "metric_refresh",
  status: "queued" | "running" | "succeeded" | "failed",
  progress?: { message?: string },
  error?: string,
): Promise<string> {
  const ins = await db()
    .insert(processing_job)
    .values({
      org_id: orgId,
      kind,
      status,
      trigger: "test",
      trigger_payload: {},
      progress: progress ?? {},
      error,
      ...(status === "running" || status === "succeeded" || status === "failed"
        ? { started_at: new Date() }
        : {}),
      ...(status === "succeeded" || status === "failed"
        ? { finished_at: new Date() }
        : {}),
    })
    .returning({ id: processing_job.id });
  return ins[0]!.id;
}

describeIfDb("/api/onboarding/status GET (enriched)", () => {
  let orgId: string;
  let GET: typeof import("@/app/api/onboarding/status/route").GET;

  beforeAll(async () => {
    const mod = await import("@/app/api/onboarding/status/route");
    GET = mod.GET;
  });

  beforeEach(async () => {
    mockGetAuthProvider.mockResolvedValue(null);
    orgId = uniqueOrgId("api-onboarding-status");
    await createTestOrg(orgId);
    mockGetOrgId.mockResolvedValue(orgId);
    mockGetCurrentActor.mockResolvedValue({ userId: null, role: "admin" });
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
    vi.clearAllMocks();
  });

  afterAll(async () => {
    await pool().end();
  });

  it("surfaces currentStage with progress.message during processing", async () => {
    await insertJob(orgId, "business_profile_build", "running", {
      message: "Reading sales tables…",
    });

    const res = await callRoute(GET);
    expect(res.status).toBe(200);
    const body = res.body as {
      state: string;
      currentStage?: { kind: string; message: string | null };
    };
    expect(body.state).toBe("processing");
    expect(body.currentStage).toEqual({
      kind: "business_profile_build",
      message: "Reading sales tables…",
    });
  });

  it("returns null currentStage.message when worker hasn't reported one yet", async () => {
    await insertJob(orgId, "business_profile_build", "running");

    const res = await callRoute(GET);
    const body = res.body as {
      state: string;
      currentStage?: { kind: string; message: string | null };
    };
    expect(body.state).toBe("processing");
    expect(body.currentStage?.kind).toBe("business_profile_build");
    expect(body.currentStage?.message).toBeNull();
  });

  it("reports an agent-loop timeout instead of claiming the runtime vanished", async () => {
    await insertJob(
      orgId,
      "business_profile_build",
      "failed",
      undefined,
      "ax turn exceeded its 360s budget and was terminated",
    );

    const res = await callRoute(GET);
    expect(res.body).toMatchObject({
      state: "failed",
      message: expect.stringContaining("time limit"),
    });
    expect((res.body as { message: string }).message).not.toContain(
      "agent became unavailable",
    );
  });

  it("keeps runtime recovery guidance for an actual sandbox failure", async () => {
    await insertJob(
      orgId,
      "business_profile_build",
      "failed",
      undefined,
      "agent sandbox exited 1: builtin-skills not found",
    );

    const res = await callRoute(GET);
    expect(res.body).toMatchObject({
      state: "failed",
      message: expect.stringContaining("agent became unavailable"),
    });
  });

  it("counts metric_refresh jobs after the latest bootstrap_metrics_build", async () => {
    // Profile build still running so state stays "processing".
    await insertJob(orgId, "business_profile_build", "running");
    // Older bootstrap (should not affect counts since the latest bootstrap
    // sets the floor — but here there's only one).
    await insertJob(orgId, "bootstrap_metrics_build", "succeeded");

    // Eight metric_refresh: 6 succeeded, 1 running, 1 failed.
    for (let i = 0; i < 6; i++) await insertJob(orgId, "metric_refresh", "succeeded");
    await insertJob(orgId, "metric_refresh", "running");
    await insertJob(orgId, "metric_refresh", "failed");

    const res = await callRoute(GET);
    const body = res.body as {
      state: string;
      metricsProgress?: { total: number; completed: number; failed: number };
    };
    expect(body.state).toBe("processing");
    expect(body.metricsProgress).toEqual({
      total: 8,
      completed: 6,
      failed: 1,
    });
  });

  it("returns metricsProgress with state='ready' so the dashboard can show its banner", async () => {
    // Mark profile ready by seeding customer_profile row.
    await db().insert(customer_profile).values({
      org_id: orgId,
      version: 1,
      is_current: true,
      business_profile: "test",
    });
    await insertJob(orgId, "bootstrap_metrics_build", "succeeded");
    await insertJob(orgId, "metric_refresh", "running");
    await insertJob(orgId, "metric_refresh", "running");

    const res = await callRoute(GET);
    const body = res.body as {
      state: string;
      metricsProgress?: { total: number; completed: number; failed: number };
    };
    expect(body.state).toBe("ready");
    expect(body.metricsProgress).toEqual({
      total: 2,
      completed: 0,
      failed: 0,
    });
  });

  it("preserves completed solo onboarding and role views after assigning a real identity", async () => {
    await db().insert(customer_profile).values({ org_id: orgId, version: 1, is_current: true, business_profile: "existing installation" });
    await db().insert(onboarding_wizard).values({ org_id: orgId, active_seats: ["CEO", "CFO", "COO", "HR"] });
    const owner = await getOrCreateSoloAdmin(orgId);
    expect(owner?.id).toBeTruthy();
    mockGetCurrentActor.mockResolvedValue({ userId: owner!.id, role: "admin" });
    expect((await callRoute(GET)).body).toMatchObject({ state: "ready", mode: "shared", seats: ["CEO", "CFO", "COO", "HR"] });
    // The same owner must complete a personal persona only after SSO becomes active.
    mockGetAuthProvider.mockResolvedValue({ pluginName: "sso" });
    expect((await callRoute(GET)).body).toEqual({ state: "needs_persona" });
  });

  it("requires an exact personal persona for an SSO user", async () => {
    mockGetAuthProvider.mockResolvedValue({ pluginName: "sso" });
    await db().insert(customer_profile).values({
      org_id: orgId,
      version: 1,
      is_current: true,
      business_profile: "test",
    });
    mockGetCurrentActor.mockResolvedValue({
      userId: "user-personal",
      role: "member",
    });

    const res = await callRoute(GET);
    expect(res.body).toEqual({ state: "needs_persona" });
  });

  it("removes shared role views after an SSO user defines their persona", async () => {
    mockGetAuthProvider.mockResolvedValue({ pluginName: "sso" });
    await db().insert(customer_profile).values({
      org_id: orgId,
      version: 1,
      is_current: true,
      business_profile: "test",
    });
    await db().insert(operator_profile).values({
      org_id: orgId,
      user_id: "user-personal",
      role_template: "Head of EU wholesale operations",
      focus_areas: ["Stock-outs"],
    });
    mockGetCurrentActor.mockResolvedValue({
      userId: "user-personal",
      role: "member",
    });

    const res = await callRoute(GET);
    expect(res.body).toMatchObject({
      state: "ready",
      mode: "personal",
      seats: [],
    });
  });
});
