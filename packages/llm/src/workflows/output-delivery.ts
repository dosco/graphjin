import type { WorkflowOutputRecord } from "./store";

/**
 * Optional delivery hook — the worker registers one at startup to fan a
 * newly-emitted output out to bound channels (Slack, Telegram, ...). A
 * registered seam (like registerActionAdapter) so packages/llm never depends
 * on the worker's channel registry. Fire-and-forget; never fails the run.
 */
export type WorkflowOutputDeliveryHook = (
  orgId: string,
  output: WorkflowOutputRecord,
) => Promise<void> | void;

let outputDeliveryHook: WorkflowOutputDeliveryHook | null = null;

export function setWorkflowOutputDeliveryHook(
  hook: WorkflowOutputDeliveryHook | null,
): void {
  outputDeliveryHook = hook;
}

export function notifyWorkflowOutputDeliveryHook(
  orgId: string,
  output: WorkflowOutputRecord,
): void {
  if (!outputDeliveryHook) return;
  const hook = outputDeliveryHook;
  // async IIFE so a synchronous throw in the hook is captured as a rejection too.
  void (async () => hook(orgId, output))().catch((err) => {
    console.warn(
      `[workflow-output] delivery hook failed: ${err instanceof Error ? err.message : err}`,
    );
  });
}
