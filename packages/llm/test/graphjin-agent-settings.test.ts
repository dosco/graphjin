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

describe("provisionGraphjinAgent", () => {
  it("writes the key file and config, then applies the block live without a restart", async () => {
    vi.resetModules();
    const restart = vi.fn();
    vi.doMock("@neko/db", async (original) => ({
      ...(await original<typeof import("@neko/db")>()),
      getGraphjinAgentRow: async () => ({ enabled: true, provider: "anthropic", model: "claude-sonnet-5-5", config: {}, secrets: { apiKey: "sk-test" } }),
      db: () => ({ select: () => ({ from: () => ({ where: () => ({ orderBy: () => ({ limit: async () => [{ graphql_url: "http://graphjin:8080/api/v1/graphql" }] }) }) }) }) }),
    }));
    vi.doMock("../src/graphjin/restart", () => ({ requestGraphjinRestart: restart }));
    const { provisionGraphjinAgent } = await import("../src/graphjin/agent-settings");
    const dir = await mkdtemp(join(tmpdir(), "gj-agent-"));
    const file = join(dir, "agentic.yml");
    await writeFile(file, config);
    const apply = vi.fn(async () => "rev");

    await provisionGraphjinAgent("org-1", file, apply);
    expect(await readFile(join(dir, ".openneko-graphjin-agent-key"), "utf8")).toBe("sk-test");
    expect(parse(await readFile(file, "utf8")).agent.api_key_file).toBe(".openneko-graphjin-agent-key");
    expect(apply).toHaveBeenCalledWith("http://graphjin:8080/api/v1/graphql", {
      serv: { agent: expect.objectContaining({ enabled: true, provider: "anthropic", api_key_file: ".openneko-graphjin-agent-key" }) },
    });
    expect(restart).not.toHaveBeenCalled();

    // Unchanged settings apply nothing.
    apply.mockClear();
    await provisionGraphjinAgent("org-1", file, apply);
    expect(apply).not.toHaveBeenCalled();

    // A GraphJin that cannot apply the block live is restarted.
    await writeFile(file, config);
    await provisionGraphjinAgent("org-1", file, async () => {
      throw new Error("GraphJin config preview failed: serv.agent.enabled is not writable; writable agent fields: model");
    });
    expect(restart).toHaveBeenCalledWith(file, "http://graphjin:8080/api/v1/graphql");
    vi.doUnmock("@neko/db");
    vi.doUnmock("../src/graphjin/restart");
  });
});
