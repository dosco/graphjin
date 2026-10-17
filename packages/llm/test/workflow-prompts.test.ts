import { describe, expect, it } from "vitest";
import type { AgentWorkspace } from "../src/agent-backend";
import { buildWorkflowRunnerPrompt, buildWorkflowSeedMessage } from "../src/workflows/runner-prompt";
import type { WorkflowRecord } from "../src/workflows/store";
import { VALUE_ESTIMATE_INSTRUCTIONS } from "../src/prompts/sections";
import { splitStablePrompt } from "../src/prompts/stable-prefix";
import { extractValueFence } from "../src/workflows/fence-parsers";

const sampleWorkspace: AgentWorkspace = {
  orgRoot: "/tmp/org",
  skillsRoot: "/tmp/org/skills",
  memoryRoot: "/tmp/org/memory",
  knowledgeRoot: "/tmp/org/knowledge",
  uploadsRoot: "/tmp/org/uploads",
  runsRoot: "/tmp/org/runs",
  threadUploadsRoot: "/tmp/org/uploads/t1",
  runRoot: "/tmp/org/runs/r1",
  artifactRoot: "/tmp/org/runs/r1/artifacts",
  binRoot: "/tmp/org/runs/r1/bin",
};

const sampleWorkflow: WorkflowRecord = {
  id: "wf1",
  orgId: "org1",
  name: "APAC revenue dip check",
  description: "Daily APAC revenue check.",
  enabled: true,
  status: "active",
  goal: "Surface revenue dips.",
  systemPromptOverlay: "Show INR in lakhs.",
  steps: [{ id: "pull", description: "Pull last 7 days of revenue" }],
  cron: null,
  cronTimezone: "UTC",
  cronEnabled: false,
  dailyRunBudget: null,
  outputContract: null,
  createdByThreadId: null,
  createdByRunId: null,
  createdAt: new Date(),
  updatedAt: new Date(),
};

