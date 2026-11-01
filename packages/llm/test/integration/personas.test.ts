import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { pool } from "@neko/db";
import {
  buildOperatorProfileSection,
  getOperatorProfile,
  upsertOperatorProfile,
} from "../../src/work/personas";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[personas] skipping: Postgres unreachable.");
}

describeIfDb("operator personas (CV3)", () => {
  const orgId = uniqueOrgId("persona");

  beforeAll(async () => {
    await createTestOrg(orgId);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
    vi.unstubAllEnvs();
  });

  it("upserts and falls back: user row wins, org-default ('') backs it", async () => {
    expect(await getOperatorProfile(orgId, "u1")).toBeNull();

    await upsertOperatorProfile({
      orgId,
      roleTemplate: "Head of Ops",
      focusAreas: ["fulfillment", "inventory"],
    });
    const fallback = await getOperatorProfile(orgId, "u1");
    expect(fallback?.userId).toBe("");
    expect(fallback?.roleTemplate).toBe("Head of Ops");

    await upsertOperatorProfile({
      orgId,
      userId: "u1",
      displayName: "Ada",
      roleTemplate: "CFO",
      briefMd: "Prefers tables over prose.",
    });
    const own = await getOperatorProfile(orgId, "u1");
    expect(own?.userId).toBe("u1");
    expect(own?.roleTemplate).toBe("CFO");

    // Upsert updates in place (unique org+user).
    await upsertOperatorProfile({ orgId, userId: "u1", roleTemplate: "CEO" });
    expect((await getOperatorProfile(orgId, "u1"))?.roleTemplate).toBe("CEO");
  });

  it("compiles the prompt block; raw answers never appear", async () => {
    const profile = await upsertOperatorProfile({
      orgId,
      userId: "u2",
      displayName: "Bo",
      roleTemplate: "CRO",
      focusAreas: ["pipeline"],
      answers: { secret_note: "DO-NOT-LEAK" },
      briefMd: "Weekly cadence.",
    });
    const section = buildOperatorProfileSection(profile);
    expect(section).toContain("<operator-profile>");
    expect(section).toContain("You are working for Bo.");
    expect(section).toContain("Their role: CRO.");
    expect(section).toContain("- pipeline");
    expect(section).toContain("Weekly cadence.");
    expect(section).not.toContain("DO-NOT-LEAK");
    expect(buildOperatorProfileSection(null)).toBe("");
  });
});
