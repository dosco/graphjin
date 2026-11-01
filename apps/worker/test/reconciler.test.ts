import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
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
  inArray,
  pool,
  processing_job,
  sql,
  work_run,
  work_thread,
  workflow_definition,
  workflow_run,
} from "@neko/db";
import { boss, QUEUE } from "@neko/db/jobs";
import {
  reconcileStaleProcessingJobs,
  reconcileStaleRuns,
} from "../src/reconciler";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[reconciler] skipping: Postgres unreachable.");
}

async function ensurePgbossSchema(): Promise<boolean> {
  try {
    // pg-boss v10 partitions pgboss.job by queue. Inserts fail with
    // "no partition of relation 'job' found for row" when no queue
    // has been created. Skip these tests cleanly when there isn't at
    // least one partition (i.e. a fresh CI Postgres without queues).
    const result = await db().execute(
      sql`SELECT count(*)::int AS n FROM pg_inherits WHERE inhparent = 'pgboss.job'::regclass`,
    );
    const rows = (result as { rows?: Array<{ n: number }> }).rows ?? [];
    return rows.length > 0 && rows[0].n > 0;
  } catch {
    return false;
  }
}

async function insertPgbossJob(args: {
  name: string;
  processingJobId: string;
  state: "completed" | "failed" | "cancelled" | "active" | "created" | "retry";
  outputMessage?: string;
}): Promise<void> {
  const output = args.outputMessage
    ? sql`${JSON.stringify({ value: { message: args.outputMessage } })}::jsonb`
    : sql`NULL`;
  await db().execute(sql`
    INSERT INTO pgboss.job (name, data, state, output)
    VALUES (
      ${args.name},
      ${JSON.stringify({ processingJobId: args.processingJobId, orgId: "x" })}::jsonb,
      ${args.state}::pgboss.job_state,
      ${output}
    )
  `);
}

async function insertProcessingJob(args: {
  orgId: string;
  kind: string;
  status: "queued" | "running";
}): Promise<string> {
  const ins = await db()
    .insert(processing_job)
    .values({
      org_id: args.orgId,
      kind: args.kind,
      status: args.status,
      trigger: "test",
      ...(args.status === "running" ? { started_at: new Date() } : {}),
    })
    .returning({ id: processing_job.id });
  return ins[0]!.id;
}

async function readProcessingJob(id: string) {
  const rows = await db()
    .select({
      status: processing_job.status,
      error: processing_job.error,
      finished_at: processing_job.finished_at,
    })
    .from(processing_job)
    .where(eq(processing_job.id, id))
    .limit(1);
  return rows[0]!;
}

const pgbossOk = reachable ? await ensurePgbossSchema() : false;
const describeIfReady = pgbossOk ? describeIfDb : describe.skip;

if (reachable && !pgbossOk) {
  console.warn("[reconciler] skipping: pgboss schema not initialized.");
}

describeIfDb("reconcileStaleRuns — queued runs with a dead launcher", () => {
  const orgId = uniqueOrgId("reconq");

  beforeAll(async () => {
    await createTestOrg(orgId);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
  });

  async function insertRun(args: { status: string; createdAt: Date }) {
    const [thread] = await db()
      .insert(work_thread)
      .values({ org_id: orgId, title: "t" })
      .returning({ id: work_thread.id });
    const [run] = await db()
      .insert(work_run)
      .values({
        org_id: orgId,
        thread_id: thread.id,
        status: args.status,
        backend: "ax",
        created_at: args.createdAt,
        updated_at: args.createdAt,
      })
      .returning({ id: work_run.id });
    return run.id;
  }

  it("cancels queued runs older than the launcher window, keeps fresh ones", async () => {
    const oldId = await insertRun({
      status: "queued",
      createdAt: new Date(Date.now() - 10 * 60_000),
    });
    const freshId = await insertRun({ status: "queued", createdAt: new Date() });

    await reconcileStaleRuns();

    const [oldRun] = await db()
      .select({ status: work_run.status, error: work_run.error })
      .from(work_run)
      .where(eq(work_run.id, oldId));
    expect(oldRun.status).toBe("cancelled");
    expect(oldRun.error).toMatch(/Never started/);

    const [freshRun] = await db()
      .select({ status: work_run.status })
      .from(work_run)
      .where(eq(work_run.id, freshId));
    expect(freshRun.status).toBe("queued");
  });
});

