// Agentic knowledge layering: the slim bootstrap pack is built from
// gj_catalog (role-aware), marks itself agentic, and flips the prompt's
// data-access section to on-demand discovery.

import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  knowledgePackPaths,
  prefetchAgenticKnowledgePack,
  readKnowledgePack,
} from "../src/knowledge-pack";
import { buildDataAccessSection } from "../src/prompts/sections";
import type { AgentWorkspace } from "../src/agent-backend";

let dir: string;

beforeAll(async () => {
  dir = await mkdtemp(join(tmpdir(), "agentic-knowledge-"));
});

afterAll(async () => {
  await rm(dir, { recursive: true, force: true });
});

const catalogFixture = (query: string) => {
  if (query.includes('kind: { eq: "table" }')) {
    return [
      { id: "table:demo:sales.orders", name: "orders", summary: "12 columns; pk orderid" },
    ];
  }
  if (query.includes('kind: { eq: "database" }')) {
    return [{ id: "database:demo", name: "demo", summary: "postgres (default)" }];
  }
  if (query.includes('kind: { eq: "api_operation" }')) {
    return [{ id: "api_operation:importyeti:us_import_suppliers", name: "us_import_suppliers", summary: "Search US shipment suppliers" }];
  }
  if (query.includes('kind: { eq: "help" }')) {
    return [
      { id: "help:discovery", name: "Discovery help", summary: "Start here." },
      { id: "help:filters", name: "Filter help", summary: "Typed where operators." },
    ];
  }
  if (query.includes('id: "help:')) {
    return [
      {
        id: "help:query",
        name: "Query help",
        summary: "Query shape.",
        details_json: "{}",
        examples_json: "[]",
      },
    ];
  }
  return [];
};

const fakeFetch = (async (_url: unknown, init: { body?: unknown }) => {
  const { query } = JSON.parse(String(init.body)) as { query: string };
  return new Response(JSON.stringify({ data: { gj_catalog: catalogFixture(query) } }), {
    status: 200,
  });
}) as typeof fetch;

describe("prefetchAgenticKnowledgePack", () => {
  it("builds the slim pack from gj_catalog and marks the mode", async () => {
    const dest = join(dir, "pack");
    const result = await prefetchAgenticKnowledgePack({
      graphqlUrl: "http://gj.test/api/v1/graphql",
      token: "tok",
      destDir: dest,
      fetchImpl: fakeFetch,
    });
    expect(result.ok).toBe(true);
    expect(result.files.map((f) => f.file).sort()).toEqual([
      "insights.json",
      "namespaces.json",
      "syntax.json",
      "tables.json",
    ]);

    const pack = await readKnowledgePack(knowledgePackPaths(dest));
    expect(pack.mode).toBe("agentic");
    expect(pack.tables).toContain("table:demo:sales.orders");
    expect(pack.insights).toContain("help:discovery");
    expect(pack.insights).toContain("us_import_suppliers");
    expect(pack.syntax).toContain("help:query");

    const index = await readFile(join(dest, "INDEX.md"), "utf8");
    expect(index).toContain("gj_catalog");
    expect(index).toContain("agentic mode");
  });

  it("a legacy pack (no mode file) still reads as legacy", async () => {
    const pack = await readKnowledgePack(knowledgePackPaths(join(dir, "missing")));
    expect(pack.mode).toBe("legacy");
  });

  it("shows catalog API operations when a source has no tables", () => {
    const section = buildDataAccessSection({
      queryTool: "mcp_neko_graphjin_execute_graphql",
      workspace: { orgRoot: "/w", knowledgeRoot: "/w/knowledge" } as AgentWorkspace,
      knowledge: {
        mode: "agentic",
        tables: '{"tables":[]}',
        namespaces: '{"databases":[]}',
        insights: '{"api_operations":[{"name":"us_import_suppliers","summary":"Search US shipment suppliers"}]}',
        syntax: "{}",
      },
    });
    expect(section).toContain("us_import_suppliers — Search US shipment suppliers");
    expect(section).toContain("mcp_neko_graphjin_query_catalog");
  });
});

describe("buildDataAccessSection layering", () => {
  const workspace = {
    orgRoot: "/w",
    knowledgeRoot: "/w/knowledge",
  } as AgentWorkspace;

  const base = {
    queryTool: "mcp_neko_graphjin_execute_graphql",
    queryIdentity: "service" as const,
    workspace,
    knowledge: {
      tables: "{}",
      namespaces: "{}",
      insights: "{}",
      syntax: "{}",
    },
  };

  it("agentic mode teaches native catalog-first discovery", () => {
    const section = buildDataAccessSection({
      ...base,
      knowledge: { ...base.knowledge, mode: "agentic" },
      inlineKnowledge: "syntax",
    });
    expect(section).toContain("gj_catalog");
    expect(section).toContain("mcp_neko_graphjin_query_catalog");
    expect(section).toContain("query_catalog detail rows");
    expect(section).toContain('gj_catalog(id: "help:<topic>")');
    expect(section).not.toContain("complete prefetched schema");
  });

  it("legacy mode uses the prefetched pack without discovery", () => {
    const section = buildDataAccessSection({
      ...base,
      knowledge: { ...base.knowledge, mode: "legacy" },
      inlineKnowledge: "syntax",
    });
    expect(section).toContain("complete prefetched schema");
    expect(section).not.toContain("gj_catalog");
  });
});
