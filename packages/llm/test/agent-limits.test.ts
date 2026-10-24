import { afterEach, describe, expect, it, vi } from "vitest";
import {
  agentLimits,
  axLimitEnv,
  axRunLimits,
} from "../src/agent-limits";
import { agentTurnTimeoutMs, workflowTurnBudget } from "../src/agent-backend";

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("agentLimits", () => {
  it("keeps the chat defaults", () => {
    const limits = agentLimits("chat");
    expect(limits.timeoutMs).toBe(9 * 60_000);
    expect(limits.maxTurns).toBe(25);
    expect(limits.maxToolCalls).toBeUndefined();
    expect(limits.delegation).toEqual({ maxIterations: 50 });
    expect(limits.toolOutput).toEqual({ maxBytes: 50_000 });
    expect(limits.terminal.timeoutSeconds).toBe(180);
  });

  it("gives workflows a tool-call cap, continuations and medium effort", () => {
    const limits = agentLimits("workflow");
    expect(limits.timeoutMs).toBe(15 * 60_000);
    expect(limits.maxToolCalls).toBe(150);
    expect(limits.maxContinuations).toBe(2);
    expect(limits.reasoningEffort).toBe("medium");
  });

  it("reads overrides and clamps them to their ranges", () => {
    vi.stubEnv("OPENNEKO_AGENT_TURN_TIMEOUT_MS", "120000");
    vi.stubEnv("OPENNEKO_AGENT_MAX_TURNS", "9000");
    vi.stubEnv("OPENNEKO_AGENT_TOOL_OUTPUT_MAX_BYTES", "10");
    vi.stubEnv("OPENNEKO_AGENT_TERMINAL_TIMEOUT_SECONDS", "x");
    const limits = agentLimits("chat");
    expect(limits.timeoutMs).toBe(120_000);
    expect(limits.maxTurns).toBe(500);
    expect(limits.toolOutput.maxBytes).toBe(1_000);
    expect(limits.terminal.timeoutSeconds).toBe(180);
  });

  it("keeps the older helpers on the shared source", () => {
    vi.stubEnv("OPENNEKO_AGENT_TURN_TIMEOUT_MS", "300000");
    vi.stubEnv("OPENNEKO_WORKFLOW_MAX_TOOL_CALLS", "80");
    expect(agentTurnTimeoutMs()).toBe(300_000);
    expect(workflowTurnBudget()).toEqual({
      timeoutMs: 15 * 60_000,
      reasoningEffort: "medium",
      maxToolIterations: 80,
      maxContinuations: 2,
    });
  });
});

describe("backend mappings", () => {
  it("maps the limits onto the Ax run spec and env", () => {
    expect(axRunLimits(agentLimits("chat"))).toEqual({
      timeout_ms: 9 * 60_000,
      max_actor_steps: 25,
      max_child_steps: 50,
    });
    expect(axRunLimits(agentLimits("workflow"))).toEqual({
      timeout_ms: 15 * 60_000,
      max_actor_steps: 150,
      max_child_steps: 50,
      max_operations: 150,
      reasoning_effort: "medium",
    });
    expect(axLimitEnv(agentLimits("chat"))).toEqual({
      OPENNEKO_HARNESS_TERMINAL_TIMEOUT_SECONDS: "180",
      OPENNEKO_HARNESS_TERMINAL_MAX_OUTPUT: "50000",
    });
  });
});