describeIfReady("reconcileStaleProcessingJobs", () => {
  let orgId: string;

  beforeAll(async () => {
    // pg-boss v10 partitions pgboss.job by queue name. Inserts into a queue
    // whose partition isn't created yet fail with "no partition of relation
    // 'job' found for row". Pre-create every queue this suite touches (and
    // a couple of others for safety) so the SQL INSERT in insertPgbossJob
    // lands cleanly.
    const b = await boss();
    for (const name of Object.values(QUEUE)) {
      await b.createQueue(name).catch(() => {
        // idempotent — ignore "queue already exists"
      });
    }
  });

  beforeEach(async () => {
    orgId = uniqueOrgId("reconciler");
    await createTestOrg(orgId);
  });

  afterEach(async () => {
    await db().execute(sql`
      DELETE FROM pgboss.job WHERE data->>'processingJobId' IN (
        SELECT id::text FROM processing_job WHERE org_id = ${orgId}
      )
    `);
    await deleteTestOrg(orgId);
  });

  it("mirrors pg-boss completed state to processing_job=succeeded", async () => {
    const jobId = await insertProcessingJob({
      orgId,
      kind: "business_profile_build",
      status: "running",
    });
    await insertPgbossJob({
      name: "business_profile_build",
      processingJobId: jobId,
      state: "completed",
    });

    const summary = await reconcileStaleProcessingJobs({ orgId });
    expect(summary.succeeded).toBeGreaterThanOrEqual(1);

    const row = await readProcessingJob(jobId);
    expect(row.status).toBe("succeeded");
    expect(row.error).toBeNull();
    expect(row.finished_at).not.toBeNull();
  });

  it("mirrors pg-boss failed state and propagates the error message", async () => {
    const jobId = await insertProcessingJob({
      orgId,
      kind: "business_profile_build",
      status: "running",
    });
    await insertPgbossJob({
      name: "business_profile_build",
      processingJobId: jobId,
      state: "failed",
      outputMessage: "agent crashed: ENOENT",
    });

    await reconcileStaleProcessingJobs({ orgId });

    const row = await readProcessingJob(jobId);
    expect(row.status).toBe("failed");
    expect(row.error).toBe("agent crashed: ENOENT");
  });

  it("treats pg-boss timeout (failed state with timeout message) as failed", async () => {
    const jobId = await insertProcessingJob({
      orgId,
      kind: "business_profile_build",
      status: "running",
    });
    await insertPgbossJob({
      name: "business_profile_build",
      processingJobId: jobId,
      state: "failed",
      outputMessage: "job failed by timeout in active state",
    });

    await reconcileStaleProcessingJobs({ orgId });
    const row = await readProcessingJob(jobId);
    expect(row.status).toBe("failed");
    expect(row.error).toContain("timeout");
  });

  it("propagates metric_refresh failure to metric.last_refresh_status", async () => {
    const metricInsert = await db()
      .insert(metric)
      .values({
        org_id: orgId,
        role: "CEO",
        slug: "rec-test",
        source: "bootstrap",
        title: "Reconciler test",
        why: "Validate the reconciler propagates failure to metric",
        chart_hint: "kpi",
        active: true,
        last_refresh_status: "pending",
      })
      .returning({ id: metric.id });
    const metricId = metricInsert[0]!.id;

    const jobId = await insertProcessingJob({
      orgId,
      kind: "metric_refresh",
      status: "queued",
    });
    await db()
      .update(metric)
      .set({ last_refresh_job_id: jobId })
      .where(eq(metric.id, metricId));
    await insertPgbossJob({
      name: "metric_refresh",
      processingJobId: jobId,
      state: "failed",
      outputMessage: "upstream provider unavailable",
    });

    await reconcileStaleProcessingJobs({ orgId });

    const m = await db()
      .select({
        status: metric.last_refresh_status,
        err: metric.last_refresh_error,
      })
      .from(metric)
      .where(eq(metric.id, metricId))
      .limit(1);
    expect(m[0]?.status).toBe("failed");
    expect(m[0]?.err).toBe("upstream provider unavailable");
  });

  it("resets running -> queued when pg-boss is still active (handler crashed mid-flight)", async () => {
    const jobId = await insertProcessingJob({
      orgId,
      kind: "business_profile_build",
      status: "running",
    });
    await insertPgbossJob({
      name: "business_profile_build",
      processingJobId: jobId,
      state: "active",
    });

    const summary = await reconcileStaleProcessingJobs({ orgId });
    expect(summary.requeued).toBeGreaterThanOrEqual(1);

    const row = await readProcessingJob(jobId);
    expect(row.status).toBe("queued");
    expect(row.error).toBeNull();
  });

  it("leaves a queued row alone when pg-boss is still active", async () => {
    const jobId = await insertProcessingJob({
      orgId,
      kind: "business_profile_build",
      status: "queued",
    });
    await insertPgbossJob({
      name: "business_profile_build",
      processingJobId: jobId,
      state: "created",
    });

    await reconcileStaleProcessingJobs({ orgId });
    const row = await readProcessingJob(jobId);
    expect(row.status).toBe("queued");
  });

  it("marks orphaned (no pg-boss row) processing_job as failed", async () => {
    const jobId = await insertProcessingJob({
      orgId,
      kind: "business_profile_build",
      status: "queued",
    });

    const summary = await reconcileStaleProcessingJobs({ orgId });
    expect(summary.lost).toBeGreaterThanOrEqual(1);

    const row = await readProcessingJob(jobId);
    expect(row.status).toBe("failed");
    expect(row.error).toContain("lost from queue");
  });

  it("respects minAgeMs — leaves recent rows untouched", async () => {
    const jobId = await insertProcessingJob({
      orgId,
      kind: "business_profile_build",
      status: "running",
    });
    await insertPgbossJob({
      name: "business_profile_build",
      processingJobId: jobId,
      state: "failed",
      outputMessage: "permanent",
    });

    const summary = await reconcileStaleProcessingJobs({
      minAgeMs: 10_000,
      orgId,
    });
    expect(summary.failed).toBe(0);

    const row = await readProcessingJob(jobId);
    expect(row.status).toBe("running");
  });
});

