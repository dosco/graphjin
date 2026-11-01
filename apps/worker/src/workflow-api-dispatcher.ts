import { startupEvent, startupPhase, withStartupTrace } from "@neko/telemetry/startup";
import { enqueue, QUEUE, type WorkflowRunFirePayload } from "@neko/db/jobs";
import {
  expireWorkflowApiResults,
  leasePendingWorkflowApiAdmissions,
  markWorkflowApiAdmissionEnqueued,
  recoverStaleWorkflowApiAdmissions,
  releaseWorkflowApiAdmissionDispatch,
  type LeasedWorkflowApiAdmission,
} from "@neko/llm/workflows";

export const WORKFLOW_API_DISPATCH_INTERVAL_MS = 5_000;

export type WorkflowApiDispatcherDependencies = {
  lease: (input: { now: Date }) => Promise<LeasedWorkflowApiAdmission[]>;
  enqueue: (
    payload: WorkflowRunFirePayload,
    admission: LeasedWorkflowApiAdmission,
  ) => Promise<string | null>;
  markEnqueued: (
    admissionId: string,
    queueJobId: string,
    now: Date,
  ) => Promise<void>;
  release: (
    admissionId: string,
    errorCode: string,
    now: Date,
  ) => Promise<void>;
  recover: (now: Date) => Promise<number>;
  expire: (now: Date) => Promise<number>;
};

const defaultDependencies: WorkflowApiDispatcherDependencies = {
  lease: (input) => leasePendingWorkflowApiAdmissions(input),
  enqueue: (payload) =>
    enqueue(QUEUE.WORKFLOW_RUN_FIRE, payload, {
      retryLimit: 2,
      retryDelay: 15,
      retryBackoff: true,
    }),
  markEnqueued: markWorkflowApiAdmissionEnqueued,
  release: releaseWorkflowApiAdmissionDispatch,
  recover: recoverStaleWorkflowApiAdmissions,
  expire: expireWorkflowApiResults,
};

let running = false;

export async function runWorkflowApiDispatcherTick(input: {
  now?: Date;
  dependencies?: Partial<WorkflowApiDispatcherDependencies>;
} = {}): Promise<{ dispatched: number; recovered: number; expired: number }> {
  if (running) return { dispatched: 0, recovered: 0, expired: 0 };
  running = true;
  const now = input.now ?? new Date();
  const deps = { ...defaultDependencies, ...(input.dependencies ?? {}) };
  try {
    const [recovered, expired] = await Promise.all([
      deps.recover(now),
      deps.expire(now),
    ]);
    const admissions = await deps.lease({ now });
    let dispatched = 0;
    for (const admission of admissions) {
      const payload: WorkflowRunFirePayload = {
        orgId: admission.orgId,
        workflowId: admission.workflowId,
        triggerKind: "api",
        apiAdmissionId: admission.id,
        workflowRunId: admission.workflowRunId,
        workRunId: admission.workRunId,
        threadId: admission.threadId,
        executionMode: admission.executionMode,
        admittedAt: admission.admittedAt.toISOString(),
        queueAttempt: admission.attempts,
      };
      try {
        const queueJobId = await withStartupTrace({ workflowRunId: admission.workflowRunId, runId: admission.workRunId, threadId: admission.threadId }, () => {
          startupEvent("workflow.admission_wait", { durationMs: Math.max(0, now.getTime() - admission.admittedAt.getTime()) });
          return startupPhase("workflow.enqueue", () => deps.enqueue(payload, admission));
        });
        if (!queueJobId) throw new Error("queue_rejected");
        await deps.markEnqueued(admission.id, queueJobId, now);
        dispatched += 1;
      } catch (error) {
        const code =
          error instanceof Error && error.message === "queue_rejected"
            ? "queue_rejected"
            : "queue_unavailable";
        await deps.release(admission.id, code, now);
      }
    }
    if (dispatched > 0 || recovered > 0 || expired > 0) {
      console.log(
        `[workflow-api-dispatcher] dispatched=${dispatched} recovered=${recovered} expired=${expired}`,
      );
    }
    return { dispatched, recovered, expired };
  } finally {
    running = false;
  }
}

export async function startWorkflowApiDispatcher(input: {
  intervalMs?: number;
  dependencies?: Partial<WorkflowApiDispatcherDependencies>;
} = {}): Promise<{ stop(): void }> {
  await runWorkflowApiDispatcherTick({ dependencies: input.dependencies });
  const timer = setInterval(() => {
    void runWorkflowApiDispatcherTick({ dependencies: input.dependencies }).catch(
      (error) => {
        console.error(
          `[workflow-api-dispatcher] tick failed: ${error instanceof Error ? error.message : String(error)}`,
        );
      },
    );
  }, input.intervalMs ?? WORKFLOW_API_DISPATCH_INTERVAL_MS);
  timer.unref();
  return { stop: () => clearInterval(timer) };
}

export function resetWorkflowApiDispatcherForTesting(): void {
  running = false;
}
