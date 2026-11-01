import { describe, expect, it } from "vitest";
import {
  PRIMARY_PROVIDER_OPTIONS,
  RESEARCH_PROVIDER_OPTIONS,
  getDefaultPrimaryModel,
  getDefaultResearchModel,
  getPrimaryProviderFields,
  getResearchProviderFields,
  isPrimaryProvider,
  isResearchProvider,
  maskSecret,
} from "../src/config";

describe("isPrimaryProvider / isResearchProvider", () => {
  it("accepts every documented primary provider", () => {
    for (const opt of PRIMARY_PROVIDER_OPTIONS) {
      expect(isPrimaryProvider(opt.value)).toBe(true);
    }
  });

  it("accepts every documented research provider", () => {
    for (const opt of RESEARCH_PROVIDER_OPTIONS) {
      expect(isResearchProvider(opt.value)).toBe(true);
    }
  });

  it("rejects unknown providers", () => {
    expect(isPrimaryProvider("not-a-thing")).toBe(false);
    expect(isResearchProvider("not-a-thing")).toBe(false);
    // Cross-scope leakage check: research providers are not primary providers.
    expect(isPrimaryProvider("disabled")).toBe(false);
  });
});

describe("getDefaultPrimaryModel", () => {
  it("returns a non-empty model for every supported provider", () => {
    for (const opt of PRIMARY_PROVIDER_OPTIONS) {
      const model = getDefaultPrimaryModel(opt.value);
      expect(model).toBeTruthy();
      expect(model.length).toBeGreaterThan(2);
    }
  });

  it("uses Gemini 3.6 Flash for Google Gemini", () => {
    expect(getDefaultPrimaryModel("google-gemini")).toBe("gemini-3.6-flash");
  });
});

describe("getDefaultResearchModel", () => {
  it("returns a string for every supported provider", () => {
    for (const opt of RESEARCH_PROVIDER_OPTIONS) {
      const model = getDefaultResearchModel(opt.value);
      expect(typeof model).toBe("string");
    }
  });
});

describe("getPrimaryProviderFields", () => {
  it("requires apiKey for OpenAI-style providers", () => {
    for (const provider of ["openai", "anthropic", "mistral", "groq"] as const) {
      const fields = getPrimaryProviderFields(provider);
      const apiKey = fields.find((f) => f.key === "apiKey");
      expect(apiKey, `apiKey field missing for ${provider}`).toBeDefined();
      expect(apiKey?.required).toBe(true);
      expect(apiKey?.kind).toBe("secret");
    }
  });

  it("requires url for ollama (no apiKey)", () => {
    const fields = getPrimaryProviderFields("ollama");
    expect(fields.find((f) => f.key === "url")).toMatchObject({
      required: true,
      kind: "url",
    });
    expect(fields.find((f) => f.key === "apiKey")).toBeUndefined();
  });

  it("requires resourceName + deploymentName + apiKey for azure-openai", () => {
    const fields = getPrimaryProviderFields("azure-openai");
    const required = fields.filter((f) => f.required).map((f) => f.key).sort();
    expect(required).toEqual(["apiKey", "deploymentName", "resourceName"]);
  });

  it("requires projectId + region for vertex (uses ADC, no apiKey)", () => {
    const fields = getPrimaryProviderFields("vertex");
    const required = fields.filter((f) => f.required).map((f) => f.key).sort();
    expect(required).toEqual(["projectId", "region"]);
  });
});

describe("getResearchProviderFields", () => {
  it("returns no fields for disabled scope", () => {
    expect(getResearchProviderFields("disabled")).toEqual([]);
  });

  it("requires apiKey for perplexity", () => {
    const fields = getResearchProviderFields("perplexity");
    expect(fields).toHaveLength(1);
    expect(fields[0]).toMatchObject({ key: "apiKey", required: true, kind: "secret" });
  });
});

describe("maskSecret", () => {
  it("returns empty string for falsy values", () => {
    expect(maskSecret(undefined)).toBe("");
    expect(maskSecret("")).toBe("");
  });

  it("fully masks short secrets (<=8 chars) with no leakage", () => {
    expect(maskSecret("abc")).toBe("•••");
    expect(maskSecret("12345678")).toBe("••••••••");
  });

  it("shows first 3 + last 4 chars for longer secrets", () => {
    const masked = maskSecret("sk-abcdefghijklmnop");
    expect(masked.startsWith("sk-")).toBe(true);
    expect(masked.endsWith("mnop")).toBe(true);
    expect(masked).not.toContain("def");
  });

  it("never reveals the middle of a long key", () => {
    const secret = "sk-proj-supersecretlonglongkey-1234";
    const masked = maskSecret(secret);
    expect(masked).not.toContain("supersecret");
  });
});

// Env-based provider inference was removed (provider config lives in the
// llm_provider_config table only, populated by /setup or /admin/settings/agent).
// The legacy readPrimaryProviderConfigFromEnv / readResearchProviderConfigFromEnv
// are now no-ops that always return null — covered by their integration paths
// in the resolver / settings tests.
