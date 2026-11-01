import { afterAll, afterEach, describe, expect, it, vi } from "vitest";
import { action_request, db, pool } from "@neko/db";
import { dbReachable, withTestOrg } from "@neko/db/test-helpers";
import { approveActionRequest, autoApprovePreparedActionRequest, hasHumanActionApproval } from "../../src/workflows/action-store";

const reachable = await dbReachable();
describe.skipIf(!reachable)("Explicit solo action approvals", () => {
  afterAll(async () => { await pool().end(); });
  afterEach(() => vi.unstubAllEnvs());
  it.each(["solo", "team"])("records userless human approval only in the solo profile (%s)", async (profile) => {
    vi.stubEnv("OPENNEKO_PROFILE", profile);
    await withTestOrg(async (orgId) => {
      const [row] = await db().insert(action_request).values({ org_id: orgId, scope: "external", kind: "test", status: "pending_approval" }).returning();
      const approved = await approveActionRequest({ id: row.id, orgId, approverUserId: null, approver: { userId: null, role: "admin" } });
      expect(await hasHumanActionApproval(approved)).toBe(profile === "solo");
    });
  });
  it("never interprets automatic approval or legacy userless calls as human approval", async () => {
    vi.stubEnv("OPENNEKO_PROFILE", "solo");
    await withTestOrg(async (orgId) => {
      for (const automatic of [true, false]) {
        const [row] = await db().insert(action_request).values({ org_id: orgId, scope: "external", kind: "test", status: "pending_approval" }).returning();
        const approved = automatic
          ? await autoApprovePreparedActionRequest({ id: row.id, orgId, reason: "test" })
          : await approveActionRequest({ id: row.id, orgId, approverUserId: null });
        expect(await hasHumanActionApproval(approved)).toBe(false);
      }
    });
  });
});
