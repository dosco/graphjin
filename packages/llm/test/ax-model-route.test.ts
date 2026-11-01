import { describe, expect, it } from "vitest";
import { PRIMARY_PROVIDER_OPTIONS } from "../src/config";
import { resolveAxModelRoute, resolveProviderRuntime } from "../src/provider-runtime";

describe("resolveAxModelRoute", () => {
  it("uses native clients for OpenAI, Anthropic and Gemini", () => {
    expect(resolveAxModelRoute({ provider: "anthropic", model: "claude-sonnet-5-5" })).toEqual({
      provider: "anthropic",
      model: "claude-sonnet-5-5",
      keyEnv: "ANTHROPIC_API_KEY",
    });
    expect(resolveAxModelRoute({ provider: "google-gemini", model: "gemini-3.6-flash" }).provider).toBe("google-gemini");
    expect(resolveAxModelRoute({ provider: "openai", model: "gpt-5.5" }).provider).toBe("openai");
  });

  it("builds Azure from its resource and deployment", () => {
    expect(
      resolveAxModelRoute({ provider: "azure-openai", model: "ignored", config: { resourceName: "acme", deploymentName: "sales-gpt" } }),
    ).toEqual({
      provider: "azure-openai",
      model: "sales-gpt",
      keyEnv: "AZURE_FOUNDRY_API_KEY",
      options: { resource_name: "acme", deployment_name: "sales-gpt", api_version: "2024-10-21" },
    });
  });

  it("uses each provider's Ax profile on the provider's API host", () => {
    expect(resolveAxModelRoute({ provider: "groq", model: "llama-5" })).toEqual({
      provider: "groq",
      model: "llama-5",
      url: "https://api.groq.com/openai/v1",
      keyEnv: "GROQ_API_KEY",
    });
    expect(resolveAxModelRoute({ provider: "x-grok", model: "grok-5" }).provider).toBe("grok");
    expect(resolveAxModelRoute({ provider: "huggingface", model: "m" }).provider).toBe("huggingface-router");
    expect(resolveAxModelRoute({ provider: "ollama", model: "qwen", config: { url: "http://localhost:11434" } })).toEqual({
      provider: "openai-compatible",
      model: "qwen",
      url: "http://host.docker.internal:11434/v1",
    });
  });

  it("reaches the egress host the sandbox allows for every admin provider", () => {
    const config = { resourceName: "acme", deploymentName: "d", projectId: "p", region: "us-central1", url: "http://localhost:11434" };
    for (const { value } of PRIMARY_PROVIDER_OPTIONS) {
      let runtime: ReturnType<typeof resolveProviderRuntime>;
      try {
        runtime = resolveProviderRuntime({ provider: value, model: "m", config });
      } catch {
        continue;
      }
      const ax = resolveAxModelRoute({ provider: value, model: "m", config });
      const host = ax.url
        ? new URL(ax.url).hostname
        : ax.provider === "azure-openai"
          ? `${ax.options?.resource_name}.openai.azure.com`
          : new URL(runtime.baseUrl).hostname;
      expect(host, value).toBe(runtime.endpoint.host);
    }
  });
});
