import { enqueue, QUEUE, type WorkflowRunFirePayload } from "@neko/db/jobs";
import {
  leasePendingWorkflowFirings,
  markWorkflowFiringEnqueued,
  materializeDueWorkflowFirings,
  recordWorkflowSchedulerFailure,
  recordWorkflowSchedulerSuccess,
  recoverStaleWorkflowScheduleFirings,
  releaseWorkflowFiringDispatch,
  type LeasedWorkflowFiring,
  type MaterializeScheduleResult,
} from "@neko/llm/workflows";

export const WORKFLOW_SCHEDULER_INTERVAL_MS = 30_000;
export const WORKFLOW_SCHEDULER_STALE_AFTER_MS = 120_000;

export type WorkflowSchedulerHealth = {
  status: "starting" | "ok" | "degraded";
  running: boolean;
  stale: boolean;
  startedAt: string;
  lastAttemptAt: string | null;
  lastSuccessAt: string | null;
  lastErrorAt: string | null;
  lastError: string | null;
  consecutiveFailures: number;
  materialized: number;
  dispatched: number;
  recovered: number;
};

type SchedulerState = {
  running: boolean;
  startedAt: Date;
  lastAttemptAt: Date | null;
  lastSuccessAt: Date | null;
  lastErrorAt: Date | null;
  lastError: string | null;
  consecutiveFailures: number;
  materialized: number;
  dispatched: number;
  recovered: number;
};

export type WorkflowSchedulerDependencies = {
  materialize: (now: Date) => Promise<MaterializeScheduleResult>;
  recover: (now: Date) => Promise<number>;
  lease: (input: { now: Date }) => Promise<LeasedWorkflowFiring[]>;
  enqueue: (
    payload: WorkflowRunFirePayload,
    firing: LeasedWorkflowFiring,
  ) => Promise<string | null>;
  markEnqueued: (
    firingId: string,
    queueJobId: string,
    now: Date,
  ) => Promise<void>;
  releaseDispatch: (
    firingId: string,
    error: unknown,
    now: Date,
  ) => Promise<void>;
  recordSuccess: (input: { dispatched: number; now: Date }) => Promise<void>;
  recordFailure: (error: unknown, now: Date) => Promise<void>;
};

const defaultDependencies: WorkflowSchedulerDependencies = {
  materialize: materializeDueWorkflowFirings,
  recover: recoverStaleWorkflowScheduleFirings,
  lease: (input) => leasePendingWorkflowFirings(input),
  enqueue: (payload, firing) =>
    enqueue(QUEUE.WORKFLOW_RUN_FIRE, payload, {
      retryLimit: 2,
      retryDelay: 30,
      retryBackoff: true,
    }),
  markEnqueued: markWorkflowFiringEnqueued,
  releaseDispatch: releaseWorkflowFiringDispatch,
  recordSuccess: recordWorkflowSchedulerSuccess,
  recordFailure: recordWorkflowSchedulerFailure,
};

const state: SchedulerState = {
  running: false,
  startedAt: new Date(),
  lastAttemptAt: null,
  lastSuccessAt: null,
  lastErrorAt: null,
  lastError: null,
  consecutiveFailures: 0,
  materialized: 0,
  dispatched: 0,
  recovered: 0,
};

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function getWorkflowSchedulerHealth(
  now = new Date(),
): WorkflowSchedulerHealth {
  const stale =
    state.lastSuccessAt !== null &&
    now.getTime() - state.lastSuccessAt.getTime() >
      WORKFLOW_SCHEDULER_STALE_AFTER_MS;
  const status =
    state.lastSuccessAt === null
      ? state.lastError === null
        ? "starting"
        : "degraded"
      : stale || state.lastError !== null
        ? "degraded"
        : "ok";
  return {
    status,
    running: state.running,
    stale,
    startedAt: state.startedAt.toISOString(),
    lastAttemptAt: state.lastAttemptAt?.toISOString() ?? null,
    lastSuccessAt: state.lastSuccessAt?.toISOString() ?? null,
    lastErrorAt: state.lastErrorAt?.toISOString() ?? null,
    lastError: state.lastError,
    consecutiveFailures: state.consecutiveFailures,
    materialized: state.materialized,
    dispatched: state.dispatched,
    recovered: state.recovered,
  };
}

