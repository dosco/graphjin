import { describe, expect, it } from "vitest";
import { normalizeAxUsage } from "../src/agent-backends/ax";
import { nekoProviderFromHermes } from "../src/provider-runtime";
import { estimateTokenCostUsd } from "../src/spend/model-prices";

describe("provider names", () => {
  it("maps Hermes provider names to the names Ax and OpenNeko use", () => {
    expect(nekoProviderFromHermes("gemini")).toBe("google-gemini");
    expect(nekoProviderFromHermes("openai-api")).toBe("openai");
    expect(nekoProviderFromHermes("custom", { provider: "groq", model: "llama" })).toBe("groq");
    expect(nekoProviderFromHermes("custom")).toBe("custom");
  });
});

describe("Ax usage pricing", () => {
  const usage = {
    input_tokens: 66_972, output_tokens: 1_823, cache_read_tokens: 78_519,
    reasoning_tokens: 5_557, total_tokens: 152_871, coverage: "complete" as const,
  };

  it("prices Gemini 3.8 Flash from the published snapshot, reasoning billed as output", () => {
    const cost = estimateTokenCostUsd("google-gemini", "gemini-3.8-flash", {
      inputTokens: 66_972, outputTokens: 1_823, cacheReadTokens: 78_519, totalTokens: 152_871,
    }, new Date("2026-10-12T00:00:00Z"))!;
    // 66,972 × $0.75 + 78,519 × $0.075 + (1,823 + 5,557) × $3.75, per million.
    expect(cost).toBeCloseTo(0.0838, 4);
    expect(estimateTokenCostUsd("google-gemini", "gemini-3.8-flash", { inputTokens: 1_000_000 }, new Date("2027-02-01T00:00:00Z"))).toBeCloseTo(1.5, 6);
  });

  it("marks an Ax run estimated for a priced route and unknown otherwise", () => {
    const priced = normalizeAxUsage(usage, { provider: "google-gemini", model: "gemini-3.8-flash" });
    expect(priced.costStatus).toBe("estimated");
    expect(priced.estimatedCostUsd).toBeGreaterThan(0);
    expect(normalizeAxUsage(usage, { provider: "mistral", model: "mistral-large" }).costStatus).toBe("unknown");
    const partial = normalizeAxUsage({ ...usage, coverage: "partial", reported: 4, requests: 5 }, { provider: "google-gemini", model: "gemini-3.8-flash" });
    expect(partial.estimatedCostUsd).toBeCloseTo(priced.estimatedCostUsd! * 1.25, 8);
    expect(normalizeAxUsage({ ...usage, coverage: "unavailable" }, { provider: "google-gemini", model: "gemini-3.8-flash" }).costStatus).toBe("unknown");
  });
});
