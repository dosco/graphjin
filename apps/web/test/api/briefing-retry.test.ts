/**
 * /api/briefing/retry — re-enqueue a metric_refresh for a single failed
 * (or stuck) card. Asserts:
 *   - happy path: insert processing_job + enqueue + flip metric.last_refresh_status to "pending"
 *   - idempotent: existing in-flight job → return its id, no duplicate insert
 *   - org check: metric belonging to a different org → 404
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
  metric,
  pool,
  processing_job,
} from "@neko/db";
import { callRoute } from "../_helpers/route";

const { mockGetOrgId, mockEnqueue } = vi.hoisted(() => ({
  mockGetOrgId: vi.fn(),
  mockEnqueue: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@/lib/db", async () => {
  const actual = await vi.importActual<typeof import("@/lib/db")>("@/lib/db");
  return { ...actual, getOrgId: mockGetOrgId };
});

vi.mock("@neko/db/jobs", async () => {
  const actual = await vi.importActual<typeof import("@neko/db/jobs")>("@neko/db/jobs");
  return { ...actual, enqueue: mockEnqueue };
});

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[api/briefing/retry] skipping: Postgres unreachable.");
}

async function insertFailedMetric(orgId: string, slug: string): Promise<string> {
  const ins = await db()
    .insert(metric)
    .values({
      org_id: orgId,
      role: "CEO",
      slug,
      source: "bootstrap",
      title: `Title for ${slug}`,
      why: "test",
      chart_hint: "kpi",
      active: true,
      last_refresh_status: "failed",
      last_refresh_error: "previous failure",
    })
    .returning({ id: metric.id });
  return ins[0]!.id;
}

describeIfDb("/api/briefing/retry POST", () => {
  let orgId: string;
  let POST: typeof import("@/app/api/briefing/retry/route").POST;

  beforeAll(async () => {
    const mod = await import("@/app/api/briefing/retry/route");
    POST = mod.POST;
  });

  beforeEach(async () => {
    orgId = uniqueOrgId("api-briefing-retry");
    await createTestOrg(orgId);
    mockGetOrgId.mockResolvedValue(orgId);
    mockEnqueue.mockClear();
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
    vi.clearAllMocks();
  });

  afterAll(async () => {
    await pool().end();
  });

  it("400s when metricId is missing", async () => {
    const res = await callRoute(POST, { method: "POST", body: {} });
    expect(res.status).toBe(400);
  });

  it("404s when metric belongs to a different org", async () => {
    const otherOrg = uniqueOrgId("other-org");
    await createTestOrg(otherOrg);
    try {
      const otherMetricId = await insertFailedMetric(otherOrg, "other-card");
      const res = await callRoute(POST, {
        method: "POST",
        body: { metricId: otherMetricId },
      });
      expect(res.status).toBe(404);
    } finally {
      await deleteTestOrg(otherOrg);
    }
  });

  it("inserts a fresh processing_job, enqueues, and flips status to pending", async () => {
    const metricId = await insertFailedMetric(orgId, "broken-card");

    const res = await callRoute(POST, {
      method: "POST",
      body: { metricId },
    });
    expect(res.status).toBe(200);
    const body = res.body as { jobId: string; alreadyRunning: boolean };
    expect(body.alreadyRunning).toBe(false);
    expect(body.jobId).toBeDefined();

    // Worker queue called with the new job id
    expect(mockEnqueue).toHaveBeenCalledTimes(1);
    expect(mockEnqueue).toHaveBeenCalledWith("metric_refresh", {
      processingJobId: body.jobId,
      orgId,
    });

    // processing_job row created with kind='metric_refresh'
    const jobs = await db()
      .select({ id: processing_job.id, status: processing_job.status })
      .from(processing_job)
      .where(eq(processing_job.id, body.jobId));
    expect(jobs).toHaveLength(1);
    expect(jobs[0].status).toBe("queued");

    // metric.last_refresh_status flipped to pending + error cleared
    const rows = await db()
      .select({
        status: metric.last_refresh_status,
        err: metric.last_refresh_error,
        jobRef: metric.last_refresh_job_id,
      })
      .from(metric)
      .where(eq(metric.id, metricId));
    expect(rows[0].status).toBe("pending");
    expect(rows[0].err).toBeNull();
    expect(rows[0].jobRef).toBe(body.jobId);
  });

  it("returns existing job id when one is already in-flight (idempotent)", async () => {
    const metricId = await insertFailedMetric(orgId, "already-running");

    // Pre-seed a queued processing_job for this metric.
    const seeded = await db()
      .insert(processing_job)
      .values({
        org_id: orgId,
        kind: "metric_refresh",
        status: "queued",
        trigger: "test",
        trigger_payload: { metricId },
      })
      .returning({ id: processing_job.id });
    const existingJobId = seeded[0]!.id;

    const res = await callRoute(POST, {
      method: "POST",
      body: { metricId },
    });
    expect(res.status).toBe(200);
    const body = res.body as { jobId: string; alreadyRunning: boolean };
    expect(body.alreadyRunning).toBe(true);
    expect(body.jobId).toBe(existingJobId);

    // No second enqueue.
    expect(mockEnqueue).not.toHaveBeenCalled();

    // No new processing_job inserted (count stays at 1).
    const jobs = await db()
      .select({ id: processing_job.id })
      .from(processing_job)
      .where(
        and(
          eq(processing_job.org_id, orgId),
          eq(processing_job.kind, "metric_refresh"),
        ),
      );
    expect(jobs).toHaveLength(1);
  });
});
