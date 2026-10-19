import { afterEach, describe, expect, it, vi } from "vitest";
import {
  buildProfilerPrompt,
  profilerAgentRunControls,
  profilerTimeoutMs,
  validateBusinessProfile,
} from "../src/profiler";

const VALID_PROFILE = `# AdventureWorks Cycles — Business Profile

## What they are
AdventureWorks Cycles designs and sells bicycles and accessories.

## Who they serve
Specialty retailers and direct consumers.

## Where they operate
North America, Europe, and Australia.

## Scale
- Date range covered, recent volume + value
- Top revenue drivers
- People served and people who do the work

## Operational footprint
Manufacturing, sales, fulfillment, and finance.

## What a downstream LLM should hold in mind
- Bikes and accessories are the core offer.
- Wholesale and DTC both matter.
- Europe is a growth priority.`;

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("profilerTimeoutMs", () => {
  it("defaults to a bounded onboarding timeout", () => {
    vi.stubEnv("OPENNEKO_PROFILER_TIMEOUT_MS", "");
    vi.stubEnv("OPENNEKO_AGENT_TURN_TIMEOUT_MS", "");
    expect(profilerTimeoutMs()).toBe(9 * 60_000);
  });

  it("allows an explicit override", () => {
    vi.stubEnv("OPENNEKO_PROFILER_TIMEOUT_MS", "45000");
    expect(profilerTimeoutMs()).toBe(45_000);
  });

  it("disables hidden backend retries for onboarding profiler runs", () => {
    vi.stubEnv("OPENNEKO_PROFILER_TIMEOUT_MS", "45000");
    expect(profilerAgentRunControls()).toEqual({
      retries: 0,
      timeoutMs: 45_000,
    });
  });
});

describe("validateBusinessProfile", () => {
  it("accepts a profile that matches the required markdown contract", () => {
    expect(validateBusinessProfile(VALID_PROFILE, "AdventureWorks Cycles")).toBe(
      VALID_PROFILE,
    );
  });

  it("rejects agent failure/apology text", () => {
    const failure = `# AdventureWorks Cycles — Business Profile

## What they are
I am sorry, but I was unable to connect to the GraphJin server.

## Who they serve
Not measured.

## Where they operate
Not measured.

## Scale
Not measured.

## Operational footprint
Not measured.

## What a downstream LLM should hold in mind
Not measured.`;

    expect(() =>
      validateBusinessProfile(failure, "AdventureWorks Cycles"),
    ).toThrow(/failure text/);
  });

  it("accepts markdown without enforcing a heading or section template", () => {
    const profile = `# AdventureWorks at a glance

AdventureWorks makes and sells bicycles.

- It serves retailers and consumers.
- It operates across several regions.`;

    expect(validateBusinessProfile(profile, "AdventureWorks Cycles")).toBe(
      profile,
    );
  });

  it("unwraps an outer markdown fence", () => {
    const fenced = `\`\`\`markdown\n${VALID_PROFILE}\n\`\`\``;

    expect(validateBusinessProfile(fenced, "AdventureWorks Cycles")).toBe(
      VALID_PROFILE,
    );
  });

  it("rejects an empty response", () => {
    expect(() =>
      validateBusinessProfile("  \n", "AdventureWorks Cycles"),
    ).toThrow(/empty business profile/);
  });
});

describe("buildProfilerPrompt knowledge inlining", () => {
  const bigTables = JSON.stringify({
    tables: Array.from({ length: 400 }, (_, i) => ({
      name: `table_${i}`,
      schema: "public",
      summary: `summary for table ${i} `.repeat(4),
    })),
  });
  const bigInsights = JSON.stringify({
    hub_tables: Array.from({ length: 10 }, (_, i) => ({
      name: `hub_${i}`,
      summary: "x".repeat(300),
      examples: [`{ hub_${i}(limit: 10) { id } }`],
      join_paths: Array.from(
        { length: 8 },
        (_, j) => `public.hub_${i}.col_${j} -> public.other_${j}.id`,
      ),
    })),
    help_cards: Array.from({ length: 30 }, (_, i) => ({
      id: `help:topic${i}`,
      summary: "y".repeat(200),
    })),
  });

  it("agentic mode inlines capped digests, never the raw pack files", () => {
    const prompt = buildProfilerPrompt({
      orgName: "Acme",
      companyNote: "",
      shellTool: "terminal",
      queryTool: "mcp_neko_graphjin_execute_graphql",
      knowledge: {
        mode: "agentic",
        tables: bigTables,
        namespaces: "{}",
        insights: bigInsights,
        syntax: '{"essentials":[],"patterns":[]}',
      },
    });
    expect(prompt).not.toContain(bigTables);
    expect(prompt).not.toContain(bigInsights);
    expect(prompt).toContain("join: public.hub_0.col_0 -> public.other_0.id");
    expect(prompt).toContain("- help:topic0");
    expect(prompt).toContain("`mcp_neko_graphjin_query_catalog`");
    expect(prompt).toContain("`mcp_neko_graphjin_execute_saved_query`");
    expect(prompt).not.toContain("graphjin cli");
    expect(prompt).not.toContain("graphjin cli health");
    expect(prompt).not.toContain("graphjin cli find_path");
    expect(prompt).not.toContain("graphjin cli explore_relationships");
    // Raw agentic packs run 50KB+; the whole prompt must stay well under
    // the size that hung the agent's first stream.
    expect(prompt.length).toBeLessThan(25_000);
  });

  it("legacy mode keeps the full prefetched pack inline", () => {
    const prompt = buildProfilerPrompt({
      orgName: "Acme",
      companyNote: "",
      shellTool: "terminal",
      queryTool: "mcp_neko_graphjin_execute_graphql",
      knowledge: {
        mode: "legacy",
        tables: bigTables,
        namespaces: "{}",
        insights: '{"hub_tables":[]}',
        syntax: "{}",
      },
    });
    expect(prompt).toContain(bigTables);
  });

  it("uses only the brokered GraphJin query tool for isolated runs", () => {
    const prompt = buildProfilerPrompt({
      orgName: "Acme",
      companyNote: "",
      shellTool: "terminal",
      queryTool: "mcp_neko_graphjin_execute_graphql",
      knowledge: {
        mode: "agentic",
        tables: bigTables,
        namespaces: "{}",
        insights: bigInsights,
        syntax: "{}",
      },
    });
    expect(prompt).toContain("`mcp_neko_graphjin_execute_graphql`");
    expect(prompt).toContain("`mcp_neko_graphjin_query_catalog`");
    expect(prompt).toContain("Treat the requested output sections as an evidence checklist");
    expect(prompt).toContain("trusted host supplies their exact schemas");
    expect(prompt).not.toContain("graphjin cli setup");
    expect(prompt).not.toContain("graphjin cli execute_graphql");
  });
});
