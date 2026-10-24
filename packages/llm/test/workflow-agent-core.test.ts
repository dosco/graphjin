import { describe, expect, it } from "vitest";
import type { AgentBackend, AgentRunOptions, AgentWorkspace } from "../src/agent-backend";
import type { AgentControlPlane } from "../src/work/control-plane";
import { runWorkflowAgentBackend } from "../src/workflows/agent-core";

const workspace: AgentWorkspace = {
  orgRoot: "/tmp/org",
  skillsRoot: "/tmp/org/skills",
  memoryRoot: "/tmp/org/memory",
  knowledgeRoot: "/tmp/org/knowledge",
  uploadsRoot: "/tmp/org/uploads",
  runsRoot: "/tmp/org/runs",
  threadUploadsRoot: "/tmp/org/uploads/thread-1",
  runRoot: "/tmp/org/runs/run-1",
  artifactRoot: "/tmp/org/runs/run-1/artifacts",
  binRoot: "/tmp/org/runs/run-1/bin",
};

// This test only inspects the MCP server wiring; no tool handler is invoked.
const controlPlane = {} as AgentControlPlane;

describe("runWorkflowAgentBackend", () => {
  it("threads workflow MCP bridge env needed by the in-box bridge", async () => {
    let captured: AgentRunOptions | undefined;
    const backend: AgentBackend = {
      id: "ax",
      capabilities: {
        mcpTools: true,
        sessionResume: false,
        nativeDelegation: "ax-worker",
      },
      async run(opts) {
        captured = opts;
        return { status: "completed", finalText: "done" };
      },
    };

    await runWorkflowAgentBackend({
      backend,
      prompt: "prompt",
      userMessage: "begin",
      orgId: "org-1",
      threadId: "thread-1",
      runId: "run-1",
      workflowRunId: "workflow-run-1",
      mode: "headless",
      triggeredByObservationId: "obs-1",
      workspace,
      controlPlane,
      emit: async () => {},
      timeoutMs: 1_800_000,
    });

    expect(captured?.mcpBridgeEnv).toMatchObject({
      OPENNEKO_MCP_MODE: "workflow",
      OPENNEKO_MCP_ORG_ID: "org-1",
      OPENNEKO_MCP_THREAD_ID: "thread-1",
      OPENNEKO_MCP_RUN_ID: "run-1",
      OPENNEKO_MCP_SKILLS_ROOT: "/tmp/org/skills",
      OPENNEKO_MCP_WORKFLOW_RUN_ID: "workflow-run-1",
      OPENNEKO_MCP_TRIGGERED_BY_OBSERVATION_ID: "obs-1",
    });
    expect(captured?.timeoutMs).toBe(1_800_000);
    expect(captured?.mcpServers).toEqual(
      expect.objectContaining({
        neko_action: expect.anything(),
        neko_graphjin: expect.anything(),
        neko_memory: expect.anything(),
        neko_records: expect.anything(),
        neko_workflow_output: expect.anything(),
      }),
    );
  });
});

describe("runWorkflowAgentBackend continuations", () => {
  const timeoutError = "ax turn exceeded its 900s budget and was terminated";

  function scriptedBackend(outcomes: Array<"timeout" | "done">) {
    const prompts: string[] = [];
    const options: AgentRunOptions[] = [];
    const backend: AgentBackend = {
      id: "ax",
      capabilities: {
        mcpTools: true,
        sessionResume: false,
        nativeDelegation: "ax-worker",
      },
      async run(opts) {
        prompts.push(opts.prompt);
        options.push(opts);
        const outcome = outcomes[prompts.length - 1];
        await opts.onEvent?.({
          type: "tool_start",
          id: `t${prompts.length}`,
          name: "terminal",
          input: { command: `python3 fetch.py --part ${prompts.length}` },
        });
        if (outcome === "timeout") {
          await opts.onEvent?.({ type: "error", message: timeoutError });
          return { status: "failed", finalText: "", error: timeoutError, timedOut: true };
        }
        return { status: "completed", finalText: "done" };
      },
    };
    return { backend, prompts, options };
  }

  const base = {
    prompt: "prompt",
    userMessage: "begin",
    orgId: "org-1",
    threadId: "thread-1",
    runId: "run-1",
    workflowRunId: "workflow-run-1",
    mode: "headless" as const,
    networkHosts: [],
    workspace,
    controlPlane,
  };

  it("continues a timed-out turn and hides the recovered timeout", async () => {
    const { backend, prompts, options } = scriptedBackend(["timeout", "done"]);
    const events: string[] = [];
    const result = await runWorkflowAgentBackend({
      ...base,
      backend,
      emit: async (event) => {
        events.push(event.type === "error" ? `error:${event.message}` : event.type);
      },
      maxContinuations: 2,
      reasoningEffort: "medium",
      maxToolIterations: 150,
    });

    expect(result.status).toBe("completed");
    expect(prompts).toHaveLength(2);
    expect(prompts[1]).toContain('<continuation number="1">');
    expect(prompts[1]).toContain("python3 fetch.py --part 1");
    expect(events.some((e) => e.startsWith("error:"))).toBe(false);
    expect(options[0]).toMatchObject({ reasoningEffort: "medium", maxToolIterations: 150 });
  });

  it("fails with the timeout once continuations run out", async () => {
    const { backend, prompts } = scriptedBackend(["timeout", "timeout", "timeout"]);
    const errors: string[] = [];
    const result = await runWorkflowAgentBackend({
      ...base,
      backend,
      emit: async (event) => {
        if (event.type === "error") errors.push(event.message);
      },
      maxContinuations: 2,
    });

    expect(prompts).toHaveLength(3);
    expect(result.status).toBe("failed");
    expect(result.error).toContain("2 continuation(s) also ran out of time");
    expect(errors).toEqual([timeoutError]);
  });

  it("does not continue when continuations are off", async () => {
    const { backend, prompts } = scriptedBackend(["timeout", "done"]);
    const result = await runWorkflowAgentBackend({
      ...base,
      backend,
      emit: async () => {},
    });
    expect(prompts).toHaveLength(1);
    expect(result.timedOut).toBe(true);
  });
});
