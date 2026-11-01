import type { WorkflowStep } from "./store";

/**
 * Refuses a workflow saved with script steps. OpenNeko runs no scripts, so
 * the step would silently do nothing; the operator rewrites it as a step the
 * agent carries out with its tools.
 */
export function assertNoScriptSteps(steps: readonly WorkflowStep[]): void {
  const scripted = steps.filter((step) => (step as { script?: unknown }).script != null);
  if (scripted.length === 0) return;
  throw new Error(
    `This workflow has script steps (${scripted.map((step) => step.id).join(", ")}), and OpenNeko no longer runs scripts. ` +
      "Edit the workflow and describe each of those steps for the agent to carry out.",
  );
}
