// Browser durations share one monotonic clock; never subtract browser/server timestamps.
type Timing = { threadId: string; started: number; values: Record<string, number>; output: boolean };
const pending = new Map<string, Timing>();
function report(runId: string, timing: Timing): void {
  void fetch(`/api/work/threads/${timing.threadId}/runs/${runId}/timing`, {
    method: "POST", headers: { "content-type": "application/json" }, keepalive: true,
    body: JSON.stringify(timing.values),
  }).catch(() => {});
}
export function acknowledgeWorkStartup(threadId: string, runId: string, started: number): void {
  // Bounded when a user navigates away before receiving output.
  if (pending.size >= 32) pending.delete(pending.keys().next().value!);
  const timing = { threadId, started, values: { acknowledgementMs: performance.now() - started }, output: false };
  pending.set(runId, timing);
  report(runId, timing);
}
export function markWorkStartup(runId: string, phase: "streamOpenMs" | "firstEventMs" | "firstOutputMs" | "doneMs"): void {
  const timing = pending.get(runId);
  if (!timing || timing.values[phase] !== undefined) return;
  timing.values[phase] = performance.now() - timing.started;
  if (phase === "firstOutputMs") {
    timing.output = true;
    // This is a paint opportunity after dispatching the React update, not proof of visible pixels.
    requestAnimationFrame(() => requestAnimationFrame(() => {
      timing.values.paintOpportunityMs = performance.now() - timing.started;
      timing.values.documentVisible = document.visibilityState === "visible" ? 1 : 0;
      report(runId, timing);
      pending.delete(runId);
    }));
  } else if (phase === "doneMs" && !timing.output) {
    report(runId, timing);
    pending.delete(runId);
  }
}