describe("buildWorkflowRunnerPrompt", () => {
  const sampleKnowledge = {
    tables: '{"tables":[{"name":"salesorderdetail"}]}',
    namespaces: '{"namespaces":["public"]}',
    insights: '{"hub_tables":["salesorderdetail"]}',
    syntax: '{"query":{"aggregations":{"functions":["sum_<column>"]},"examples":{"aggregations":[{"description":"sum(expr)","query":"{ orders { revenue: sum(expr: { mul: [price, qty] }) } }"}]}}}',
  };
  const base = {
    workflow: sampleWorkflow,
    mode: "headless" as const,
    memoryContext: "",
    backend: "hermes" as const,
    workspace: sampleWorkspace,
    knowledge: sampleKnowledge,
  };

  it("puts the run's artifact path and memories after the stable prefix", () => {
    const run = (n: number) =>
      buildWorkflowRunnerPrompt({
        ...base,
        mcpTools: true,
        memoryContext: `- memory ${n}`,
        workspace: { ...sampleWorkspace, runRoot: `/tmp/org/runs/r${n}`, artifactRoot: `/tmp/org/runs/r${n}/artifacts` },
      });
    const [first, second] = [run(1), run(2)];
    const stable = first.slice(0, first.indexOf("<files>"));
    expect(second.startsWith(stable)).toBe(true);
    expect(stable).toContain("<finishing>");
    expect(stable).toContain("<mode>headless</mode>");
    expect(first.indexOf("<long_term_memory>")).toBeGreaterThan(first.indexOf("<files>"));
    expect(splitStablePrompt(first).stable).toBe(stable.trimEnd());
    expect(splitStablePrompt(second).stable).toBe(stable.trimEnd());
    expect(splitStablePrompt(second).turn).toMatch(/^<files>\nSave every report or file you make under \/tmp\/org\/runs\/r2\/artifacts/);
  });

  it("uses MCP tool names when mcpTools=true", () => {
    const prompt = buildWorkflowRunnerPrompt({ ...base, mcpTools: true });
    expect(prompt).toContain("mcp_neko_workflow_output_emit");
    expect(prompt).toContain("mcp_neko_action_request");
    expect(prompt).not.toContain("```neko_workflow_output");
    expect(prompt).not.toContain("```neko_action_request");
  });

  it.each(["headless", "live"] as const)("keeps time saved in %s production workflows but omits it for evals", (mode) => {
    const prompt = buildWorkflowRunnerPrompt({ ...base, mode, mcpTools: true });
    expect(prompt).toContain("```neko_value");
    expect(prompt).toContain(VALUE_ESTIMATE_INSTRUCTIONS);
    expect(extractValueFence(VALUE_ESTIMATE_INSTRUCTIONS)).toMatchObject({
      errors: [],
      payload: { minutes_saved: 90 },
    });
    expect(prompt).not.toMatch(/neko_(vitals|followups)/);
    expect(prompt).toContain("mcp_neko_workflow_output_emit");
    const evaluation = buildWorkflowRunnerPrompt({ ...base, mode, mcpTools: true, includeUxMetadata: false });
    expect(evaluation).not.toMatch(/neko_(value|vitals|followups)/);
    expect(evaluation).toContain("mcp_neko_workflow_output_emit");
  });

  it("fails closed when the native GraphJin broker tool is unavailable", () => {
    expect(() =>
      buildWorkflowRunnerPrompt({ ...base, mcpTools: false }),
    ).toThrow(/native GraphJin broker tool/);
  });

  it("surfaces installed plugin action kinds to the native tool path", () => {
      const prompt = buildWorkflowRunnerPrompt({
        ...base,
        mcpTools: true,
        pluginActions: [
          {
            kind: "send_slack_dm",
            description: "Send a DM to a Slack user.",
            example: { user: "amit", text: "Stock is low." },
          },
          {
            kind: "lookup_slack_entity",
            description: "Look up a Slack user.",
            default_mode: "deny",
          },
        ],
      });
      expect(prompt).toContain("send_slack_dm");
      // the example payload is surfaced verbatim so the agent copies the shape
      expect(prompt).toContain('example payload: {"user":"amit","text":"Stock is low."}');
      // denied-everywhere kinds are not advertised
      expect(prompt).not.toContain("lookup_slack_entity");
  });

  it("surfaces an internal record action's scope to the native action tool", () => {
    const prompt = buildWorkflowRunnerPrompt({
      ...base,
      mcpTools: true,
      pluginActions: [
        {
          kind: "record_create",
          scope: "internal",
          description: "Create a generated-app record.",
          default_mode: "ask",
        },
      ],
    });

    expect(prompt).toContain("`record_create` (scope: internal)");
    expect(prompt).toContain("Always use the exact scope listed");
  });

  it("includes the workflow's overlay and steps", () => {
    const prompt = buildWorkflowRunnerPrompt({ ...base, mcpTools: true });
    expect(prompt).toContain("Show INR in lakhs.");
    expect(prompt).toContain("Pull last 7 days of revenue");
  });

  it("uses native brokered GraphQL for data access", () => {
    const prompt = buildWorkflowRunnerPrompt({ ...base, mcpTools: true });
    expect(prompt).toContain("<data_access>");
    expect(prompt).toContain("mcp_neko_graphjin_execute_graphql");
    expect(prompt).toContain("mcp_neko_graphjin_query_catalog");
    expect(prompt).not.toContain("graphjin cli");
  });

  it("inlines syntax.json so cat-truncation can't hide the aggregation patterns", () => {
    const prompt = buildWorkflowRunnerPrompt({ ...base, mcpTools: true });
    expect(prompt).toContain(sampleKnowledge.syntax);
  });

  it("exposes search + save MCP tools in the long_term_memory block when mcpTools=true", () => {
    const prompt = buildWorkflowRunnerPrompt({ ...base, mcpTools: true });
    expect(prompt).toContain("<long_term_memory>");
    expect(prompt).toContain("mcp_neko_memory_search");
    expect(prompt).toContain("mcp_neko_memory_save");
  });

  it("uses native dynamic delegation guidance for Hermes workflow runs", () => {
    const prompt = buildWorkflowRunnerPrompt({
      ...base,
      backend: "hermes",
      mcpTools: true,
    });
    expect(prompt).toContain("delegate_task");
    expect(prompt).toContain("OpenNeko does not provide named subagent profiles");
    expect(prompt).toContain("toolsets");
  });

});

describe("buildWorkflowSeedMessage", () => {
  it("hands the trigger event to the agent", () => {
    const message = buildWorkflowSeedMessage({
      workflow: { name: "quote shortfall" },
      triggerKind: "subscription",
      triggerPayload: { table: "x_quote", primary_key: { xq_id: 602218 }, snapshot: { xq_ref_id: "Q1554738-14" } },
    });
    expect(message).toContain("<trigger_input>");
    expect(message).toContain('"xq_ref_id": "Q1554738-14"');
  });

  it("keeps a run with no payload to the plain start line", () => {
    const message = buildWorkflowSeedMessage({ workflow: { name: "daily" }, triggerKind: "cron", triggerPayload: {} });
    expect(message).toMatch(/^\[scheduled run started at .+\] Begin executing the "daily" workflow\.$/);
  });

  it("prefers the operator's message", () => {
    expect(buildWorkflowSeedMessage({ workflow: { name: "w" }, triggerKind: "manual", userMessage: "run for Q1", triggerPayload: { a: 1 } })).toBe("run for Q1");
  });
});
