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
  resolveAgentBackendId,
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

  it("resolves Hermes when no primary provider can run Ax", async () => {
    expect(await resolveAgentBackendId(orgId)).toBe("hermes");
    expect((await resolveAgentBackend(orgId)).id).toBe("hermes");
  });

  it("defaults to Ax when the primary provider has an Ax route", async () => {
    await seedProvider(orgId, {
      scope: "primary",
      provider: "google-gemini",
      model: "gemini-3.6-flash",
    });
    expect(await resolveAgentBackendId(orgId)).toBe("ax");
  });

  it("keeps a saved Hermes choice", async () => {
    await seedProvider(orgId, {
      scope: "primary",
      provider: "google-gemini",
      model: "gemini-3.6-flash",
    });
    await seedProvider(orgId, {
      scope: "agent",
      provider: "hermes",
      config: { backend: "hermes" },
    });
    expect(await resolveAgentBackendId(orgId)).toBe("hermes");
  });

  it("exposes the exact provider and model configured for the Hermes runtime", async () => {
    await seedProvider(orgId, {
      scope: "primary",
      provider: "google-gemini",
      model: "gemini-3.6-flash",
    });

    const backend = await resolveAgentBackend(orgId, "hermes");
    expect(backend.configuredIdentity).toEqual({
      provider: "gemini",
      model: "gemini-3.6-flash",
    });
    expect(backend.model).toBe("gemini-3.6-flash");
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
    expect(await resolveAgentBackendId(orgId)).toBe("hermes");
    expect((await resolveAgentBackend(orgId)).id).toBe("hermes");
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
      provider: "hermes",
      config: { globalCap: "not-a-number" },
    });
    expect((await resolveAgentConcurrency(orgId)).globalCap).toBe(
      AGENT_DEFAULT_GLOBAL_CAP,
    );
  });
});
