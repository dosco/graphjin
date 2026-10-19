import {
  and,
  db,
  eq,
  inArray,
  lte,
  metric,
  processing_job,
  sql,
  work_run,
  workflow_run,
} from "@neko/db";

export type ReconcileSummary = {
  succeeded: number;
  failed: number;
  requeued: number;
  lost: number;
};

const STALE_RUN_REASON =
  "Interrupted — the worker stopped before the run finished.";
const STALE_QUEUED_REASON =
  "Never started — the process that accepted this run stopped before launching it. Retry to run it again.";
// Queued runs are launched in-process moments after the insert; one stuck
// longer than this has lost its launcher (web restart/sleep, launch error).
const STALE_QUEUED_MIN_AGE_MS = 120_000;

/**
 * Cancel work_run / workflow_run rows stranded in "running" — a run that
 * outlived the process tracking it (worker SIGKILLed mid-run, before its own
 * finalizer could mark it cancelled). With minAgeMs past the agent's own timeout,
 * a still-"running" row is provably dead. The processing_job reconciler above
 * doesn't cover these tables.
 */
export async function reconcileStaleRuns(opts?: {
  minAgeMs?: number;
  /** Leave web chat runs alone; the web process owns and finishes them. */
  workerOwnedOnly?: boolean;
}): Promise<{ cancelled: number }> {
  const cutoff = new Date(Date.now() - (opts?.minAgeMs ?? 0));
  const now = new Date();

  const stale = await db()
    .update(work_run)
    .set({
      status: "cancelled",
      error: STALE_RUN_REASON,
      finished_at: now,
      updated_at: now,
    })
    .where(and(
      eq(work_run.status, "running"),
      lte(work_run.updated_at, cutoff),
      opts?.workerOwnedOnly
        ? sql`not exists (select 1 from spend_reservation sr where sr.work_run_id = ${work_run.id} and sr.source = 'chat')`
        : undefined,
    ))
    .returning({ id: work_run.id });

  // Queued chat runs have no durable queue behind them — the web process
  // launches them in-memory right after the insert. If that process dies or
  // errors at launch (laptop sleep, EADDRINUSE, restart), the row sits
  // "queued" forever. Cancel honestly so the UI offers Retry.
  const queuedCutoff = new Date(
    Date.now() - Math.max(opts?.minAgeMs ?? 0, STALE_QUEUED_MIN_AGE_MS),
  );
  const staleQueued = await db()
    .update(work_run)
    .set({
      status: "cancelled",
      error: STALE_QUEUED_REASON,
      finished_at: now,
      updated_at: now,
    })
    .where(
      and(eq(work_run.status, "queued"), lte(work_run.created_at, queuedCutoff)),
    )
    .returning({ id: work_run.id });
  stale.push(...staleQueued);
  if (stale.length === 0) return { cancelled: 0 };

  await db()
    .update(workflow_run)
    .set({
      status: "cancelled",
      error: STALE_RUN_REASON,
      finished_at: now,
      updated_at: now,
    })
    .where(
      and(
        inArray(workflow_run.work_run_id, stale.map((r) => r.id)),
        inArray(workflow_run.status, ["running", "queued"]),
      ),
    );
  return { cancelled: stale.length };
}

type BossRow = {
  state: string;
  output: { value?: { message?: string } } | null;
};

const TERMINAL_BOSS_STATES = new Set(["completed", "failed", "cancelled"]);
const ACTIVE_BOSS_STATES = new Set(["created", "active", "retry"]);

export async function reconcileStaleProcessingJobs(opts?: {
  minAgeMs?: number;
  orgId?: string;
}): Promise<ReconcileSummary> {
  const minAgeMs = opts?.minAgeMs ?? 0;

  const baseFilter = opts?.orgId
    ? and(
        eq(processing_job.org_id, opts.orgId),
        inArray(processing_job.status, ["queued", "running"]),
      )
    : inArray(processing_job.status, ["queued", "running"]);
  const filter = minAgeMs > 0
    ? and(
        baseFilter,
        lte(processing_job.updated_at, new Date(Date.now() - minAgeMs)),
      )
    : baseFilter;

  const candidates = await db()
    .select({
      id: processing_job.id,
      kind: processing_job.kind,
      status: processing_job.status,
    })
    .from(processing_job)
    .where(filter);

  const summary: ReconcileSummary = {
    succeeded: 0,
    failed: 0,
    requeued: 0,
    lost: 0,
  };

  for (const c of candidates) {
    const bossRows = await db().execute<BossRow>(sql`
      SELECT state, output
      FROM pgboss.job
      WHERE name = ${c.kind}
        AND data->>'processingJobId' = ${c.id}
      ORDER BY created_on DESC
      LIMIT 1
    `);
    const boss = (bossRows.rows[0] as BossRow | undefined) ?? null;
    const now = new Date();

    if (!boss) {
      const reason = "lost from queue (no pg-boss entry found)";
      await db()
        .update(processing_job)
        .set({ status: "failed", error: reason, finished_at: now, updated_at: now })
        .where(eq(processing_job.id, c.id));
      await failOwningMetricIfRefresh(c.id, c.kind, reason);
      summary.lost++;
      continue;
    }

    if (boss.state === "completed") {
      await db()
        .update(processing_job)
        .set({
          status: "succeeded",
          finished_at: now,
          error: null,
          updated_at: now,
        })
        .where(eq(processing_job.id, c.id));
      summary.succeeded++;
      continue;
    }

    if (TERMINAL_BOSS_STATES.has(boss.state)) {
      const msg =
        boss.output?.value?.message ?? `pg-boss state=${boss.state}`;
      await db()
        .update(processing_job)
        .set({ status: "failed", error: msg, finished_at: now, updated_at: now })
        .where(eq(processing_job.id, c.id));
      await failOwningMetricIfRefresh(c.id, c.kind, msg);
      summary.failed++;
      continue;
    }

    if (ACTIVE_BOSS_STATES.has(boss.state)) {
      if (c.status === "running") {
        await db()
          .update(processing_job)
          .set({
            status: "queued",
            started_at: null,
            finished_at: null,
            error: null,
            updated_at: now,
          })
          .where(eq(processing_job.id, c.id));
        summary.requeued++;
      }
      continue;
    }

    console.warn(
      `[reconciler] unknown pg-boss state="${boss.state}" for processing_job=${c.id}; leaving as-is`,
    );
  }

  return summary;
}

async function failOwningMetricIfRefresh(
  processingJobId: string,
  kind: string,
  errorMsg: string,
): Promise<void> {
  if (kind !== "metric_refresh") return;
  await db()
    .update(metric)
    .set({
      last_refresh_status: "failed",
      last_refresh_error: errorMsg,
      updated_at: new Date(),
    })
    .where(eq(metric.last_refresh_job_id, processingJobId));
}
