export const WORK_MEMORY_KINDS = [
  "preference",
  "business_rule",
  "metric_definition",
  "thread_note",
  "correction",
  "company_context",
  "other",
] as const;

export type WorkMemoryKind = (typeof WORK_MEMORY_KINDS)[number];

export const WORK_MEMORY_SCOPES = ["global", "thread", "database"] as const;
export type WorkMemoryScope = (typeof WORK_MEMORY_SCOPES)[number];

export const WORK_PENDING_MEMORY_STATUSES = [
  "proposed",
  "accepted",
  "declined",
] as const;
export type WorkPendingMemoryStatus =
  (typeof WORK_PENDING_MEMORY_STATUSES)[number];

export type WorkMemoryContext = {
  orgId: string;
  threadId?: string | null;
  runId?: string | null;
  /**
   * CV2 memory layer of the acting principal. null/absent = team layer.
   * When undefined, write paths fall back to the runId's K1 actor.
   */
  userId?: string | null;
  /** Held team_memory ids ("global", "database:<id>"); undefined means every team memory. */
  teamMemory?: "*" | ReadonlySet<string>;
};
