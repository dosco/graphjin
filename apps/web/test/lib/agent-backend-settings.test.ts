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
import { AGENT_DEFAULT_GLOBAL_CAP } from "@neko/llm";
import {
  getAgentSettings,
  getAgentSettingsPayload,
  saveAgentSettingsDraft,
} from "@/lib/agent-backend-settings";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[agent-settings] skipping: metadata Postgres unreachable.");
}

describeIfDb("agent-settings", () => {
  let orgId: string;

  beforeAll(async () => {
    orgId = uniqueOrgId("agent-settings");
    await createTestOrg(orgId);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  afterEach(async () => {
    await clearProvider(orgId, "agent");
    await clearProvider(orgId, "primary");
  });

  it("returns concurrency defaults with no row", async () => {
    await expect(getAgentSettings(orgId)).resolves.toEqual({
      source: "default",
      globalCap: AGENT_DEFAULT_GLOBAL_CAP,
    });
  });

  it("returns the concurrency payload", async () => {
    await expect(getAgentSettingsPayload(orgId)).resolves.toEqual({
      agent: {
        source: "default",
        globalCap: AGENT_DEFAULT_GLOBAL_CAP,
      },
      defaults: { globalCap: AGENT_DEFAULT_GLOBAL_CAP },
    });
  });

  it("persists the concurrency on the Ax agent row", async () => {
    await saveAgentSettingsDraft(orgId, { globalCap: 50 });
    expect((await getAgentSettings(orgId)).globalCap).toBe(50);

    const [row] = await db()
      .select({
        provider: llm_provider_config.provider,
        config: llm_provider_config.config,
      })
      .from(llm_provider_config)
      .where(
        and(
          eq(llm_provider_config.org_id, orgId),
          eq(llm_provider_config.scope, "agent"),
        ),
      );
    expect(row.provider).toBe("ax");
    expect(row.config).toEqual({ globalCap: 50 });
  });

  it("falls back to the default for an invalid cap", async () => {
    await saveAgentSettingsDraft(orgId, { globalCap: "not-a-number" });
    expect((await getAgentSettings(orgId)).globalCap).toBe(
      AGENT_DEFAULT_GLOBAL_CAP,
    );
  });

  it("keeps the saved cap on a later save without one", async () => {
    await saveAgentSettingsDraft(orgId, { globalCap: 9 });
    await expect(saveAgentSettingsDraft(orgId, {})).resolves.toEqual({ source: "org", globalCap: 9 });
  });

  it("does not alter the primary provider", async () => {
    await seedProvider(orgId, {
      scope: "primary",
      provider: "google-gemini",
      model: "gemini-pro-latest",
      secrets: { apiKey: "gemini-key" },
    });
    await saveAgentSettingsDraft(orgId, { globalCap: 10 });
    const [primary] = await db()
      .select({ provider: llm_provider_config.provider })
      .from(llm_provider_config)
      .where(
        and(
          eq(llm_provider_config.org_id, orgId),
          eq(llm_provider_config.scope, "primary"),
        ),
      );
    expect(primary.provider).toBe("google-gemini");
  });
});