export async function runDurableWorkflowSchedulerTick(
  input: {
    now?: Date;
    dependencies?: Partial<WorkflowSchedulerDependencies>;
  } = {},
): Promise<WorkflowSchedulerHealth> {
  if (state.running) return getWorkflowSchedulerHealth(input.now);

  const now = input.now ?? new Date();
  const deps = { ...defaultDependencies, ...(input.dependencies ?? {}) };
  state.running = true;
  state.lastAttemptAt = now;

  try {
    const materialized = await deps.materialize(now);
    const recovered = await deps.recover(now);
    const firings = await deps.lease({ now });
    let dispatched = 0;
    const dispatchErrors: unknown[] = [];

    for (const firing of firings) {
      const payload: WorkflowRunFirePayload = {
        orgId: firing.orgId,
        workflowId: firing.workflowId,
        triggerKind: "cron",
        scheduleFiringId: firing.id,
        triggerPayload: {
          firingTime: firing.scheduledFor.toISOString(),
          scheduleFiringId: firing.id,
        },
      };
      try {
        const queueJobId = await deps.enqueue(payload, firing);
        if (!queueJobId) {
          throw new Error("pg-boss did not accept the workflow firing");
        }
        await deps.markEnqueued(firing.id, queueJobId, now);
        dispatched++;
      } catch (error) {
        await deps.releaseDispatch(firing.id, error, now);
        dispatchErrors.push(error);
        console.warn(
          `[workflow-scheduler] dispatch failed firing=${firing.id}: ${errorMessage(error)}`,
        );
      }
    }

    if (dispatchErrors.length > 0) {
      throw new Error(
        `${dispatchErrors.length}/${firings.length} workflow firing dispatches failed`,
      );
    }

    await deps.recordSuccess({ dispatched, now });
    state.lastSuccessAt = now;
    state.lastError = null;
    state.consecutiveFailures = 0;
    state.materialized = materialized.materialized;
    state.dispatched = dispatched;
    state.recovered = recovered;

    if (materialized.materialized > 0 || dispatched > 0 || recovered > 0) {
      console.log(
        `[workflow-scheduler] materialized=${materialized.materialized} dispatched=${dispatched} recovered=${recovered} coalesced=${materialized.coalescedOccurrences}`,
      );
    }
  } catch (error) {
    state.lastErrorAt = now;
    state.lastError = errorMessage(error);
    state.consecutiveFailures++;
    await deps.recordFailure(error, now).catch(() => undefined);
    console.error(`[workflow-scheduler] tick failed: ${state.lastError}`);
  } finally {
    state.running = false;
  }

  return getWorkflowSchedulerHealth(now);
}

export type WorkflowSchedulerController = {
  stop(): void;
};

export async function startDurableWorkflowScheduler(
  input: {
    intervalMs?: number;
    dependencies?: Partial<WorkflowSchedulerDependencies>;
  } = {},
): Promise<WorkflowSchedulerController> {
  state.startedAt = new Date();
  state.lastAttemptAt = null;
  state.lastSuccessAt = null;
  state.lastErrorAt = null;
  state.lastError = null;
  state.consecutiveFailures = 0;
  state.materialized = 0;
  state.dispatched = 0;
  state.recovered = 0;

  await runDurableWorkflowSchedulerTick({
    dependencies: input.dependencies,
  });
  const timer = setInterval(() => {
    void runDurableWorkflowSchedulerTick({
      dependencies: input.dependencies,
    });
  }, input.intervalMs ?? WORKFLOW_SCHEDULER_INTERVAL_MS);
  timer.unref();

  return {
    stop() {
      clearInterval(timer);
    },
  };
}

/** Reset only process-local observability state; intended for unit tests. */
export function resetWorkflowSchedulerHealthForTesting(): void {
  state.running = false;
  state.startedAt = new Date();
  state.lastAttemptAt = null;
  state.lastSuccessAt = null;
  state.lastErrorAt = null;
  state.lastError = null;
  state.consecutiveFailures = 0;
  state.materialized = 0;
  state.dispatched = 0;
  state.recovered = 0;
}
