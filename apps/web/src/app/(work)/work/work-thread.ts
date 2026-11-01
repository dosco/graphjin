import type { MessageRecord, RunRecord, ThreadBundle, WorkEvent } from "./work-types";

export function prependOlder(prev: ThreadBundle, older: ThreadBundle): ThreadBundle {
  const runIds = new Set(prev.runs.map((run) => run.id));
  const messageIds = new Set(prev.messages.map((message) => message.id));
  return {
    ...prev,
    runs: [...older.runs.filter((run) => !runIds.has(run.id)), ...prev.runs],
    messages: [...older.messages.filter((message) => !messageIds.has(message.id)), ...prev.messages],
    eventsByRun: { ...older.eventsByRun, ...prev.eventsByRun },
    hasOlder: older.hasOlder,
  };
}

export function isRunInFlight(run: RunRecord): boolean {
  return run.status === "queued" || run.status === "running";
}

export function latestInFlightRun(runs: RunRecord[]): RunRecord | null {
  for (let i = runs.length - 1; i >= 0; i -= 1) {
    if (isRunInFlight(runs[i])) return runs[i];
  }
  return null;
}

function needsAssistantTimelinePlaceholder(
  run: RunRecord,
  events: WorkEvent[] | undefined,
): boolean {
  return isRunInFlight(run) || Boolean(run.error) || Boolean(events?.length);
}

export function withAssistantTimelinePlaceholders(bundle: ThreadBundle): ThreadBundle {
  const placeholders: MessageRecord[] = [];
  const assistantRunIds = new Set(
    bundle.messages
      .filter((message) => message.role === "assistant" && message.runId)
      .map((message) => message.runId as string),
  );

  for (const run of bundle.runs) {
    if (assistantRunIds.has(run.id)) continue;
    if (!needsAssistantTimelinePlaceholder(run, bundle.eventsByRun[run.id])) {
      continue;
    }
    placeholders.push({
      id: `assistant-${run.id}`,
      runId: run.id,
      role: "assistant",
      content: "",
      createdAt: run.createdAt,
    });
  }

  if (placeholders.length === 0) return bundle;

  const messages = [...bundle.messages];
  for (const placeholder of placeholders) {
    // After the run's own question, or at the run's start time when it has none (a routine run).
    const asked = messages.findLastIndex(
      (message) => message.runId === placeholder.runId && message.role === "user",
    );
    const later = messages.findIndex((message) => message.createdAt > placeholder.createdAt);
    const insertAt = asked >= 0 ? asked + 1 : later >= 0 ? later : messages.length;
    messages.splice(insertAt, 0, placeholder);
  }

  return { ...bundle, messages };
}

export function runWasCancelled(event: { type: string; result?: unknown }): boolean {
  return event.type === "done" && (event.result as { status?: string } | undefined)?.status === "cancelled";
}
