/**
 * runMetricRefresh job orchestration tests.
 *
 * The metric agent itself (runMetricAgent) is mocked — we're testing the
 * worker's wiring, not the LLM. Asserts:
 *   - Path 1 (bootstrap): metricId payload → loads metric → snapshot lands
 *   - Path 2 (chat): question payload → creates metric row + snapshot
 *   - Validation failures throw + leave no snapshot
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
  data_source,
  db,
  eq,
  metric,
  metric_snapshot,
  pool,
  processing_job,
} from "@neko/db";

const { mockEnsureHostConfig, mockRunMetricAgent, mockGraphjinQuery } = vi.hoisted(() => ({
  mockEnsureHostConfig: vi.fn(),
  mockRunMetricAgent: vi.fn(),
  mockGraphjinQuery: vi.fn(),
}));

vi.mock("@neko/llm", async () => {
  const actual = await vi.importActual<typeof import("@neko/llm")>("@neko/llm");
  return {
    ...actual,
    ensureHostConfigProvisioned: mockEnsureHostConfig,
    runMetricAgent: mockRunMetricAgent,
  };
});

vi.mock("@neko/llm/graphjin", async () => {
  const actual = await vi.importActual<typeof import("@neko/llm/graphjin")>("@neko/llm/graphjin");
  return {
    ...actual,
    graphjinQuery: mockGraphjinQuery,
  };
});

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[jobs/metric-refresh] skipping: Postgres unreachable.");
}

function stubResult(overrides: Record<string, unknown> = {}) {
  return {
    reasoning: "stub",
    headlineMetric: "$1.00M",
    headlineLabel: "Test",
    insightText: "Up.",
    detailText: "Driven by stub.",
    mood: "watch",
    chartType: "kpi",
    chartData: [{ d: "Test", v: 1_000_000, t: 950_000 }],
    timeWindow: {
      grain: "year",
      start: "2024-04-01",
      end: "2025-04-01",
      label: "TTM",
    },
    ...overrides,
  };
}

async function insertProcessingJob(
  orgId: string,
  payload: Record<string, unknown>,
): Promise<string> {
  const ins = await db()
    .insert(processing_job)
    .values({
      org_id: orgId,
      kind: "metric_refresh",
      status: "running",
      trigger: "test",
      trigger_payload: payload,
      started_at: new Date(),
    })
    .returning({ id: processing_job.id });
  return ins[0]!.id;
}

// metric_snapshot has no org_id column — it scopes via metric.metric_id.
// Without this join, the query would return snapshots from every other org
// in the dev DB and fail length assertions that assume a clean slate.
async function snapshotsForOrg(orgId: string): Promise<{ id: string }[]> {
  return db()
    .select({ id: metric_snapshot.id })
    .from(metric_snapshot)
    .innerJoin(metric, eq(metric_snapshot.metric_id, metric.id))
    .where(eq(metric.org_id, orgId));
}

async function insertMetric(orgId: string, slug: string): Promise<string> {
  const ins = await db()
    .insert(metric)
    .values({
      org_id: orgId,
      role: "CEO",
      slug,
      source: "bootstrap",
      title: `Title for ${slug}`,
      why: `Why ${slug}`,
      chart_hint: "kpi",
      active: true,
    })
    .returning({ id: metric.id });
  return ins[0]!.id;
}

describeIfDb("runMetricRefresh", () => {
  let orgId: string;
  let runMetricRefresh: typeof import("../../src/jobs/metric-refresh").runMetricRefresh;

  beforeAll(async () => {
    const mod = await import("../../src/jobs/metric-refresh.js");
    runMetricRefresh = mod.runMetricRefresh;
  });

  beforeEach(async () => {
    orgId = uniqueOrgId("job-metric-refresh");
    await createTestOrg(orgId);
    mockEnsureHostConfig.mockResolvedValue(undefined);
    mockRunMetricAgent.mockResolvedValue(stubResult());
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
    vi.clearAllMocks();
  });

  afterAll(async () => {
    await pool().end();
  });

  describe("path 1: bootstrap metric (trigger_payload.metricId)", () => {
    it("loads the metric, runs the agent, writes a snapshot", async () => {
      const metricId = await insertMetric(orgId, "revenue-by-channel");
      const jobId = await insertProcessingJob(orgId, { metricId });

      await runMetricRefresh(jobId, orgId);

      const snaps = await db()
        .select({ status: metric_snapshot.status, payload: metric_snapshot.payload })
        .from(metric_snapshot)
        .where(eq(metric_snapshot.metric_id, metricId));
      expect(snaps).toHaveLength(1);
      expect(snaps[0].status).toBe("watch");

      // Mock was called with the right slug + role
      expect(mockRunMetricAgent).toHaveBeenCalledWith(
        expect.objectContaining({
          orgId,
          slug: "revenue-by-channel",
          role: "CEO",
          jobId,
        }),
      );
      expect(mockEnsureHostConfig).toHaveBeenCalledWith(orgId);
      expect(mockEnsureHostConfig.mock.invocationCallOrder[0]).toBeLessThan(
        mockRunMetricAgent.mock.invocationCallOrder[0],
      );
      const [job] = await db()
        .select({ result: processing_job.result })
        .from(processing_job)
        .where(eq(processing_job.id, jobId));
      expect(job?.result).toMatchObject({
        telemetry: {
          schemaVersion: "openneko.harness-run-summary/v1",
          runId: jobId,
          status: "completed",
          phases: expect.arrayContaining([expect.objectContaining({ name: "metric.db_read", ok: true }), expect.objectContaining({ name: "metric.persist", ok: true })]),
        },
      });
    });

    it("runs a reviewed saved query deterministically without an LLM", async () => {
      await db().insert(data_source).values({
        org_id: orgId,
        kind: "customer",
        name: "magento",
        graphql_url: "http://magento-graphjin.test",
        auth_mode: "none",
        is_default: true,
        enabled: true,
      });
      const definition = {
        title: "Orders placed",
        description: "Orders in the selected period.",
        calculationNote: "Cancellations remain included.",
        chartHint: "metric",
        unit: "count",
        directionGood: "up",
        execution: {
          mode: "saved_query",
          source: "magento_analytics",
          query: "orders_placed",
          document: "query Orders($from: String!, $to: String!, $storeIds: [Int!]!) { sales_order { orders: count } }",
          result: { kind: "scalar", path: "sales_order.0.orders" },
          freshnessSeconds: 7200,
          runtime: { storeIds: [1], windowDays: 30 },
        },
      };
      const [card] = await db().insert(metric).values({
        org_id: orgId,
        role: "executive",
        slug: "magento-orders-placed",
        source: "magento",
        title: "Orders placed",
        why: "Reviewed Magento order count.",
        chart_hint: "metric",
        active: true,
        execution_mode: "saved_query",
        definition_json: definition,
        definition_version: 1,
        definition_hash: "definition-hash",
      }).returning({ id: metric.id });
      mockGraphjinQuery.mockResolvedValueOnce({
        data: { sales_order: [{ orders: 42 }] },
      });
      const jobId = await insertProcessingJob(orgId, { metricId: card!.id });

      await runMetricRefresh(jobId, orgId);

      expect(mockRunMetricAgent).not.toHaveBeenCalled();
      expect(mockGraphjinQuery).toHaveBeenCalledWith(
        expect.objectContaining({
          baseUrl: "http://magento-graphjin.test/api/v1/graphql",
          query: definition.execution.document,
          role: "service",
          variables: expect.objectContaining({ storeIds: [1] }),
        }),
      );
      const [snapshot] = await db()
        .select({
          value: metric_snapshot.value,
          definitionHash: metric_snapshot.definition_hash,
          status: metric_snapshot.status,
        })
        .from(metric_snapshot)
        .where(eq(metric_snapshot.metric_id, card!.id));
      expect(snapshot).toMatchObject({
        value: "42",
        definitionHash: "definition-hash",
        status: "watch",
      });
    });

    it("throws when the metricId doesn't resolve to a row (no snapshot written)", async () => {
      const fakeId = "00000000-0000-0000-0000-000000000000";
      const jobId = await insertProcessingJob(orgId, { metricId: fakeId });

      await expect(runMetricRefresh(jobId, orgId)).rejects.toThrow(/not found/);

      const snaps = await snapshotsForOrg(orgId);
      expect(snaps).toHaveLength(0);
    });
  });

  describe("path 2: chat question (trigger_payload.question)", () => {
    it("creates a chat metric row, then a snapshot pointing at it", async () => {
      const jobId = await insertProcessingJob(orgId, {
        question: "What's our revenue this month?",
        slug: "chat-revenue-q",
        title: "Revenue MTD",
        why: "User asked",
        chartHint: "kpi",
        role: "CEO",
        classification: {
          durationMs: 25,
          usage: {
            inputTokens: 100,
            outputTokens: 20,
            totalTokens: 120,
            coverage: "complete",
          },
          provider: "google-gemini",
          model: "gemini-test",
        },
      });

      await runMetricRefresh(jobId, orgId);

      const metrics = await db()
        .select({ slug: metric.slug, source: metric.source, active: metric.active })
        .from(metric)
        .where(eq(metric.org_id, orgId));
      expect(metrics).toHaveLength(1);
      expect(metrics[0]).toMatchObject({ slug: "chat-revenue-q", source: "chat", active: false });

      const snaps = await snapshotsForOrg(orgId);
      expect(snaps).toHaveLength(1);
      expect(mockRunMetricAgent).toHaveBeenCalledWith(
        expect.objectContaining({
          question: "What's our revenue this month?",
          title: "Revenue MTD",
          why: "User asked",
        }),
      );
      const [job] = await db()
        .select({ result: processing_job.result })
        .from(processing_job)
        .where(eq(processing_job.id, jobId));
      expect(job?.result).toMatchObject({
        telemetry: {
          counts: { inference: 1 },
          usage: {
            inputTokens: 100,
            outputTokens: 20,
            totalTokens: 120,
            coverage: "complete",
          },
        },
      });
    });

    it("re-running with same slug+role+org reuses the existing metric row", async () => {
      const payload = {
        question: "Q?",
        slug: "chat-x",
        title: "X",
        why: "Y",
        chartHint: "bar",
        role: "CEO",
      };
      const job1 = await insertProcessingJob(orgId, payload);
      await runMetricRefresh(job1, orgId);
      const job2 = await insertProcessingJob(orgId, payload);
      await runMetricRefresh(job2, orgId);

      const metrics = await db()
        .select({ id: metric.id, created_by_job: metric.created_by_job })
        .from(metric)
        .where(and(eq(metric.org_id, orgId), eq(metric.slug, "chat-x")));
      expect(metrics).toHaveLength(1);
      // Status endpoint locates the snapshot via metric.created_by_job, so the
      // reused row must be re-linked to the latest job — otherwise rerun /
      // re-ask returns payload=null in the UI.
      expect(metrics[0].created_by_job).toBe(job2);
      const snaps = await snapshotsForOrg(orgId);
      expect(snaps).toHaveLength(2);
    });
  });

  describe("metric.last_refresh_status bookkeeping", () => {
    it("stamps last_refresh_status='ok' on success", async () => {
      const metricId = await insertMetric(orgId, "ok-status");
      const jobId = await insertProcessingJob(orgId, { metricId });
      await runMetricRefresh(jobId, orgId);

      const rows = await db()
        .select({
          status: metric.last_refresh_status,
          err: metric.last_refresh_error,
          jobRef: metric.last_refresh_job_id,
        })
        .from(metric)
        .where(eq(metric.id, metricId));
      expect(rows[0].status).toBe("ok");
      expect(rows[0].err).toBeNull();
      expect(rows[0].jobRef).toBe(jobId);
    });

    it("stamps last_refresh_status='failed' + error on validation failure", async () => {
      mockRunMetricAgent.mockResolvedValueOnce(stubResult({ mood: "ecstatic" }));
      const metricId = await insertMetric(orgId, "fail-status-mood");
      const jobId = await insertProcessingJob(orgId, { metricId });

      await expect(runMetricRefresh(jobId, orgId)).rejects.toThrow();

      const rows = await db()
        .select({
          status: metric.last_refresh_status,
          err: metric.last_refresh_error,
          jobRef: metric.last_refresh_job_id,
        })
        .from(metric)
        .where(eq(metric.id, metricId));
      expect(rows[0].status).toBe("failed");
      expect(rows[0].err).toMatch(/invalid mood/);
      expect(rows[0].jobRef).toBe(jobId);
      const [job] = await db()
        .select({ result: processing_job.result })
        .from(processing_job)
        .where(eq(processing_job.id, jobId));
      expect(job?.result).toMatchObject({
        telemetry: { status: "failed", errorType: "metric_result_invalid" },
      });

      // Snapshot should not exist on failure path.
      const snaps = await snapshotsForOrg(orgId);
      expect(snaps).toHaveLength(0);
    });

    it("stamps last_refresh_status='failed' on sentinel-headline rejection", async () => {
      mockRunMetricAgent.mockResolvedValueOnce(
        stubResult({ headlineMetric: "Error" }),
      );
      const metricId = await insertMetric(orgId, "fail-status-sentinel");
      const jobId = await insertProcessingJob(orgId, { metricId });

      await expect(runMetricRefresh(jobId, orgId)).rejects.toThrow(/sentinel/);

      const rows = await db()
        .select({ status: metric.last_refresh_status, err: metric.last_refresh_error })
        .from(metric)
        .where(eq(metric.id, metricId));
      expect(rows[0].status).toBe("failed");
      expect(rows[0].err).toMatch(/sentinel/);
    });
  });

  describe("validation", () => {
    it("rejects an agent result with an invalid mood (no snapshot written)", async () => {
      mockRunMetricAgent.mockResolvedValueOnce(stubResult({ mood: "ecstatic" }));
      const metricId = await insertMetric(orgId, "bad-mood-metric");
      const jobId = await insertProcessingJob(orgId, { metricId });

      await expect(runMetricRefresh(jobId, orgId)).rejects.toThrow(/invalid mood/);
      const snaps = await snapshotsForOrg(orgId);
      expect(snaps).toHaveLength(0);
    });
  });
});
