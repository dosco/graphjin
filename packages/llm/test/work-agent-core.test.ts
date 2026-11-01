import { describe, expect, it } from "vitest";
import type {
  AgentBackend,
  AgentRunOptions,
  AgentWorkspace,
} from "../src/agent-backend";
import type { AgentControlPlane } from "../src/work/control-plane";
import {
  GRAPHJIN_DIRECT_GOVERNED_POLICY,
  GRAPHJIN_TOOL_POLICY_ENV,
} from "../src/work/graphjin-tool-policy";
import { runAgentBackend } from "../src/work/agent-core";

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

// These tests only inspect the MCP server wiring; no tool handler is invoked.
const controlPlane = {} as AgentControlPlane;

describe("runAgentBackend", () => {
  it("mounts the brokered tool servers for MCP-capable chat agents", async () => {
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

    await runAgentBackend({
      backend,
      prompt: "prompt",
      userMessage: "find the camera loan",
      orgId: "org-1",
      threadId: "thread-1",
      runId: "run-1",
      workspace,
      controlPlane,
      pluginActions: [],
      emit: async () => {},
    });

    expect(captured?.mcpServers).toEqual(
      expect.objectContaining({
        neko_interaction: expect.anything(),
        neko_graphjin: expect.anything(),
      }),
    );
    expect(captured?.mcpBridgeEnv).toMatchObject({
      OPENNEKO_MCP_MODE: "work",
      OPENNEKO_MCP_ORG_ID: "org-1",
      OPENNEKO_MCP_RUN_ID: "run-1",
      OPENNEKO_MCP_WANTS_CARDS: "1",
    });
  });

  it("mounts pack actions separately from plugin actions", async () => {
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

    const packActions = [{
      kind: "magento.manage_catalog",
      description: "Change Magento catalog data.",
      scope: "external" as const,
      default_mode: "ask" as const,
    }];
    await runAgentBackend({
      backend,
      prompt: "prompt",
      userMessage: "change one Magento price",
      orgId: "org-1",
      threadId: "thread-1",
      runId: "run-1",
      workspace,
      controlPlane,
      pluginActions: [],
      packActions,
      emit: async () => {},
    });

    expect(captured?.mcpServers).toEqual(
      expect.objectContaining({ neko_pack_actions: expect.anything() }),
    );
    expect(captured?.mcpServers).not.toHaveProperty("neko_plugin_actions");
    expect(captured?.mcpBridgeEnv?.OPENNEKO_MCP_PACK_ACTIONS).toBe(
      JSON.stringify(packActions),
    );
    expect(captured?.mcpBridgeEnv?.OPENNEKO_MCP_PLUGIN_ACTIONS).toBe("[]");
  });

  it("applies and serializes the same per-run GraphJin policy", async () => {
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

    await runAgentBackend({
      backend,
      prompt: "prompt",
      userMessage: "answer directly",
      orgId: "org-1",
      threadId: "thread-1",
      runId: "run-1",
      workspace,
      controlPlane,
      pluginActions: [],
      graphjinToolPolicy: GRAPHJIN_DIRECT_GOVERNED_POLICY,
      emit: async () => {},
    });

    expect(captured?.mcpServers).toEqual(
      expect.objectContaining({ neko_graphjin: expect.anything() }),
    );
    expect(captured?.mcpBridgeEnv?.[GRAPHJIN_TOOL_POLICY_ENV]).toBe(
      "direct-governed",
    );
  });

  it.each(["ax"] as const)("mounts only GraphJin's agent on the agent path for %s", async (id) => {
    let captured: AgentRunOptions | undefined;
    const backend: AgentBackend = {
      id,
      capabilities: { mcpTools: true, sessionResume: false },
      async run(opts) {
        captured = opts;
        return { status: "completed", finalText: "done" };
      },
    };

    await runAgentBackend({
      backend,
      prompt: "prompt",
      userMessage: "How many orders?",
      orgId: "org-1",
      threadId: "thread-1",
      runId: "run-1",
      workspace,
      controlPlane,
      pluginActions: [],
      graphjinDataPath: "agent",
      emit: async () => {},
    });

    expect(captured?.mcpServers).toHaveProperty("neko_graphjin_agent");
    expect(captured?.mcpServers).not.toHaveProperty("neko_graphjin");
    expect(captured?.hostedGraphjin).toBeUndefined();

    // With GraphJin hosted in openneko serve, the turn asks it there.
    await runAgentBackend({
      backend,
      prompt: "prompt",
      userMessage: "How many orders?",
      orgId: "org-1",
      threadId: "thread-1",
      runId: "run-1",
      workspace,
      controlPlane,
      pluginActions: [],
      graphjinDataPath: "agent",
      hostedGraphjin: { token: "run-jwt" },
      emit: async () => {},
    });
    expect(captured?.mcpServers).not.toHaveProperty("neko_graphjin_agent");
    expect(captured?.mcpServers).not.toHaveProperty("neko_graphjin");
    expect(captured?.hostedGraphjin).toEqual({ token: "run-jwt", threadId: "thread-1" });
  });
});
