import "server-only";
import type { AgentEvent } from "@neko/llm";

// globalThis-stashed so HMR doesn't drop in-flight runs in dev.
export type RunSubscriber = (event: AgentEvent, id: number) => void;

export type RunEntry = {
  runId: string;
  threadId: string;
  orgId: string;
  abortController: AbortController;
  subscribers: Set<RunSubscriber>;
};

declare global {
  var __neko_run_registry: Map<string, RunEntry> | undefined;
}

const runs: Map<string, RunEntry> =
  globalThis.__neko_run_registry ??
  (globalThis.__neko_run_registry = new Map());

export function registerRun(entry: RunEntry): void {
  runs.set(entry.runId, entry);
}

export function getRun(runId: string): RunEntry | undefined {
  return runs.get(runId);
}

export function unregisterRun(runId: string): void {
  runs.delete(runId);
}

export function abortRun(runId: string): boolean {
  const entry = runs.get(runId);
  if (!entry) return false;
  entry.abortController.abort();
  return true;
}

export function subscribeToRun(
  runId: string,
  subscriber: RunSubscriber,
): (() => void) | null {
  const entry = runs.get(runId);
  if (!entry) return null;
  entry.subscribers.add(subscriber);
  return () => {
    entry.subscribers.delete(subscriber);
  };
}

export function notifyRunSubscribers(
  runId: string,
  event: AgentEvent,
  id: number,
): void {
  const entry = runs.get(runId);
  if (!entry) return;
  for (const sub of entry.subscribers) {
    try {
      sub(event, id);
    } catch (err) {
      console.error(`[neko-run-registry] subscriber threw for ${runId}:`, err);
    }
  }
}
