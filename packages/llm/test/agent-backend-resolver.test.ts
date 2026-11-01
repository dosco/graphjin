import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import {
  clearProvider,
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  seedProvider,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { pool } from "@neko/db";
import {
  resolveAgentBackend,
  resolveAgentConcurrency,
} from "../src/agent-backend-resolver";
import { AGENT_DEFAULT_GLOBAL_CAP } from "../src/agent-backend";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn(
    "[agent-backend-resolver] skipping: metadata Postgres unreachable.",
  );
}

describeIfDb("agent backend resolution", () => {
  let orgId: string;

  beforeAll(async () => {
    orgId = uniqueOrgId("resolver");
    await createTestOrg(orgId);
  });

  afterEach(async () => {
    await Promise.all([
      clearProvider(orgId, "agent"),
      clearProvider(orgId, "primary"),
    ]);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  it("always resolves Ax", async () => {
    expect((await resolveAgentBackend(orgId)).id).toBe("ax");
  });

  it("exposes the Ax route configured for the primary provider", async () => {
    await seedProvider(orgId, {
      scope: "primary",
      provider: "google-gemini",
      model: "gemini-3.6-flash",
    });

    const backend = await resolveAgentBackend(orgId);
    expect(backend.id).toBe("ax");
    expect(backend.configuredIdentity).toEqual({
      provider: "google-gemini",
      model: "gemini-3.6-flash",
    });
    expect(backend.model).toBe("gemini-3.6-flash");
  });

  it("overrides the primary model when asked", async () => {
    await seedProvider(orgId, {
      scope: "primary",
      provider: "google-gemini",
      model: "gemini-3.6-flash",
    });
    expect((await resolveAgentBackend(orgId, "gemini-3.8-flash")).model).toBe("gemini-3.8-flash");
  });

  it("does not claim configured identity for a disabled provider", async () => {
    await seedProvider(orgId, {
      scope: "primary",
      provider: "google-gemini",
      model: "gemini-3.6-flash",
      enabled: false,
    });

    const backend = await resolveAgentBackend(orgId);
    expect(backend.configuredIdentity).toBeUndefined();
    expect(backend.model).toBeUndefined();
  });

  it("ignores unrelated values in a stale agent row", async () => {
    await seedProvider(orgId, {
      scope: "agent",
      provider: "obsolete-runtime",
      config: { backend: "obsolete-runtime", globalCap: 30 },
    });
    expect((await resolveAgentBackend(orgId)).id).toBe("ax");
    expect((await resolveAgentConcurrency(orgId)).globalCap).toBe(30);
  });

  it("uses the default concurrency without a row", async () => {
    expect((await resolveAgentConcurrency(orgId)).globalCap).toBe(
      AGENT_DEFAULT_GLOBAL_CAP,
    );
  });

  it("falls back when stored concurrency is malformed", async () => {
    await seedProvider(orgId, {
      scope: "agent",
      provider: "ax",
      config: { globalCap: "not-a-number" },
    });
    expect((await resolveAgentConcurrency(orgId)).globalCap).toBe(
      AGENT_DEFAULT_GLOBAL_CAP,
    );
  });
});
