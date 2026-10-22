// Turns an executor error into a sentence an operator can act on. The raw
// error stays available as technical detail.
export function describeActionFailure(error: string | null | undefined, system = "the target system"): string {
  const text = error ?? "";
  if (/fetch failed|ECONNREFUSED|ENOTFOUND|EAI_AGAIN|connection refused|getaddrinfo|socket hang up/i.test(text)) {
    return `OpenNeko could not reach ${system}, so it changed nothing. Ask a follow-up to propose it again once ${system} is back.`;
  }
  if (/timeout|timed out|ETIMEDOUT/i.test(text)) {
    return `${system.charAt(0).toUpperCase()}${system.slice(1)} did not answer in time. OpenNeko changed nothing.`;
  }
  if (/\b(401|403)\b|unauthori[sz]ed|forbidden/i.test(text)) {
    return `${system.charAt(0).toUpperCase()}${system.slice(1)} refused OpenNeko's credentials. OpenNeko changed nothing.`;
  }
  if (/reconcil/i.test(text)) {
    return `OpenNeko sent the change but could not confirm the result in ${system}. Check the record before you retry.`;
  }
  return `The change did not run in ${system}. OpenNeko changed nothing.`;
}

export function systemForActionKind(kind: string): string {
  const prefix = kind.split(".")[0];
  if (prefix === "magento") return "Magento";
  return "the target system";
}

export type ActionOutcome =
  | "waiting"
  | "approved"
  | "completed"
  | "needs_check"
  | "partial"
  | "failed"
  | "rejected";

export const OUTCOME_LABEL: Record<ActionOutcome, string> = {
  waiting: "Waiting for you",
  approved: "Approved",
  completed: "Completed",
  needs_check: "Needs checking",
  partial: "Partly applied",
  failed: "Failed",
  rejected: "Rejected",
};

// The request status says whether a person decided. The executor result
// says what happened in the target system. A change-set that Magento
// accepted but OpenNeko could not confirm is not complete.
export function actionOutcome(status: string, executionResultStatus?: string | null): ActionOutcome {
  if (status === "pending_approval") return "waiting";
  if (status === "rejected") return "rejected";
  if (status === "failed") return "failed";
  if (executionResultStatus === "reconcile_required") return "needs_check";
  if (executionResultStatus === "partially_applied") return "partial";
  if (executionResultStatus === "failed") return "failed";
  if (status === "executed") return "completed";
  return "approved";
}

export function describeOutcome(
  outcome: ActionOutcome,
  error: string | null | undefined,
  system: string,
): string | null {
  if (outcome === "failed") return describeActionFailure(error, system);
  if (outcome === "needs_check") {
    return `${system.charAt(0).toUpperCase()}${system.slice(1)} accepted the change, but OpenNeko could not confirm the new values. Check the record in ${system} before you retry.`;
  }
  if (outcome === "partial") {
    return `Some rows changed in ${system} and some did not. Check the rows that failed before you retry.`;
  }
  return null;
}
