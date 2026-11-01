/**
 * Contract tests for POST /api/work/threads — focused on the "deep dive"
 * seed: the dashboard sends { seedMetricId } when a briefing card is
 * opened in a new thread, and the route must insert the card payload as
 * the opening work_message so the agent picks it up from history.
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
import { and, db, eq, metric, metric_snapshot, pool, work_message, work_thread } from "@neko/db";
import { BRIEFING_CARD_SENTINEL, parseBriefingCardMessage } from "@/lib/briefing-card-context";
import { callRoute } from "../_helpers/route";

const { mockGetOrgId } = vi.hoisted(() => ({ mockGetOrgId: vi.fn() }));

vi.mock("@/lib/db", async () => {
  const actual = await vi.importActual<typeof import("@/lib/db")>("@/lib/db");
  return { ...actual, getOrgId: mockGetOrgId };
});

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;
if (!reachable) {
  console.warn("[api/work/threads] skipping: Postgres unreachable.");
}

async function insertTestMetric(
  orgId: string,
  opts: { slug: string; title: string; withSnapshot?: boolean } = {
    slug: "rev-yoy",
    title: "Revenue YoY",
  },
): Promise<string> {
  const inserted = await db()
    .insert(metric)
    .values({
      org_id: orgId,
      role: "CEO",
      slug: opts.slug,
      source: "bootstrap",
      title: opts.title,
      chart_hint: "line",
      cadence: "daily",
      active: true,
    })
    .returning({ id: metric.id });
  const metricId = inserted[0]!.id;
  if (opts.withSnapshot) {
    await db().insert(metric_snapshot).values({
      metric_id: metricId,
      status: "good",
      payload: {
        mood: "good",
        headlineMetric: "$50.37M",
        headlineLabel: "TTM Revenue",
        insightText: "Revenue surged 28.1% YoY.",
        detailText: "Strong growth across Q3 and Q4.",
        chartType: "line",
        chartData: [
          { d: "Jan", v: 4 },
          { d: "Feb", v: 4.5 },
        ],
      },
    });
  }
  return metricId;
}

describeIfDb("/api/work/threads", () => {
  let orgId: string;
  let POST: typeof import("@/app/api/work/threads/route").POST;
  let loadBriefingCardForSeed: typeof import("@/app/api/work/threads/route").loadBriefingCardForSeed;

  beforeAll(async () => {
    const mod = await import("@/app/api/work/threads/route");
    POST = mod.POST;
    loadBriefingCardForSeed = mod.loadBriefingCardForSeed;
  });

  beforeEach(async () => {
    orgId = uniqueOrgId("api-work-threads");
    await createTestOrg(orgId);
    mockGetOrgId.mockResolvedValue(orgId);
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
    vi.clearAllMocks();
  });

  afterAll(async () => {
    await pool().end();
  });

  // ── POST contract ──────────────────────────────────────────────────────

  it("creates an empty thread when no seedMetricId is provided", async () => {
    const res = await callRoute(POST, { method: "POST", body: {} });
    expect(res.status).toBe(200);
    const body = res.body as { thread: { id: string } };
    const messages = await db()
      .select()
      .from(work_message)
      .where(eq(work_message.thread_id, body.thread.id));
    expect(messages).toHaveLength(0);
  });

  it("seeds a briefing-card work_message when seedMetricId resolves", async () => {
    const metricId = await insertTestMetric(orgId, {
      slug: "rev-yoy",
      title: "Revenue YoY",
      withSnapshot: true,
    });

    const res = await callRoute(POST, {
      method: "POST",
      body: { seedMetricId: metricId },
    });
    expect(res.status).toBe(200);
    const body = res.body as { thread: { id: string; title: string } };
    // Deep-dive threads are titled from the metric so they never show
    // "Untitled thread" in the sidebar before the first user message.
    expect(body.thread.title).toBe("Revenue YoY");

    const messages = await db()
      .select()
      .from(work_message)
      .where(eq(work_message.thread_id, body.thread.id));
    expect(messages).toHaveLength(1);
    const seed = messages[0];
    expect(seed.role).toBe("user");
    expect(seed.content.startsWith(BRIEFING_CARD_SENTINEL)).toBe(true);

    const parsed = parseBriefingCardMessage(seed.content);
    expect(parsed).not.toBeNull();
    expect(parsed?.metricId).toBe(metricId);
    expect(parsed?.text).toBe("Revenue YoY");
    expect(parsed?.metric).toBe("$50.37M");
    expect(parsed?.label).toBe("TTM Revenue");
    expect(parsed?.mood).toBe("good");
    expect(parsed?.detail).toContain("Revenue surged 28.1% YoY.");
    expect(parsed?.chart).toBe("line");
    expect(parsed?.chartData).toHaveLength(2);
  });

  it("ignores an unknown seedMetricId silently (thread still created)", async () => {
    const res = await callRoute(POST, {
      method: "POST",
      body: { seedMetricId: "00000000-0000-0000-0000-000000000000" },
    });
    expect(res.status).toBe(200);
    const body = res.body as { thread: { id: string } };
    const messages = await db()
      .select()
      .from(work_message)
      .where(eq(work_message.thread_id, body.thread.id));
    expect(messages).toHaveLength(0);
  });

  it("ignores a metric that belongs to another org (cross-org isolation)", async () => {
    const otherOrgId = uniqueOrgId("api-work-threads-other");
    await createTestOrg(otherOrgId);
    try {
      const foreignMetricId = await insertTestMetric(otherOrgId, {
        slug: "rev-yoy",
        title: "Revenue YoY",
        withSnapshot: true,
      });
      const res = await callRoute(POST, {
        method: "POST",
        body: { seedMetricId: foreignMetricId },
      });
      expect(res.status).toBe(200);
      const body = res.body as { thread: { id: string } };
      const messages = await db()
        .select()
        .from(work_message)
        .where(eq(work_message.thread_id, body.thread.id));
      expect(messages).toHaveLength(0);
    } finally {
      await deleteTestOrg(otherOrgId);
    }
  });

  // ── loadBriefingCardForSeed direct ─────────────────────────────────────

  it("loadBriefingCardForSeed returns null for missing metric", async () => {
    const seed = await loadBriefingCardForSeed(
      orgId,
      "00000000-0000-0000-0000-000000000000",
    );
    expect(seed).toBeNull();
  });

  it("loadBriefingCardForSeed handles a metric with no snapshot (empty payload fields)", async () => {
    const metricId = await insertTestMetric(orgId, {
      slug: "no-snap",
      title: "No Snapshot Yet",
    });
    const seed = await loadBriefingCardForSeed(orgId, metricId);
    expect(seed).not.toBeNull();
    expect(seed?.title).toBe("No Snapshot Yet");
    const parsed = parseBriefingCardMessage(seed!.message);
    expect(parsed).not.toBeNull();
    expect(parsed?.metricId).toBe(metricId);
    expect(parsed?.text).toBe("No Snapshot Yet");
    expect(parsed?.metric).toBe("");
    expect(parsed?.label).toBe("");
    expect(parsed?.detail).toBe("");
    expect(parsed?.chart).toBe("line"); // falls back to metric.chart_hint
    expect(parsed?.chartData).toEqual([]);
  });

  it("loadBriefingCardForSeed enforces org scoping", async () => {
    const otherOrgId = uniqueOrgId("api-work-threads-load-other");
    await createTestOrg(otherOrgId);
    try {
      const foreignMetricId = await insertTestMetric(otherOrgId, {
        slug: "foreign",
        title: "Foreign Metric",
        withSnapshot: true,
      });
      const seed = await loadBriefingCardForSeed(orgId, foreignMetricId);
      expect(seed).toBeNull();
    } finally {
      await deleteTestOrg(otherOrgId);
    }
  });

  it("work_thread row is still created when the seed silently fails", async () => {
    const res = await callRoute(POST, {
      method: "POST",
      body: { seedMetricId: "00000000-0000-0000-0000-000000000000" },
    });
    const body = res.body as { thread: { id: string } };
    const threads = await db()
      .select({ id: work_thread.id })
      .from(work_thread)
      .where(
        and(
          eq(work_thread.id, body.thread.id),
          eq(work_thread.org_id, orgId),
        ),
      );
    expect(threads).toHaveLength(1);
  });
});
