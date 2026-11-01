import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { parse } from "yaml";
import { graphjinAgentModelFromRow, patchGraphjinAgentBlock } from "../src/graphjin/agent-settings";

const config = `mcp:
  include_tools_with_agent: true
agent:
  enabled: true
  provider: openai
  model: gpt-4.1-mini
  api_key_env: GRAPHJIN_AGENT_API_KEY
  max_steps: 8
  read_only: true
`;

describe("GraphJin agent settings", () => {
  it("maps admin providers onto GraphJin's Ax profiles", () => {
    expect(graphjinAgentModelFromRow({ provider: "anthropic", model: "claude-sonnet-5-5", config: {}, secrets: { apiKey: "k" } })).toEqual({
      provider: "anthropic",
      model: "claude-sonnet-5-5",
      apiKey: "k",
    });
    expect(graphjinAgentModelFromRow({ provider: "azure-openai", model: "x", config: { resourceName: "acme", deploymentName: "sales" }, secrets: { apiKey: "k" } })).toEqual({
      provider: "azure-openai",
      model: "sales",
      baseUrl: "https://acme.openai.azure.com/openai/deployments/sales",
      apiKey: "k",
    });
    expect(graphjinAgentModelFromRow({ provider: "groq", model: "llama", config: {}, secrets: { apiKey: "k" } }).baseUrl).toBe(
      "https://api.groq.com/openai/v1",
    );
  });

  it("writes the agent block with the key file and drops an old key variable", () => {
    const provider = patchGraphjinAgentBlock(config, { provider: "anthropic", model: "claude-sonnet-5-5" });
    expect(provider.changed).toBe(true);
    const agent = parse(provider.content).agent;
    expect(agent.api_key_env).toBeUndefined();
    expect(agent).toMatchObject({
      enabled: true,
      provider: "anthropic",
      model: "claude-sonnet-5-5",
      api_key_file: ".openneko-graphjin-agent-key",
      read_only: true,
      max_steps: 12,
      timeout_seconds: 170,
      reasoning: "low",
    });
    expect(agent.base_url).toBeUndefined();
    expect(parse(provider.content).mcp).toEqual({ include_tools_with_agent: true });

    const model = patchGraphjinAgentBlock(provider.content, { provider: "anthropic", model: "claude-haiku-5-5" });
    expect(model).toMatchObject({ changed: true });
    expect(patchGraphjinAgentBlock(model.content, { provider: "anthropic", model: "claude-haiku-5-5" }).changed).toBe(false);
  });
});

describe("data path default", () => {
  it("uses direct tools unless an admin opted into GraphJin's agent", async () => {
    const { graphjinAgentEnabled } = await import("@neko/db");
    expect(graphjinAgentEnabled(null)).toBe(false);
    expect(graphjinAgentEnabled({ enabled: true })).toBe(true);
    expect(graphjinAgentEnabled({ enabled: false })).toBe(false);
  });

  it("leaves GraphJin's agent alone when nothing is saved", async () => {
    vi.resetModules();
    vi.doMock("@neko/db", async (original) => ({
      ...(await original<typeof import("@neko/db")>()),
      getGraphjinAgentRow: async () => null,
    }));
    const { resolveGraphjinAgentModel } = await import("../src/graphjin/agent-settings");
    expect(await resolveGraphjinAgentModel("org-1")).toBeNull();
    vi.doUnmock("@neko/db");
  });

  it("gives an opted-in GraphJin agent the primary model", async () => {
    vi.resetModules();
    const primary = { provider: "anthropic", model: "claude-sonnet-5-5", config: {}, secrets: { apiKey: "sk-primary" }, enabled: true };
    vi.doMock("@neko/db", async (original) => ({
      ...(await original<typeof import("@neko/db")>()),
      getGraphjinAgentRow: async () => ({ enabled: true, provider: "primary", model: null, config: {}, secrets: {} }),
      db: () => ({ select: () => ({ from: () => ({ where: () => ({ limit: async () => [primary] }) }) }) }),
    }));
    const { resolveGraphjinAgentModel } = await import("../src/graphjin/agent-settings");
    expect(await resolveGraphjinAgentModel("org-1")).toEqual({ provider: "anthropic", model: "claude-sonnet-5-5", apiKey: "sk-primary" });
    vi.doUnmock("@neko/db");
  });

  it("leaves GraphJin's agent alone once an admin opts into direct tools", async () => {
    vi.resetModules();
    vi.doMock("@neko/db", async (original) => ({
      ...(await original<typeof import("@neko/db")>()),
      getGraphjinAgentRow: async () => ({ enabled: false, provider: "primary", model: null, config: {}, secrets: {} }),
    }));
    const { resolveGraphjinAgentModel } = await import("../src/graphjin/agent-settings");
    expect(await resolveGraphjinAgentModel("org-1")).toBeNull();
    vi.doUnmock("@neko/db");
  });
});

describe("provisionGraphjinAgent", () => {
  it("writes the key file and config, then applies the block live without a restart", async () => {
    vi.resetModules();
    vi.doMock("@neko/db", async (original) => ({
      ...(await original<typeof import("@neko/db")>()),
      getGraphjinAgentRow: async () => ({ enabled: true, provider: "anthropic", model: "claude-sonnet-5-5", config: {}, secrets: { apiKey: "sk-test" } }),
      db: () => ({ select: () => ({ from: () => ({ where: () => ({ orderBy: () => ({ limit: async () => [{ graphql_url: "http://openneko-serve:8080/api/v1/graphql" }] }) }) }) }) }),
    }));
    const { provisionGraphjinAgent } = await import("../src/graphjin/agent-settings");
    const dir = await mkdtemp(join(tmpdir(), "gj-agent-"));
    const file = join(dir, "agentic.yml");
    await writeFile(file, config);
    const apply = vi.fn(async () => "rev");

    await provisionGraphjinAgent("org-1", file, apply);
    expect(await readFile(join(dir, ".openneko-graphjin-agent-key"), "utf8")).toBe("sk-test");
    expect(parse(await readFile(file, "utf8")).agent.api_key_file).toBe(".openneko-graphjin-agent-key");
    expect(apply).toHaveBeenCalledWith("http://openneko-serve:8080/api/v1/graphql", {
      serv: { agent: expect.objectContaining({ enabled: true, provider: "anthropic", api_key_file: ".openneko-graphjin-agent-key" }) },
    });

    // The file already holds the block, but the running GraphJin may have
    // missed it, so the block is applied again.
    apply.mockClear();
    await provisionGraphjinAgent("org-1", file, apply);
    expect(apply).toHaveBeenCalledOnce();

    vi.doUnmock("@neko/db");
  });
});
