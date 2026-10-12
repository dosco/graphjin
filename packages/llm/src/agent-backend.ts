import type { HarnessRunSummary, NormalizedUsage } from "@neko/telemetry";
import {
  METRIC_REFRESH_DEFAULT_CAP,
  agentLimits,
  type AgentReasoningEffort,
} from "./agent-limits";

export const AGENT_BACKEND_IDS = ["hermes", "ax"] as const;
export type AgentBackendId = (typeof AGENT_BACKEND_IDS)[number];

/** The backend for an org that has not chosen one, when its primary provider supports it. */
export const DEFAULT_AGENT_BACKEND: AgentBackendId = "ax";

export { AGENT_DEFAULT_GLOBAL_CAP, METRIC_REFRESH_DEFAULT_CAP } from "./agent-limits";

/** Concurrent metric refreshes: OPENNEKO_METRIC_REFRESH_CONCURRENCY, else the default, never above globalCap. */
export function metricRefreshConcurrency(globalCap: number, configured?: string): number {
  const value = Number(configured);
  const cap = configured?.trim() && Number.isInteger(value) && value >= 1 ? value : METRIC_REFRESH_DEFAULT_CAP;
  return Math.max(1, Math.min(cap, globalCap));
}

export const AGENT_BACKEND_OPTIONS = [
  {
    value: "ax",
    label: "Ax (default)",
    description: "OpenNeko's Go agent. Calls tools from code, so one model step can run many tools.",
  },
  {
    value: "hermes",
    label: "Hermes",
    description: "Subprocess agent. Works with any LLM provider.",
  },
] as const;

export function isAgentBackendId(value: string): value is AgentBackendId {
  return (AGENT_BACKEND_IDS as readonly string[]).includes(value);
}

export function shellToolName(backendId: AgentBackendId): string {
  void backendId;
  return "terminal";
}

export type AgentSurfaceMessage = {
  version: "v0.9" | "v1.0";
  [key: string]: unknown;
};

export type AgentArtifact = {
  path: string;
  label: string;
  mimeType?: string;
};

export type AgentInputQuestion = {
  /** Stable within one needs_input event; used to bind submitted answers. */
  id: string;
  /** Short UI label such as "Destination" or "Size mix". */
  header?: string;
  question: string;
  options?: Array<{
    label: string;
    description?: string;
  }>;
};

export type OutputMood = "good" | "watch" | "act";

export type AgentVital = {
  label: string;
  value: string;
  sub?: string;
  /** How the figure was obtained. Missing on historical events. */
  basis?: "observed" | "calculated" | "estimated";
  /** Human-readable freshness, for example "as of 23 Jul 2026". */
  asOf?: string;
  /** Short source name, never a fabricated citation. */
  source?: string;
};

/** Provider-reported usage normalized once at the backend boundary. */
export type AgentTokenUsage = NormalizedUsage;

/**
 * Provider/model selected by the trusted OpenNeko runtime configuration.
 * This is intentionally separate from the provider/model carried by a usage
 * event: that event describes what the backend process reported for the live
 * session, so callers can compare configured and observed identity.
 */
export type AgentModelIdentity = {
  provider: string;
  model: string;
};

export type AgentModelIdentityAttestation = {
  /** Selected by trusted OpenNeko configuration before process launch. */
  configured?: AgentModelIdentity;
  /** Reported by the live backend session, independently of configuration. */
  observed?: AgentModelIdentity;
};

