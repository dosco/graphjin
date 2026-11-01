import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { db, eq, organization, pool } from "@neko/db";
import { getSetupCompleteAt } from "@/lib/org-state";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn(
    "[org-state] skipping: metadata Postgres unreachable. Run `docker compose up -d`.",
  );
}

describeIfDb("getSetupCompleteAt", () => {
  let orgId: string;

  beforeAll(async () => {
    orgId = uniqueOrgId("org-state");
    await createTestOrg(orgId);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  afterEach(async () => {
    await db()
      .update(organization)
      .set({ setup_complete_at: null })
      .where(eq(organization.id, orgId));
  });

  it("returns null for a fresh org", async () => {
    expect(await getSetupCompleteAt(orgId)).toBeNull();
  });

  it("returns the Date when set", async () => {
    const now = new Date();
    await db()
      .update(organization)
      .set({ setup_complete_at: now })
      .where(eq(organization.id, orgId));
    const got = await getSetupCompleteAt(orgId);
    expect(got).toBeInstanceOf(Date);
    expect(got!.getTime()).toBeCloseTo(now.getTime(), -2);
  });

  it("returns null for a non-existent org (graceful)", async () => {
    expect(await getSetupCompleteAt("nonexistent-org-xyz")).toBeNull();
  });
});
