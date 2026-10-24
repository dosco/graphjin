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
import { NextRequest } from "next/server";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  db,
  eq,
  pool,
  processing_job,
  work_run,
  work_run_event,
  work_thread,
} from "@neko/db";
import { finishWorkRun } from "@neko/llm/work";

const { mockGetOrgId, mockEnqueue, mockResolveBackend } = vi.hoisted(() => ({
  mockGetOrgId: vi.fn(),
  mockEnqueue: vi.fn(),
  mockResolveBackend: vi.fn(),
}));

vi.mock("@/lib/db", async () => {
  const actual = await vi.importActual<typeof import("@/lib/db")>("@/lib/db");
  return { ...actual, getOrgId: mockGetOrgId };
});

vi.mock("@neko/db/jobs", async () => {
  const actual = await vi.importActual<typeof import("@neko/db/jobs")>(
    "@neko/db/jobs",
  );
  return { ...actual, enqueue: mockEnqueue };
});

vi.mock("@neko/llm", async () => {
  const actual = await vi.importActual<typeof import("@neko/llm")>("@neko/llm");
  return { ...actual, resolveAgentBackend: mockResolveBackend };
});

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[api/work/runs] skipping: Postgres unreachable.");
}

async function callRunsPost(
  POST: typeof import("@/app/api/work/threads/[threadId]/runs/route").POST,
  args: { threadId: string; body?: unknown },
): Promise<{ status: number; body: unknown }> {
  const req = new NextRequest("http://localhost:3000/test", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: args.body !== undefined ? JSON.stringify(args.body) : undefined,
  });
  const res = await POST(req, {
    params: Promise.resolve({ threadId: args.threadId }),
  });
  const text = await res.text();
  let parsed: unknown;
  try {
    parsed = text ? JSON.parse(text) : null;
  } catch {
    parsed = text;
  }
  return { status: res.status, body: parsed };
}

async function callCancelPost(
  POST: typeof import("@/app/api/work/runs/[runId]/cancel/route").POST,
  runId: string,
): Promise<{ status: number; body: unknown }> {
  const res = await POST(new Request("http://localhost:3000/test", {
    method: "POST",
  }), {
    params: Promise.resolve({ runId }),
  });
  return { status: res.status, body: await res.json() };
}