export type AgentEvent =
  // Delta of real prose since the last message event. Backends MUST NOT emit
  // structured-output payloads (a2ui fences, tool-call JSON, etc.) here — use
  // the `surface` event for cards.
  | { type: "message"; role: "user" | "assistant"; content: string }
  /** Hermes ACP's provider-neutral, model-authored mid-turn commentary. */
  | { type: "interim"; id: string; content: string; source: "hermes_interim_assistant" }
  | {
      type: "tool_start";
      id: string;
      name: string;
      input?: unknown;
      /** Cumulative provider usage for the current turn, not an additive delta. */
      usageSnapshot?: AgentTokenUsage;
    }
  | { type: "tool_delta"; id: string; delta: unknown }
  | { type: "tool_end"; id: string; result?: unknown; error?: string }
  | { type: "surface"; messages: AgentSurfaceMessage[] }
  | { type: "artifact"; artifact: AgentArtifact }
  | { type: "status"; message: string }
  | {
      /**
       * A provider-authored, user-visible summary of the current model step.
       * This is never raw hidden reasoning and never comes from a second
       * inference. Backends must emit it only when the provider explicitly
       * labels the stream as a thought summary.
       */
      type: "progress";
      id: string;
      content: string;
      source: "provider_summary";
      provider: "google-gemini" | "anthropic";
    }
  | {
      type: "usage";
      source: "outer" | "inner";
      provider?: string;
      model?: string;
      modelIdentity?: AgentModelIdentityAttestation;
      usage: AgentTokenUsage;
    }
  /** Content-free operational summary persisted for clients and operators. */
  | { type: "telemetry"; summary: HarnessRunSummary }
  /** Content-free: the backend retried or rerouted a model step. */
  | { type: "retry"; reason: string }
  | { type: "error"; message: string }
  | {
      /**
       * A sandbox capability was refused by policy. This is host-derived from
       * the tool result, not prose authored by the model, so every channel can
       * offer an honest recovery path without scraping the answer text.
       */
      type: "capability_denied";
      capability: "network_egress";
      reason: "policy_denied";
      host: string;
      port?: number;
      method?: string;
      path?: string;
    }
  | { type: "done"; result?: unknown }
  | { type: "output_emit"; output_id: string; kind: string }
  | {
      type: "action_request_emit";
      action_request_id: string;
      kind: string;
      scope: "internal" | "external";
      risk_level?: string;
      /**
       * Agent's natural-language framing — populated when the request
       * is awaiting approval so the chat UI can render the headline
       * on the inline card. Omitted for auto-approved requests where
       * no human ever sees it.
       */
      intent?: string;
      /** Pre-policy summary string suitable as a fallback when intent is absent. */
      summary?: string;
      /**
       * "auto_approved" → queued; "pending_approval" → needs the user
       * to click Approve before the worker fires the adapter.
       */
      decision: "auto_approved" | "pending_approval";
    }
  | {
      /**
       * Terminal status of an action_request that was either
       * auto-approved or user-approved. Surfaced inline in /work so
       * the user (and agent, on the next turn) sees what happened.
       */
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
      /** Operator-supplied reason when the user rejected the request. */
      rejection_reason?: string;
    }
  | {
      type: "needs_input";
      /** Modality-free summary retained for older and thin channels. */
      question: string;
      options?: string[];
      /** Structured questions used by rich channels. */
      questions?: AgentInputQuestion[];
      /** Why the run cannot safely continue without these answers. */
      reason?: string;
      /** Present when the tool also emitted a deterministic A2UI form. */
      surfaceId?: string;
    }
  // Suggested follow-up questions — channel-agnostic content emitted once at
  // the end of a /work run via a `neko_followups` fence. Any channel (the Ask
  // rail, Telegram, Slack) can surface these as "ask next" prompts.
  | { type: "followups"; items: string[] }
  // The headline numbers that carry the answer — channel-agnostic content
  // emitted once at the end of a /work run via a `neko_vitals` fence. Each
  // channel renders them its own way (the web rail as a tile grid, a chat
  // channel as a one-line recap, a voice channel by reading them aloud).
  | { type: "vitals"; items: AgentVital[] }
  /**
   * Host-derived: the run loaded a skill. The model does not author this.
   * firstEventId is the work_run_event.id of the tool_start that used it.
   */
  | {
      type: "skill_used";
      name: string;
      source: "hermes" | "read";
      contentHash: string;
      origin: "builtin" | "custom" | "pack";
      packId?: string;
      packVersion?: string;
      configCommitSha?: string | null;
      firstEventId: number;
    };

export type AgentChatMessage = {
  id?: string;
  role: "user" | "assistant";
  content: string;
  runId?: string | null;
  createdAt?: string;
};

export type AgentWorkspace = {
  orgRoot: string;
  skillsRoot: string;
  memoryRoot: string;
  knowledgeRoot: string;
  uploadsRoot: string;
  runsRoot: string;
  threadUploadsRoot: string;
  runRoot: string;
  artifactRoot: string;
  binRoot: string;
};

/** Wall clock for one chat turn, from the shared agent limits. */
export function agentTurnTimeoutMs(): number {
  return agentLimits("chat").timeoutMs;
}

