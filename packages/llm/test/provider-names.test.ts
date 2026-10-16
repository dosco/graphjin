import { describe, expect, it } from "vitest";
import { normalizeAxUsage } from "../src/agent-backends/ax";
import { nekoProviderFromHermes } from "../src/provider-runtime";
import catalog from "../src/spend/ax-model-catalog.json";
import { estimateTokenCost, estimateTokenCostUsd, MODEL_PRICES_VERSION } from "../src/spend/model-prices";

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
  it("prefers the cost the harness reports for the route it used", () => {
    const harness = { ...usage, cost_micros: 41_250, priced_by: "catalog" as const, catalog_version: "v0.0.0-test" };
    expect(normalizeAxUsage(harness, { provider: "google-gemini", model: "gemini-3.5-flash-lite" })).toMatchObject({
      costStatus: "estimated", estimatedCostUsd: 0.04125, costSource: "ax_harness_catalog", pricingCatalogVersion: "ax-go@v0.0.0-test",
    });
    expect(normalizeAxUsage({ ...harness, coverage: "partial", reported: 4, requests: 5 }, { provider: "openai", model: "gpt-6-luna" }).estimatedCostUsd)
      .toBeCloseTo(0.04125 * 1.25, 10);
    // A host override for the configured route wins over the harness catalog cost.
    expect(normalizeAxUsage(harness, { provider: "google-gemini", model: "gemini-3.8-flash" })).toMatchObject({ costSource: "price_override" });
    // An unpriced harness report falls back to the host estimate.
    expect(normalizeAxUsage({ ...harness, priced_by: "unpriced" }, { provider: "anthropic", model: "claude-opus-5-5" })).toMatchObject({
      costSource: "ax_model_catalog", pricingCatalogVersion: MODEL_PRICES_VERSION,
    });
  });
});

describe("Ax model catalog snapshot", () => {
  it("prices current Gemini and Claude models", () => {
    // $0.30 input, $0.03 cache read, $2.50 output per million.
    expect(estimateTokenCost("google-gemini", "gemini-3.5-flash-lite", { inputTokens: 1_000_000, cacheReadTokens: 1_000_000, outputTokens: 1_000_000 }))
      .toMatchObject({ usd: expect.closeTo(2.83, 10), costSource: "ax_model_catalog" });
    expect(estimateTokenCostUsd("anthropic", "claude-opus-5-5", { inputTokens: 1_000_000, cacheWriteTokens: 1_000_000, cacheReadTokens: 1_000_000, outputTokens: 1_000_000 }))
      .toBeCloseTo(4 + 5 + 0.2 + 20, 10);
    expect(estimateTokenCostUsd("GoogleGeminiAI", "gemini-3.6-flash", { outputTokens: 1_000_000 })).toBeCloseTo(7.5, 10);
    expect(estimateTokenCostUsd("openai-compatible", "gpt-5.6", { inputTokens: 100_000 })).toBeCloseTo(0.4, 10);
  });

  it("applies the long-context tier per call", () => {
    const usage = { inputTokens: 150_000, cacheReadTokens: 60_000, outputTokens: 10_000 };
    expect(estimateTokenCostUsd("google-gemini", "gemini-2.5-pro", usage)).toBeCloseTo((150_000 * 2.5 + 60_000 * 0.25 + 10_000 * 15) / 1e6, 10);
    expect(estimateTokenCostUsd("google-gemini", "gemini-2.5-pro", { ...usage, calls: 2 })).toBeCloseTo((150_000 * 1.25 + 60_000 * 0.125 + 10_000 * 10) / 1e6, 10);
  });

  it("lets an override win and leaves unknown models unpriced", () => {
    expect(estimateTokenCost("google-gemini", "gemini-3.8-flash", { inputTokens: 1_000_000 }, new Date("2026-10-12T00:00:00Z")))
      .toMatchObject({ usd: expect.closeTo(0.75, 10), costSource: "price_override" });
    expect(catalog.providers["google-gemini"]["gemini-3.8-flash"].promptTokenCostPer1M).toBe(1.5);
    expect(estimateTokenCostUsd("google-gemini", "no-such-model", { inputTokens: 1_000 })).toBeUndefined();
    expect(estimateTokenCostUsd("groq", "llama-3.3-70b-versatile", { inputTokens: 1_000 })).toBeUndefined();
    expect(estimateTokenCostUsd("anthropic", "toString", { inputTokens: 1_000 })).toBeUndefined();
  });

  it("carries the pinned module version", () => {
    expect(catalog.version).toMatch(/^v\d+\.\d+\.\d+/);
    expect(MODEL_PRICES_VERSION).toBe(`ax-go@${catalog.version}+overrides-2026-10`);
  });
});
