import type { A2UIMessage } from "@/a2ui/types";

export type AnswerVital = {
  label: string;
  value: string;
  sub?: string;
  basis?: "observed" | "calculated" | "estimated";
  asOf?: string;
  source?: string;
};

export type MessageRecord = {
  id: string;
  runId: string | null;
  role: "user" | "assistant";
  content: string;
  createdAt: string;
};

export type RunRecord = {
  id: string;
  backend: "hermes";
  status: string;
  error: string | null;
  createdAt: string;
  finishedAt: string | null;
  analysisMinutesSaved?: number | null;
  analysisMinutesBasis?: string | null;
  actorRole?: "admin" | "member" | "service" | null;
  routineName?: string | null;
};

export type WorkEvent =
  | { type: "hello"; runId: string; threadId: string; backend?: RunRecord["backend"] }
  // `content` is a delta — concatenating all message events for a run
  // reconstructs the full assistant text. Mirrors `AgentEvent.message` in
  // packages/llm/src/agent-backend.ts.
  | { type: "message"; role: "user" | "assistant"; content: string }
  | { type: "interim"; id: string; content: string; source: "hermes_interim_assistant" }
  | { type: "tool_start"; id: string; name: string; input?: unknown }
  | { type: "tool_delta"; id: string; delta: unknown }
  | { type: "tool_end"; id: string; result?: unknown; error?: string }
  | { type: "surface"; messages: A2UIMessage[] }
  | { type: "artifact"; artifact: { path: string; label: string; mimeType?: string } }
  | { type: "status"; message: string }
  | {
      type: "progress";
      id: string;
      content: string;
      source: "provider_summary";
      provider: "google-gemini" | "anthropic";
    }
  | { type: "error"; message: string }
  | {
      type: "capability_denied";
      capability: "network_egress";
      reason: "policy_denied";
      host: string;
      port?: number;
      method?: string;
      path?: string;
    }
  | {
      type: "action_request_emit";
      action_request_id: string;
      kind: string;
      scope: "internal" | "external";
      risk_level?: string;
      intent?: string;
      summary?: string;
      decision: "auto_approved" | "pending_approval";
    }
  | {
      type: "action_request_result";
      action_request_id: string;
      kind: string;
      status: "succeeded" | "failed" | "rejected" | "partially_applied" | "reconcile_required";
      outcome?: {
        result?: Record<string, unknown> | null;
        externalRef?: string | null;
        commandOrOperation?: string | null;
      };
      error?: string;
      rejection_reason?: string;
    }
  | {
      type: "needs_input";
      question: string;
      options?: string[];
      questions?: Array<{
        id: string;
        header?: string;
        question: string;
        options?: Array<{ label: string; description?: string }>;
      }>;
      reason?: string;
      surfaceId?: string;
    }
  | { type: "followups"; items: string[] }
  | { type: "vitals"; items: AnswerVital[] }
  | { type: "done"; result?: unknown };

export type ThreadBundle = {
  thread: {
    id: string;
    title: string;
    createdAt: string;
    updatedAt: string;
    lastMessageAt: string;
  };
  runs: RunRecord[];
  messages: MessageRecord[];
  eventsByRun: Record<string, WorkEvent[]>;
  hasOlder?: boolean;
};

export type StreamResult = "done" | "closed";

export type ActiveRunStream = {
  threadId: string;
  runId: string;
  close: () => void;
};

export type UploadedWorkFile = {
  name: string;
  size: number;
  relativePath: string;
  absolutePath: string;
};

export type PendingMemory = {
  id: string;
  draftText: string;
  draftKind: string;
  draftScope: string;
  confidence: number;
  reasoning: string | null;
  conflicts: Array<{ memoryId: string; text: string; similarity: number }>;
};

export type ToolItem = {
  id: string;
  name: string;
  input?: unknown;
  deltas: unknown[];
  end?: Extract<WorkEvent, { type: "tool_end" }>;
  approval?: ApprovalItem;
};

export type ApprovalItem = {
  actionRequestId: string;
  actionKind: string;
  intent: string | null;
  summary: string | null;
  decision: "auto_approved" | "pending_approval";
  result:
    | Extract<WorkEvent, { type: "action_request_result" }>
    | null;
};

export type TimelineItem =
  | { kind: "text"; content: string }
  | { kind: "interim"; id: string; content: string }
  | { kind: "progress"; id: string; content: string }
  | { kind: "tools"; tools: ToolItem[] }
  | { kind: "surface"; messages: A2UIMessage[] }
  | {
      kind: "capability";
      denial: Extract<WorkEvent, { type: "capability_denied" }>;
    }
  | {
      kind: "needs_input";
      request: Extract<WorkEvent, { type: "needs_input" }>;
    }
  | { kind: "error"; message: string }
  | { kind: "stopped" };

export type RunArtifact = Extract<WorkEvent, { type: "artifact" }>["artifact"];
