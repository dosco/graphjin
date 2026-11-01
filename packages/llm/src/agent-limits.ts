/**
 * The one source for agent run limits and budgets. Ax reads its run spec and
 * process environment from here. Each value has an environment override.
 */

export type AgentReasoningEffort = "low" | "medium" | "high";

export type AgentRunKind = "chat" | "workflow" | "agent-job";

export type AgentLimits = {
  /** Wall clock for one turn. The backend process is killed when it expires. */
  timeoutMs: number;
  /** Model turns per run: Ax max_actor_steps. */
  maxTurns: number;
  /** Tool-calling iterations per run. Undefined means only maxTurns bounds them. */
  maxToolCalls?: number;
  /** Extra turns a workflow may take after a turn times out. */
  maxContinuations: number;
  /** Undefined keeps the provider default. */
  reasoningEffort?: AgentReasoningEffort;
  delegation: {
    /** Steps each delegated child may take. */
    maxIterations: number;
  };
};

// Every concurrent job runs its own runner. Keep the
// out-of-box ceiling small; operators raise it after sizing their host.
export const AGENT_DEFAULT_GLOBAL_CAP = 3;
// Two metric refreshes at a time leave room for chat on a small host.
export const METRIC_REFRESH_DEFAULT_CAP = 2;

function readInt(name: string, fallback: number, min: number, max?: number): number {
  const raw = process.env[name]?.trim();
  if (!raw) return fallback;
  const n = Math.floor(Number(raw));
  if (!Number.isFinite(n)) return fallback;
  if (n < min) return min;
  if (max !== undefined && n > max) return max;
  return n;
}

export function isReasoningEffort(value: unknown): value is AgentReasoningEffort {
  return value === "low" || value === "medium" || value === "high";
}

/**
 * Discovery-heavy asks measured 280-450 s against a live source, so a chat turn
 * gets 9 minutes. A shorter limit killed real runs mid-stream.
 */
export function agentLimits(kind: AgentRunKind = "chat"): AgentLimits {
  const shared = {
    maxTurns: readInt("OPENNEKO_AGENT_MAX_TURNS", 25, 1, 500),
    delegation: {
      maxIterations: readInt("OPENNEKO_AGENT_DELEGATION_MAX_ITERATIONS", 50, 1),
    },
  };
  if (kind === "workflow") {
    // Workflows are mechanical loops: lower effort, a tool-call cap and a longer wall clock.
    const effort = process.env.OPENNEKO_WORKFLOW_REASONING_EFFORT?.trim();
    return {
      ...shared,
      timeoutMs: readInt("OPENNEKO_WORKFLOW_TURN_TIMEOUT_MS", 15 * 60_000, 1_000),
      maxToolCalls: readInt("OPENNEKO_WORKFLOW_MAX_TOOL_CALLS", 150, 1),
      maxContinuations: readInt("OPENNEKO_WORKFLOW_MAX_CONTINUATIONS", 2, 0),
      reasoningEffort: isReasoningEffort(effort) ? effort : "medium",
    };
  }
  return {
    ...shared,
    timeoutMs: readInt("OPENNEKO_AGENT_TURN_TIMEOUT_MS", 9 * 60_000, 1_000),
    maxContinuations: 0,
  };
}

export type AxRunLimits = {
  timeout_ms: number;
  max_actor_steps: number;
  max_child_steps: number;
  max_operations?: number;
  reasoning_effort?: AgentReasoningEffort;
};

/**
 * Ax run spec fields. A per-run tool-call cap replaces the turn count.
 */
export function axRunLimits(limits: AgentLimits): AxRunLimits {
  return {
    timeout_ms: limits.timeoutMs,
    max_actor_steps: Math.min(limits.maxToolCalls ?? limits.maxTurns, 500),
    max_child_steps: Math.min(limits.delegation.maxIterations, 500),
    ...(limits.maxToolCalls ? { max_operations: limits.maxToolCalls } : {}),
    ...(limits.reasoningEffort ? { reasoning_effort: limits.reasoningEffort } : {}),
  };
}
