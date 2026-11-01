import { describe, expect, it } from "vitest";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId } from "@neko/db/test-helpers";
import { addLocalGroupMember, app_user, builtinGroupId, createUserGroup, db } from "@neko/db";
import {
  approveActionRequest,
  createActionPolicy,
  createActionRequest,
  getActionPolicy,
  updateActionPolicy,
} from "../../src/workflows/action-store";

const reachable = await dbReachable();

(reachable ? describe : describe.skip)("approver groups", () => {
  it("maps admin approver roles to Administrators and lets only group members approve", async () => {
    const orgId = uniqueOrgId("approver");
    await createTestOrg(orgId);
    try {
      await db().insert(app_user).values([
        { id: `${orgId}-fin`, org_id: orgId, email: "fin@example.test" },
        { id: `${orgId}-other`, org_id: orgId, email: "other@example.test" },
      ]);
      const base = {
        orgId, description: "", appliesToKinds: ["send_slack_message"], appliesToScopes: ["external" as const],
        mode: "approval_required" as const, riskThresholdAutoApprove: null, allowedTargets: null, deniedTargets: null,
        limits: {}, priority: 10, enabled: true,
      };
      const adminPolicy = await createActionPolicy({ ...base, name: "admins approve", approverRole: "admin" });
      expect(adminPolicy.approverGroupId).toBe(await builtinGroupId(orgId, "administrators"));

      const finance = await createUserGroup(orgId, { name: "Finance" });
      await addLocalGroupMember(orgId, finance.id, `${orgId}-fin`);
      const policy = await createActionPolicy({ ...base, name: "finance approves", approverRole: null });
      await updateActionPolicy(orgId, policy.id, { approverGroupId: finance.id });
      expect((await getActionPolicy(orgId, policy.id))?.approverGroupId).toBe(finance.id);

      const request = await createActionRequest({
        orgId, scope: "external", kind: "send_slack_message", status: "pending_approval", policyId: policy.id, intent: "post",
      });
      await expect(approveActionRequest({
        id: request.id, orgId, approverUserId: `${orgId}-other`, approver: { userId: `${orgId}-other`, role: "member" },
      })).rejects.toThrow("approver group");
      const approved = await approveActionRequest({
        id: request.id, orgId, approverUserId: `${orgId}-fin`, approver: { userId: `${orgId}-fin`, role: "member" },
      });
      expect(approved.status).toBe("approved");
    } finally {
      await deleteTestOrg(orgId);
    }
  });
});
