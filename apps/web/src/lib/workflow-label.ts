export const NO_WORKFLOW_LABEL = "OpenNeko AI";

export function workflowDisplayName(
  workflow: { name: string } | null | undefined,
): string {
  return workflow?.name ?? NO_WORKFLOW_LABEL;
}
