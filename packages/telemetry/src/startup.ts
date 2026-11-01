import { AsyncLocalStorage } from "node:async_hooks";
import { randomUUID } from "node:crypto";
import { performance } from "node:perf_hooks";
import { errorCodeOf, observeSafely } from "./observer";
import { sanitizeAttributes } from "./redaction";
import type { HarnessObserver, ObservationInput } from "./types";

type StartupContext = {
  started?: number;
  requestId?: string;
  runId?: string;
  rootOperationId?: string;
  workflowRunId?: string;
  jobId?: string;
  threadId?: string;
  observer?: HarnessObserver;
  pending?: ObservationInput[];
  failedPhase?: string;
};
const current = new AsyncLocalStorage<{ trace: StartupContext; parent?: string }>();

export function withStartupTrace<T>(context: StartupContext, operation: () => T): T {
  return current.run({ trace: { started: performance.now(), ...current.getStore()?.trace, ...context, pending: [] } }, operation);
}

/** Link pre-run HTTP phases to the eventual run without creating a run before validation. */
export async function bindStartupRun(runId: string, observer: HarnessObserver, rootOperationId = `work:${runId}`): Promise<void> {
  const context = current.getStore()?.trace;
  if (!context) return;
  context.runId = runId;
  context.rootOperationId = rootOperationId;
  context.observer = observer;
  startupEvent("run.link", {});
  for (const observation of context.pending ?? []) await observeSafely(observer, {
    ...observation, parentOperationId: observation.parentOperationId ?? rootOperationId,
  });
  context.pending = [];
}

export function startupElapsedMs(): number | undefined {
  const started = current.getStore()?.trace.started;
  return started === undefined ? undefined : Math.max(0, performance.now() - started);
}

function metadata() {
  const context = current.getStore()?.trace;
  return {
    requestId: context?.requestId, runId: context?.runId, threadId: context?.threadId,
    workflowRunId: context?.workflowRunId, jobId: context?.jobId,
    version: process.env.OPENNEKO_VERSION ?? process.env.NEXT_PUBLIC_APP_VERSION ?? "unknown",
    commit: process.env.OPENNEKO_COMMIT ?? process.env.GIT_COMMIT ?? "unknown",
    pid: process.pid,
    elapsedMs: startupElapsedMs(),
  };
}

function log(phase: string, values: Record<string, unknown>): void {
  try { console.log(JSON.stringify({ type: "startup_timing", timestamp: new Date().toISOString(), ...metadata(), phase, ...sanitizeAttributes(values) })); } catch {}
}

async function observe(input: ObservationInput): Promise<void> {
  const context = current.getStore()?.trace;
  if (context?.observer) await observeSafely(context.observer, input);
  else if (context?.pending && context.pending.length < 128) context.pending.push(input);
}

export function startupEvent(phase: string, attributes: Record<string, unknown>): void {
  log(phase, { event: true, ...attributes });
}

/** Monotonic durations, paired spans on failure too; instrumentation never changes the result. */
export async function startupPhase<T>(phase: string, operation: () => Promise<T>, attributes: Record<string, unknown> = {}): Promise<T> {
  const context = current.getStore()?.trace;
  const operationId = `startup:${randomUUID()}`;
  const parentOperationId = current.getStore()?.parent ?? (context?.rootOperationId ?? (context?.runId ? `work:${context.runId}` : undefined));
  const started = performance.now();
  const startedAt = new Date().toISOString();
  const attrs = sanitizeAttributes({ "openneko.stage": `startup.${phase}`, ...attributes });
  await observe({ kind: "stage.start", operationId, parentOperationId, timestamp: startedAt, attributes: attrs });
  let ok = false;
  let failure: unknown;
  try {
    const result = await current.run({ trace: context ?? {}, parent: operationId }, operation);
    ok = true;
    return result;
  } catch (error) {
    failure = error;
    throw error;
  } finally {
    if (!ok && context) context.failedPhase ??= phase;
    const durationMs = Math.max(0, performance.now() - started);
    log(phase, { ...attributes, durationMs, ok, startedAt, operationId, parentOperationId });
    await observe({ kind: "stage.end", operationId, parentOperationId: parentOperationId ?? (context?.rootOperationId ?? (context?.runId ? `work:${context.runId}` : undefined)), status: ok ? "ok" : "error", attributes: attrs,
      ...(ok ? {} : failureDetail(failure)),
      measurements: { durationMs, coverage: "unavailable" } });
  }
}

function failureDetail(error: unknown) {
  return {
    errorType: error instanceof Error ? error.name : "unknown",
    errorCode: errorCodeOf(error),
    errorMessage: error instanceof Error ? error.message : String(error),
  };
}

/**
 * Ends the traced request's run as failed. A request that failed before its
 * run existed gets a run of its own, so its buffered startup phases export.
 */
export async function failStartupRun(
  error: unknown,
  createObserver: (runId: string) => HarnessObserver,
  attributes: Record<string, unknown> = {},
): Promise<void> {
  const context = current.getStore()?.trace;
  if (!context) return;
  const durationMs = startupElapsedMs() ?? 0;
  if (!context.observer) {
    const runId = context.requestId ?? randomUUID();
    const observer = createObserver(runId);
    await observeSafely(observer, {
      kind: "run.start", operationId: `work:${runId}`,
      timestamp: new Date(Date.now() - durationMs).toISOString(), attributes,
    });
    await bindStartupRun(runId, observer);
  }
  await observeSafely(context.observer, {
    kind: "run.end",
    operationId: context.rootOperationId ?? `work:${context.runId}`,
    status: "error",
    ...failureDetail(error),
    errorCode: errorCodeOf(error) ?? (context.failedPhase ? `startup.${context.failedPhase}` : "startup_failed"),
    attributes: { ...attributes, "openneko.outcome": "failed" },
    measurements: { durationMs, coverage: "unavailable" },
  });
}