/** Budget for one workflow turn, from the shared agent limits. */
export function workflowTurnBudget(): Required<
  Pick<AgentRunOptions, "timeoutMs" | "reasoningEffort" | "maxToolIterations">
> & {
  maxContinuations: number;
} {
  const limits = agentLimits("workflow");
  return {
    timeoutMs: limits.timeoutMs,
    reasoningEffort: limits.reasoningEffort ?? "medium",
    maxToolIterations: limits.maxToolCalls ?? limits.maxTurns,
    maxContinuations: limits.maxContinuations,
  };
}

export type { AgentReasoningEffort } from "./agent-limits";

/** Per-run policy for a backend's own sub-agent primitive. */
export type AgentNativeDelegationPolicy = "enabled" | "disabled";

export type AgentRunOptions = {
  prompt: string;
  userMessage?: string;
  timeoutMs?: number;
  retries?: number;
  debug?: boolean;
  tag?: string;
  orgId?: string;
  workspace?: AgentWorkspace;
  skills?: string[];
  signal?: AbortSignal;
  onEvent?: (event: AgentEvent) => Promise<void> | void;
  backendState?: Record<string, unknown>;
  mcpServers?: Record<string, unknown>;
  /** Per-run context for backends that mount MCP servers as stdio child
   *  processes (hermes/ACP) instead of in-process SDK instances. The bridge
   *  entry (OPENNEKO_MCP_BRIDGE) rebuilds each named server from this env;
   *  broker coords are copied into the clean bridge-child env only. */
  mcpBridgeEnv?: Record<string, string>;
  /**
   * Whether the backend may expose its own sub-agent primitive for this run.
   * Omitted means the production default (`enabled`). Backends without native
   * delegation ignore this setting.
   */
  nativeDelegation?: AgentNativeDelegationPolicy;
  /** Web turn ⇒ the agent renders a2ui cards. Backends that can't take the
   *  in-process SDK card server (hermes) wire their own render tool when set.
   *  See docs/PER_CHANNEL_RENDERING.md. */
  wantsCards?: boolean;
  /** Overrides the configured reasoning effort for this run. */
  reasoningEffort?: AgentReasoningEffort;
  /** Caps tool-calling iterations; the agent then stops and summarizes. */
  maxToolIterations?: number;
  /** Workflow hosts the run may reach. Backends with a web tool allow only these. */
  networkHosts?: string[];
};

export type AgentRunResult = {
  finalText: string;
  /**
   * Unprocessed agent output, for out-of-band fence parsing by the runtime
   * (action/workflow/rule/memory fences). finalText is the cleaned display
   * text — Hermes strips its hidden builder fences from it AND from the
   * message stream, so neither carries the fence bodies. rawText preserves
   * them. Omit when finalText already carries any fences (the runtime falls
   * back to finalText).
   */
  rawText?: string;
  status: "completed" | "failed" | "cancelled";
  backendState?: Record<string, unknown>;
  error?: string;
  /** The turn hit its wall-clock budget; work in the run directory survives. */
  timedOut?: boolean;
  /** The backend's own failure code, e.g. "timeout" or "model_http_429". */
  errorCode?: string;
  /** Completed with a fallback answer; errorCode names the trigger. */
  degraded?: boolean;
};

export type AgentNativeDelegation = "hermes-delegate-task" | "ax-worker";

// Per-backend feature flags so shared runtime code (runChatTurn, prompt
// builder, auto-memory dispatch) never branches on backend.id. Adding a new
// backend (Codex etc.) requires only declaring its capabilities; no edits to
// shared call sites.
export interface AgentBackendCapabilities {
  /** Accepts in-process SDK MCP servers via run().mcpServers. */
  readonly mcpTools: boolean;
  /** Honors resume: sessionId in AgentRunOptions to reload prior turns out-of-band. */
  readonly sessionResume: boolean;
  /** Native backend subagent/delegation primitive, when available. */
  readonly nativeDelegation?: AgentNativeDelegation;
}

export interface AgentBackend {
  readonly id: AgentBackendId;
  readonly capabilities: AgentBackendCapabilities;
  /** Trusted runtime configuration, before the backend process is launched. */
  readonly configuredIdentity?: AgentModelIdentity;
  /** Compatibility accessor for configuredIdentity.model. */
  readonly model?: string;
  run(opts: AgentRunOptions): Promise<AgentRunResult>;
}
