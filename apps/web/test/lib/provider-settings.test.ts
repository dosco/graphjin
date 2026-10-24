import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import {
  clearProvider,
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  seedProvider,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { and, db, eq, llm_provider_config, pool } from "@neko/db";
import {
  getProviderSettingsPayload,
  hasPrimaryProviderSetup,
  resolveResearchStatus,
  saveProviderDraft,
} from "@/lib/provider-settings";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn(
    "[provider-settings] skipping: metadata Postgres unreachable.",
  );
}

async function readPrimary(orgId: string) {
  const rows = await db()
    .select({
      provider: llm_provider_config.provider,
      model: llm_provider_config.model,
      enabled: llm_provider_config.enabled,
      secrets: llm_provider_config.secrets,
      config: llm_provider_config.config,
    })
    .from(llm_provider_config)
    .where(
      and(
        eq(llm_provider_config.org_id, orgId),
        eq(llm_provider_config.scope, "primary"),
      ),
    );
  return rows[0] ?? null;
}

const ORIGINAL_ENV = { ...process.env };

describeIfDb("provider-settings", () => {
  let orgId: string;

  beforeAll(async () => {
    orgId = uniqueOrgId("provider-settings");
    await createTestOrg(orgId);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  afterEach(async () => {
    await clearProvider(orgId, "agent");
    await clearProvider(orgId, "primary");
    await clearProvider(orgId, "research");
    process.env = { ...ORIGINAL_ENV };
  });

  describe("getProviderSettingsPayload", () => {
    it("returns default config when no rows", async () => {
      const payload = await getProviderSettingsPayload(orgId);
      expect(payload.primary.source).toBe("default");
      expect(payload.research.source).toBe("default");
    });

    it("returns the stored row when set", async () => {
      await seedProvider(orgId, {
        scope: "primary",
        provider: "anthropic",
        model: "claude-opus-4-7",
        secrets: { apiKey: "sk-ant-stored" },
      });
      const payload = await getProviderSettingsPayload(orgId);
      expect(payload.primary.source).toBe("org");
      expect(payload.primary.provider).toBe("anthropic");
      // secretStatus is masked, never plaintext.
      expect(payload.primary.secretStatus.apiKey).not.toBe("sk-ant-stored");
      expect(payload.primary.secretStatus.apiKey).toMatch(/[•*]/);
    });
  });

  describe("saveProviderDraft — primary", () => {
    it("round-trips secrets via encryption", async () => {
      await saveProviderDraft(orgId, {
        scope: "primary",
        provider: "anthropic",
        model: "claude-opus-4-7",
        secrets: { apiKey: "sk-ant-roundtrip" },
      });
      // The DB row's secret field can be plaintext (no APP_SECRET_KEY) or
      // encrypted (with one). Either way the read-back via the lib should
      // surface a masked token, not the plaintext.
      const payload = await getProviderSettingsPayload(orgId);
      expect(payload.primary.secretStatus.apiKey).not.toBe("sk-ant-roundtrip");
      expect(payload.primary.secretStatus.apiKey).toMatch(/[•*]/);
    });

    it("provider switch wipes prior provider's secrets", async () => {
      await seedProvider(orgId, {
        scope: "primary",
        provider: "google-gemini",
        model: "gemini-pro-latest",
        secrets: { apiKey: "old-gemini-key" },
      });
      await saveProviderDraft(orgId, {
        scope: "primary",
        provider: "openai",
        model: "gpt-4.1-mini",
        secrets: { apiKey: "new-openai-key" },
      });
      const primary = await readPrimary(orgId);
      const secrets = primary!.secrets as Record<string, unknown>;
      // Old key for old provider must not bleed through. Only the new key
      // remains in the secrets map.
      const values = Object.values(secrets).map(String);
      expect(values.some((v) => v.includes("old-gemini-key"))).toBe(false);
    });
  });

  describe("saveProviderDraft — agent providers", () => {
    it("allows any supported primary provider", async () => {
      await seedProvider(orgId, {
        scope: "agent",
        provider: "ax",
        config: { globalCap: 10 },
      });
      await expect(
        saveProviderDraft(orgId, {
          scope: "primary",
          provider: "openai",
          model: "gpt-4.1-mini",
          secrets: { apiKey: "sk-openai" },
        }),
      ).resolves.toBeDefined();
    });
  });

  describe("saveProviderDraft — research", () => {
    it("disabled research can be saved with empty secrets", async () => {
      const saved = await saveProviderDraft(orgId, {
        scope: "research",
        provider: "disabled",
        model: "",
        enabled: false,
        secrets: {},
      });
      expect(saved.enabled).toBe(false);
    });

    it("enabled perplexity requires apiKey", async () => {
      await expect(
        saveProviderDraft(orgId, {
          scope: "research",
          provider: "perplexity",
          model: "sonar-deep-research",
          enabled: true,
          secrets: {},
        }),
      ).rejects.toThrow(/required/i);
    });
  });

  describe("hasPrimaryProviderSetup", () => {
    it("returns true with valid org row", async () => {
      await seedProvider(orgId, {
        scope: "primary",
        provider: "anthropic",
        model: "claude-opus-4-7",
        secrets: { apiKey: "sk-ant" },
      });
      expect(await hasPrimaryProviderSetup(orgId)).toBe(true);
    });

    it("returns false with no row", async () => {
      expect(await hasPrimaryProviderSetup(orgId)).toBe(false);
    });

    it("returns false when row is disabled", async () => {
      await seedProvider(orgId, {
        scope: "primary",
        provider: "anthropic",
        model: "claude-opus-4-7",
        enabled: false,
        secrets: { apiKey: "sk-ant" },
      });
      expect(await hasPrimaryProviderSetup(orgId)).toBe(false);
    });
  });

  describe("resolveResearchStatus", () => {
    it("returns 'disabled' for explicit disabled provider", async () => {
      await seedProvider(orgId, {
        scope: "research",
        provider: "disabled",
        enabled: false,
      });
      expect(await resolveResearchStatus(orgId)).toBe("disabled");
    });

    it("returns 'enabled' for valid perplexity row", async () => {
      await seedProvider(orgId, {
        scope: "research",
        provider: "perplexity",
        model: "sonar-deep-research",
        enabled: true,
        secrets: { apiKey: "pplx-test" },
      });
      expect(await resolveResearchStatus(orgId)).toBe("enabled");
    });
  });
});
