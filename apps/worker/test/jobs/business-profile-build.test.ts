/**
 * runBusinessProfileBuild orchestration tests. Verifies the chain
 * decision: when research is disabled the worker skips industry_insights
 * and goes straight to bootstrap_metrics. When research is enabled it
 * chains industry_insights first.
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
  seedDataSource,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  db,
  eq,
  onboarding_wizard,
  pool,
  processing_job,
} from "@neko/db";

const { mockEnsureHostConfig, mockResolveResearch, mockRunProfiler, mockEnqueue } = vi.hoisted(() => ({
  mockEnsureHostConfig: vi.fn(),
  mockResolveResearch: vi.fn(),
  mockRunProfiler: vi.fn(),
  mockEnqueue: vi.fn(),
}));

vi.mock("@neko/llm", async () => {
  const actual = await vi.importActual<typeof import("@neko/llm")>("@neko/llm");
  return {
    ...actual,
    ensureHostConfigProvisioned: mockEnsureHostConfig,
    resolveResearchProviderConfig: mockResolveResearch,
    runProfiler: mockRunProfiler,
  };
});

vi.mock("@neko/db/jobs", async () => {
  const actual = await vi.importActual<typeof import("@neko/db/jobs")>("@neko/db/jobs");
  return { ...actual, enqueue: mockEnqueue };
});

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[jobs/business-profile-build] skipping: Postgres unreachable.");
}

async function insertJob(orgId: string): Promise<string> {
  const ins = await db()
    .insert(processing_job)
    .values({
      org_id: orgId,
      kind: "business_profile_build",
      status: "running",
      trigger: "test",
      started_at: new Date(),
    })
    .returning({ id: processing_job.id });
  return ins[0]!.id;
}

describeIfDb("runBusinessProfileBuild", () => {
  let orgId: string;
  let runBusinessProfileBuild: typeof import("../../src/jobs/business-profile-build").runBusinessProfileBuild;

  beforeAll(async () => {
    const mod = await import("../../src/jobs/business-profile-build.js");
    runBusinessProfileBuild = mod.runBusinessProfileBuild;
  });

  beforeEach(async () => {
    orgId = uniqueOrgId("job-business-profile");
    await createTestOrg(orgId);
    await seedDataSource(orgId);
    await db().insert(onboarding_wizard).values({
      org_id: orgId,
      company_note: "We sell bicycles to retailers.",
      fiscal_year_start_month: 7,
      active_seats: ["CEO"],
      priorities: ["Defend wholesale margins"],
      step: "submitting",
    });
    mockRunProfiler.mockResolvedValue({
      businessProfile: "Stub business profile content.",
    });
    mockEnsureHostConfig.mockResolvedValue(undefined);
    mockEnqueue.mockResolvedValue("queue-id-stub");
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
    vi.clearAllMocks();
  });

  afterAll(async () => {
    await pool().end();
  });

  it("chains industry_insights_build when research is enabled", async () => {
    mockResolveResearch.mockResolvedValue({
      enabled: true,
      provider: "perplexity",
      model: "sonar-deep-research",
      config: {},
      secrets: { apiKey: "pplx-test" },
      source: "org",
    });

    const jobId = await insertJob(orgId);
    await runBusinessProfileBuild(jobId, orgId);

    const calls = mockEnqueue.mock.calls;
    const queues = calls.map((c) => c[0] as string);
    expect(queues).toContain("industry_insights_build");
    expect(queues).not.toContain("bootstrap_metrics_build");
  });

  it("chains nothing when research is disabled", async () => {
    mockResolveResearch.mockResolvedValue({
      enabled: false,
      provider: "disabled",
      model: "",
      config: {},
      secrets: {},
      source: "org",
    });

    const jobId = await insertJob(orgId);
    await runBusinessProfileBuild(jobId, orgId);

    expect(mockEnqueue).not.toHaveBeenCalled();
  });

  it("invokes the profiler with the org's company_note + mcp_url", async () => {
    mockResolveResearch.mockResolvedValue({
      enabled: false,
      provider: "disabled",
      model: "",
      config: {},
      secrets: {},
      source: "org",
    });
    const jobId = await insertJob(orgId);
    await runBusinessProfileBuild(jobId, orgId);

    expect(mockRunProfiler).toHaveBeenCalledTimes(1);
    expect(mockEnsureHostConfig).toHaveBeenCalledWith(orgId);
    expect(mockEnsureHostConfig.mock.invocationCallOrder[0]).toBeLessThan(
      mockRunProfiler.mock.invocationCallOrder[0],
    );
    const args = mockRunProfiler.mock.calls[0][0];
    expect(args).toMatchObject({
      orgId,
      companyNote: "We sell bicycles to retailers.",
    });
    // mcp_url comes from data_source seeded in beforeEach.
    expect(args.mcpUrl).toMatch(/\/api\/v1\/mcp/);
  });

  it("throws if no mcp_url is configured", async () => {
    // Replace the data_source row with one that has no mcp_url.
    const { data_source: dataSource } = await import("@neko/db");
    await db().delete(dataSource).where(eq(dataSource.org_id, orgId));
    await db().insert(dataSource).values({
      org_id: orgId,
      kind: "graphjin",
      graphql_url: "http://example.com/graphql",
      mcp_url: null,
      label: "primary",
    });

    mockResolveResearch.mockResolvedValue({
      enabled: false,
      provider: "disabled",
      model: "",
      config: {},
      secrets: {},
      source: "org",
    });
    const jobId = await insertJob(orgId);
    await expect(runBusinessProfileBuild(jobId, orgId)).rejects.toThrow(/mcp_url/);
  });
});
