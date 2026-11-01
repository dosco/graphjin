import { describe, expect, it } from "vitest";
import {
  GRAPHJIN_AGGREGATE_RULE,
  GRAPHJIN_DATE_RULE,
  GRAPHJIN_FANOUT_RULE,
  buildDataAccessSection,
  buildMemorySection,
} from "../src/prompts/sections";
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
  tables: '{"tables":["t1","t2"]}',
  namespaces: '{"ns":["public"]}',
  insights: '{"hubs":["users"]}',
  syntax: '{"operators":["eq","gt"]}',
};

describe("buildMemorySection", () => {
  it("renders the operator-saved-context section even when no memories are loaded", () => {
    const section = buildMemorySection({
      searchTool: false,
      saveMode: "fence",
      memoryContext: undefined,
    });
    expect(section).toContain("<long_term_memory>");
    expect(section).toContain("No memories are currently saved");
    expect(section).toContain("</long_term_memory>");
  });

  it("inlines the loaded memory context when provided", () => {
    const ctx = "- [id-1] business_rule (global): Always cite tables";
    const section = buildMemorySection({
      searchTool: false,
      saveMode: "fence",
      memoryContext: ctx,
    });
    expect(section).toContain(ctx);
  });

  it("describes the MCP tool path when search + save tools are wired", () => {
    const section = buildMemorySection({
      searchTool: true,
      saveMode: "tool",
      memoryContext: "loaded memories here",
    });
    expect(section).toContain("mcp_neko_memory_save");
    expect(section).toContain("mcp_neko_memory_search");
    expect(section).not.toContain("```neko_memory");
  });

  it("describes the neko_memory fence path when saveMode is fence", () => {
    const section = buildMemorySection({
      searchTool: false,
      saveMode: "fence",
      memoryContext: "loaded memories here",
    });
    expect(section).toContain("```neko_memory");
    expect(section).toContain('"save"');
    expect(section).not.toContain("mcp_neko_memory_save");
  });

  it("emits search-only instruction when saveMode='none' but searchTool=true", () => {
    const section = buildMemorySection({
      searchTool: true,
      saveMode: "none",
      memoryContext: "loaded memories here",
    });
    expect(section).toContain("mcp_neko_memory_search");
    expect(section).not.toContain("mcp_neko_memory_save");
    expect(section).not.toContain("```neko_memory");
  });

  it("emits no write/search usage when both are off", () => {
    const section = buildMemorySection({
      searchTool: false,
      saveMode: "none",
      memoryContext: "loaded memories here",
    });
    expect(section).not.toContain("mcp_neko_memory_search");
    expect(section).not.toContain("mcp_neko_memory_save");
    expect(section).not.toContain("```neko_memory");
  });

  it("includes precedence + cite-back framing in every variant", () => {
    const variants: Array<Parameters<typeof buildMemorySection>[0]> = [
      { searchTool: true, saveMode: "tool", memoryContext: "anything" },
      { searchTool: false, saveMode: "fence", memoryContext: "anything" },
      { searchTool: true, saveMode: "none", memoryContext: "anything" },
      { searchTool: false, saveMode: "none", memoryContext: "anything" },
    ];
    for (const opts of variants) {
      const section = buildMemorySection(opts);
      expect(section).toMatch(/take precedence/i);
      expect(section).toMatch(/cite/i);
    }
  });
});

describe("buildDataAccessSection", () => {
  const native = (overrides: Partial<Parameters<typeof buildDataAccessSection>[0]> = {}) =>
    buildDataAccessSection({
      queryTool: "mcp_neko_graphjin_execute_graphql",
      queryIdentity: "actor",
      workspace: fakeWorkspace,
      knowledge: fakeKnowledge,
      inlineKnowledge: "syntax",
      ...overrides,
    });

  it("requires a native broker tool instead of falling back to the shell", () => {
    expect(() =>
      buildDataAccessSection({
        workspace: fakeWorkspace,
        knowledge: fakeKnowledge,
        inlineKnowledge: "syntax",
      }),
    ).toThrow(/native broker tool/);
  });

  it("uses the complete native GraphJin MCP surface without shell access", () => {
    const section = native();
    expect(section).toContain("mcp_neko_graphjin_execute_graphql");
    expect(section).toContain("mcp_neko_graphjin_query_catalog");
    expect(section).toContain("mcp_neko_graphjin_graphql_help");
    expect(section).toContain("with `for` set to");
    expect(section).toContain("mcp_neko_graphjin_validate_where_clause");
    expect(section).toContain("mcp_neko_graphjin_execute_saved_query");
    expect(section).toMatch(/short-lived\s+actor credential/);
    expect(section).not.toContain("graphjin cli");
    expect(section).not.toContain("`terminal`");
  });

  it("treats a refusal as final and names no script path", () => {
    const section = native();
    expect(section).toContain("Treat a refusal or unavailable tool as authoritative.");
    expect(section).not.toContain("neko_data");
    expect(section).not.toContain("neko-query");
  });

  it("inlines correctness rules for native queries", () => {
    const section = native();
    expect(section).toContain("multiple operators under");
    expect(section).toContain("flattened");
    expect(section).toContain("distinct: [parent_id]");
    expect(section).toContain("Make the database do the math");
  });

  it("inlines the prefetched pack for legacy sources", () => {
    const section = native();
    expect(section).toContain(fakeKnowledge.syntax);
    expect(section).toContain(fakeKnowledge.tables);
    expect(section).toContain(fakeKnowledge.namespaces);
    expect(section).toContain(fakeKnowledge.insights);
  });

  it("uses compact catalog-first guidance for agentic sources", () => {
    const section = native({
      knowledge: { ...fakeKnowledge, mode: "agentic" },
    });
    expect(section).toContain("gj_catalog");
    expect(section).toContain("query_catalog detail rows");
    expect(section).toContain("native tool names and schemas");
    expect(section).not.toContain("graphjin cli");
  });
});

describe("constants stay shaped right (consumed verbatim by other prompts)", () => {
  it("GRAPHJIN_DATE_RULE starts as a bullet so callers can splice consistently", () => {
    expect(GRAPHJIN_DATE_RULE.startsWith("- ")).toBe(true);
    expect(GRAPHJIN_DATE_RULE).toContain("and: [");
  });

  it("GRAPHJIN_DATE_RULE carries the house rule for relative periods", () => {
    expect(GRAPHJIN_DATE_RULE).toContain("A year or 12 months is 365 days");
    expect(GRAPHJIN_DATE_RULE).toContain("Never count back to the same calendar date");
  });

  it("GRAPHJIN_FANOUT_RULE starts as a bullet and names the three remediations", () => {
    expect(GRAPHJIN_FANOUT_RULE.startsWith("- ")).toBe(true);
    expect(GRAPHJIN_FANOUT_RULE).toContain("(a)");
    expect(GRAPHJIN_FANOUT_RULE).toContain("(b)");
    expect(GRAPHJIN_FANOUT_RULE).toContain("(c)");
  });

  it("GRAPHJIN_AGGREGATE_RULE starts as a bullet and pushes server-side aggregation", () => {
    expect(GRAPHJIN_AGGREGATE_RULE.startsWith("- ")).toBe(true);
    expect(GRAPHJIN_AGGREGATE_RULE).toContain("aggregate");
    expect(GRAPHJIN_AGGREGATE_RULE).toContain("limit");
  });

});
