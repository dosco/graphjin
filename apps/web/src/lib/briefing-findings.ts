import type { FindingCardData } from "@/components/FindingCard";

export type BriefingApprovalRow = {
  id: string;
  workflowRunId: string | null;
  kind: string;
  target: string | null;
  summary: string | null;
  riskLevel: string | null;
  createdAt: Date;
  workflowId: string | null;
  workflowName: string | null;
};

export function serializeBriefingApproval(
  approval: BriefingApprovalRow,
): FindingCardData {
  return {
    id: approval.id,
    kind: "approval",
    workflowRunId: approval.workflowRunId,
    workflow: approval.workflowId
      ? {
          id: approval.workflowId,
          name: approval.workflowName ?? "workflow",
        }
      : null,
    title: approval.summary || approval.kind,
    target: approval.target,
    riskLevel: approval.riskLevel,
    createdAt: approval.createdAt.toISOString(),
  };
}