describeIfDb("/api/work/threads/[threadId]/runs POST", () => {
  let orgId: string;
  let threadId: string;
  let POST: typeof import("@/app/api/work/threads/[threadId]/runs/route").POST;

  beforeAll(async () => {
    const mod = await import("@/app/api/work/threads/[threadId]/runs/route");
    POST = mod.POST;
  });

  beforeEach(async () => {
    orgId = uniqueOrgId("api-work-runs");
    await createTestOrg(orgId);
    mockGetOrgId.mockResolvedValue(orgId);
    mockEnqueue.mockResolvedValue("queue-id-stub");
    mockResolveBackend.mockResolvedValue({ id: "ax", run: vi.fn() });

    const ins = await db()
      .insert(work_thread)
      .values({ org_id: orgId, title: "" })
      .returning();
    threadId = ins[0]!.id;
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
    vi.clearAllMocks();
  });

  afterAll(async () => {
    await pool().end();
  });

  // After the in-process refactor (phase 3) the POST route no longer
  // creates a processing_job or enqueues a WORK_RUN job — it fires
  // runChatTurn() in the Next.js process via the registry. The work_run
  // row + JSON response are still the synchronous contract this test
  // verifies; the fire-and-forget runChatTurn runs in the background
  // with a mocked backend and doesn't affect the assertions.
  it("creates a work_run row and returns the runId + backend synchronously", async () => {
    const res = await callRunsPost(POST, {
      threadId,
      body: { message: "What's the revenue?" },
    });

    expect(res.status).toBe(200);
    const { runId, backend } = res.body as { runId: string; backend: string };
    expect(runId).toBeTruthy();
    expect(backend).toBe("ax");

    const runs = await db()
      .select({ status: work_run.status, backend: work_run.backend })
      .from(work_run)
      .where(eq(work_run.id, runId))
      .limit(1);
    expect(runs[0]).toBeDefined();
    expect(runs[0]?.backend).toBe("ax");

    // Old behavior gone: no processing_job, no enqueue.
    const procs = await db()
      .select({ id: processing_job.id })
      .from(processing_job)
      .where(eq(processing_job.org_id, orgId));
    expect(procs).toHaveLength(0);
    expect(mockEnqueue).not.toHaveBeenCalled();
  });

  it("returns 429 and creates no run when the organization spending limit is full", async () => {
    await pool().query(
      "update spend_limit set org_hourly_micros = 1000000 where org_id = $1 and workflow_id is null",
      [orgId],
    );
    const res = await callRunsPost(POST, {
      threadId,
      body: { message: "What's the revenue?" },
    });

    expect(res.status).toBe(429);
    expect(res.body).toMatchObject({
      code: "spend_budget_exhausted",
      budget: "org_hourly",
      limitUsd: 1,
      reservationUsd: 5,
    });
    expect((res.body as { error: string }).error).toMatch(/hourly spending limit of \$1\.00 for this organization/);
    const runs = await db()
      .select({ id: work_run.id })
      .from(work_run)
      .where(eq(work_run.org_id, orgId));
    expect(runs).toHaveLength(0);
  });

  it("rejects empty message with 400", async () => {
    const res = await callRunsPost(POST, { threadId, body: { message: "" } });
    expect(res.status).toBe(400);
    expect(mockEnqueue).not.toHaveBeenCalled();
  });

  it("returns 404 when the thread doesn't belong to the org", async () => {
    const fakeId = "00000000-0000-0000-0000-000000000000";
    const res = await callRunsPost(POST, {
      threadId: fakeId,
      body: { message: "anything" },
    });
    expect(res.status).toBe(404);
    expect(mockEnqueue).not.toHaveBeenCalled();
  });

  // Old test "rolls work_run + processing_job to failed when enqueue
  // throws" removed: there's no enqueue path on this route anymore.
  // Backend failures inside the fire-and-forget runChatTurn are exercised
  // by the worker-side run-chat-turn integration tests instead.

  it("recovers a running run whose in-process controller was lost", async () => {
    const [{ id: runId }] = await db()
      .insert(work_run)
      .values({
        org_id: orgId,
        thread_id: threadId,
        backend: "ax",
        status: "running",
      })
      .returning({ id: work_run.id });
    const { POST: cancel } = await import(
      "@/app/api/work/runs/[runId]/cancel/route"
    );

    const res = await callCancelPost(cancel, runId);

    expect(res.status).toBe(200);
    expect(res.body).toEqual({ ok: true, recovered: true });
    const [stored] = await db()
      .select({
        status: work_run.status,
        error: work_run.error,
        finishedAt: work_run.finished_at,
      })
      .from(work_run)
      .where(eq(work_run.id, runId));
    expect(stored?.status).toBe("cancelled");
    expect(stored?.error).toContain("process running the agent is no longer available");
    expect(stored?.finishedAt).toBeInstanceOf(Date);

    const events = await db()
      .select({ kind: work_run_event.kind, payload: work_run_event.payload })
      .from(work_run_event)
      .where(eq(work_run_event.run_id, runId));
    expect(events).toEqual([
      { kind: "done", payload: { type: "done", result: { status: "cancelled" } } },
    ]);

    // A detached completion cannot revive a run after cancellation.
    await finishWorkRun(runId, "completed", null);
    const [afterLateFinish] = await db()
      .select({ status: work_run.status })
      .from(work_run)
      .where(eq(work_run.id, runId));
    expect(afterLateFinish?.status).toBe("cancelled");
  });
});
