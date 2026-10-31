import { describe, expect, it } from "vitest";
import { buildMetricPrompt } from "../src/metric-prompt";
import type { AgentWorkspace } from "../src/agent-backend";
import type { KnowledgePackContents } from "../src/knowledge-pack";

const fakeWorkspace: AgentWorkspace = {
  orgRoot: "/tmp/wsp/org",
  skillsRoot: "/tmp/wsp/skills",
  memoryRoot: "/tmp/wsp/memory",
  knowledgeRoot: "/tmp/wsp/knowledge",
  uploadsRoot: "/tmp/wsp/uploads",
  runsRoot: "/tmp/wsp/runs",
  threadUploadsRoot: "/tmp/wsp/thread-uploads",
  runRoot: "/tmp/wsp/run",
  artifactRoot: "/tmp/wsp/artifacts",
  binRoot: "/tmp/wsp/bin",
};

const fakeKnowledge: KnowledgePackContents = {
  tables: '{"tables":["sales","products"]}',
  namespaces: '{"ns":["public"]}',
  insights: '{"hubs":["sales"]}',
  syntax: '{"operators":["sum","count","ratio"]}',
};

const fakeInput = {
  question: "Which product sold the most units last quarter?",
  title: "Top Product",
  why: "CEO wants top revenue product",
  role: "CEO",
  slug: "top-product",
  chartHint: "bar" as const,
};

const directDataAccess = {
  queryTool: "mcp_neko_graphjin_execute_graphql",
} as const;

describe("buildMetricPrompt", () => {
  it("returns a single string composed from required sections", () => {
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      ...directDataAccess,
    });
    expect(typeof prompt).toBe("string");
    // Sentinel anchors from each section.
    expect(prompt).toContain("<role>");
    expect(prompt).toContain("<long_term_memory>");
    expect(prompt).toContain("<data_access>");
    expect(prompt).toContain("<time_window>");
    expect(prompt).toContain("<mood_and_chart>");
    expect(prompt).toContain("<hard_constraints>");
    expect(prompt).toContain("<output_contract>");
    expect(prompt).toContain("<input>");
  });

  it("inlines the full knowledge pack (the metric agent is one-shot)", () => {
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      ...directDataAccess,
    });
    expect(prompt).toContain(fakeKnowledge.tables);
    expect(prompt).toContain(fakeKnowledge.namespaces);
    expect(prompt).toContain(fakeKnowledge.insights);
    expect(prompt).toContain(fakeKnowledge.syntax);
  });

  it("does not expose a shell path for direct GraphJin access", () => {
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      ...directDataAccess,
    });
    expect(prompt).not.toContain("`Bash`");
    expect(prompt).not.toContain("`terminal`");
  });

  it("uses the query-only broker surface for isolated jobs", () => {
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      ...directDataAccess,
    });
    expect(prompt).toContain("`mcp_neko_graphjin_execute_graphql`");
    expect(prompt).toContain("service credential never enter this run");
    expect(prompt).not.toContain("graphjin cli execute_graphql");
  });

  it("asks GraphJin's agent and carries no query know-how on the agent path", () => {
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      dataAgentTool: "mcp_neko_graphjin_agent_ask",
    });
    expect(prompt).toContain("`mcp_neko_graphjin_agent_ask`");
    expect(prompt).toContain("Write each instruction as a precise data request.");
    expect(prompt).not.toContain(fakeKnowledge.syntax);
    expect(prompt).not.toMatch(/hidden|not available in this treatment/i);
    expect(prompt).not.toContain("mcp_neko_graphjin_execute_graphql");
    expect(prompt).not.toContain("graphjin cli execute_graphql");
  });

  it("rejects ambiguous direct and delegated data surfaces", () => {
    const ambiguous = {
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      queryTool: "mcp_neko_graphjin_execute_graphql",
      dataAgentTool: "mcp_neko_graphjin_agent_ask",
    } as unknown as Parameters<typeof buildMetricPrompt>[0];
    expect(() =>
      buildMetricPrompt(ambiguous),
    ).toThrow("choose either queryTool or agentTool");
  });

  it("declares the JSON output contract with every required field name", () => {
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      ...directDataAccess,
    });
    for (const key of [
      "reasoning",
      "headlineMetric",
      "headlineLabel",
      "insightText",
      "detailText",
      "mood",
      "chartType",
      "chartData",
      "timeWindow",
      "grain",
      "start",
      "end",
      "label",
    ]) {
      expect(prompt).toContain(`"${key}"`);
    }
  });

  it("inlines the card input as JSON the model can read deterministically", () => {
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      ...directDataAccess,
    });
    expect(prompt).toContain('"cardTitle": "Top Product"');
    expect(prompt).toContain(
      '"userQuestion": "Which product sold the most units last quarter?"',
    );
    expect(prompt).toContain('"cardRationale": "CEO wants top revenue product"');
    expect(prompt).toContain('"cardRole": "CEO"');
    expect(prompt).toContain('"cardSlug": "top-product"');
    expect(prompt).toContain('"chartHint": "bar"');
  });

  it("forwards memoryContext into the long_term_memory block", () => {
    const ctx = "- [id-1] business_rule (global): Always cite tables";
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      ...directDataAccess,
      memoryContext: ctx,
    });
    expect(prompt).toContain(ctx);
  });

  it("never exposes the save tool (one-shot agent — operator persists memories)", () => {
    const withSearch = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      supportsMemorySearch: true,
      ...directDataAccess,
    });
    const withoutSearch = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      supportsMemorySearch: false,
      ...directDataAccess,
    });
    expect(withSearch).not.toContain("mcp_neko_memory_save");
    expect(withoutSearch).not.toContain("mcp_neko_memory_save");
  });

  it("exposes mcp_neko_memory_search when supportsMemorySearch is true", () => {
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      supportsMemorySearch: true,
      ...directDataAccess,
    });
    expect(prompt).toContain("mcp_neko_memory_search");
  });

  it("omits the search instruction when supportsMemorySearch is false", () => {
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      supportsMemorySearch: false,
      ...directDataAccess,
    });
    expect(prompt).not.toContain("mcp_neko_memory_search");
  });

  it("includes the anti-fanout + date-filter rules (regression: must keep them across refactors)", () => {
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      ...directDataAccess,
    });
    expect(prompt).toContain("flattened");
    expect(prompt).toContain("distinct: [parent_id]");
    expect(prompt).toContain("multiple operators under");
  });

  it("includes the live-max(date) anchor rule under hard_constraints (TTM correctness)", () => {
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      ...directDataAccess,
    });
    expect(prompt).toContain("max(<date_col>)");
  });

  it("includes time-window grain rules (TTM, snapshot, etc.)", () => {
    const prompt = buildMetricPrompt({
      input: fakeInput,
      knowledge: fakeKnowledge,
      workspace: fakeWorkspace,
      ...directDataAccess,
    });
    expect(prompt).toContain("TTM");
    expect(prompt).toContain("snapshot");
    expect(prompt).toContain("trailing twelve months");
  });
});