describeIfDb("reconcileStaleRuns", () => {
  let orgId: string;

  beforeEach(async () => {
    orgId = uniqueOrgId("stale-runs");
    await createTestOrg(orgId);
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
  });

  async function insertRunningRun(updatedAt: Date): Promise<{
    threadId: string;
    workRunId: string;
  }> {
    const [thread] = await db()
      .insert(work_thread)
      .values({ org_id: orgId, title: "t", channel: "workflow" })
      .returning({ id: work_thread.id });
    const [run] = await db()
      .insert(work_run)
      .values({
        org_id: orgId,
        thread_id: thread!.id,
        backend: "ax",
        status: "running",
        created_at: updatedAt,
        updated_at: updatedAt,
      })
      .returning({ id: work_run.id });
    return { threadId: thread!.id, workRunId: run!.id };
  }

  it("cancels a work_run + its workflow_run stranded in running past the age threshold", async () => {
    const stale = new Date(Date.now() - 20 * 60_000);
    const { threadId, workRunId } = await insertRunningRun(stale);
    const [wf] = await db()
      .insert(workflow_definition)
      .values({ org_id: orgId, name: "zombie wf" })
      .returning({ id: workflow_definition.id });
    const [wfRun] = await db()
      .insert(workflow_run)
      .values({
        org_id: orgId,
        workflow_id: wf!.id,
        thread_id: threadId,
        work_run_id: workRunId,
        trigger_kind: "subscription",
        status: "running",
      })
      .returning({ id: workflow_run.id });

    const res = await reconcileStaleRuns({ minAgeMs: 11 * 60_000 });
    expect(res.cancelled).toBeGreaterThanOrEqual(1);

    const wr = await db()
      .select({ status: work_run.status, error: work_run.error })
      .from(work_run)
      .where(eq(work_run.id, workRunId));
    expect(wr[0]?.status).toBe("cancelled");
    expect(wr[0]?.error).toContain("Interrupted");

    const wfr = await db()
      .select({ status: workflow_run.status })
      .from(workflow_run)
      .where(eq(workflow_run.id, wfRun!.id));
    expect(wfr[0]?.status).toBe("cancelled");
  });

  it("at boot cancels the worker's own running runs and leaves web chat runs to the web", async () => {
    const workerRun = await insertRunningRun(new Date());
    const chatRun = await insertRunningRun(new Date());
    await pool().query(
      `insert into spend_reservation (org_id, work_run_id, source, reserved_micros, created_at) values ($1, $2, 'chat', 0, now())`,
      [orgId, chatRun.workRunId],
    );
    await reconcileStaleRuns({ workerOwnedOnly: true });
    const statuses = await db()
      .select({ id: work_run.id, status: work_run.status })
      .from(work_run)
      .where(inArray(work_run.id, [workerRun.workRunId, chatRun.workRunId]));
    const byId = Object.fromEntries(statuses.map((row) => [row.id, row.status]));
    expect(byId[workerRun.workRunId]).toBe("cancelled");
    expect(byId[chatRun.workRunId]).toBe("running");
  });

  it("leaves a recently-updated running run alone (respects minAgeMs)", async () => {
    const { workRunId } = await insertRunningRun(new Date());
    await reconcileStaleRuns({ minAgeMs: 11 * 60_000 });
    const wr = await db()
      .select({ status: work_run.status })
      .from(work_run)
      .where(eq(work_run.id, workRunId));
    expect(wr[0]?.status).toBe("running");
  });
});

if (reachable) {
  afterAll(async () => {
    await pool().end();
  });
}
