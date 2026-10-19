import { afterAll, describe, expect, it } from "vitest";
import { pool } from "@neko/db";
import { dbReachable, withTestOrg } from "@neko/db/test-helpers";
import {
  createActionPolicy,
  createActionRequest,
} from "../../src/workflows";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[action-targets] skipping: Postgres unreachable.");
}

async function seedRules(orgId: string) {
  const base = {
    orgId,
    description: "",
    appliesToScopes: ["external" as const],
    riskThresholdAutoApprove: null,
    deniedTargets: null,
    limits: {},
    approverRole: null,
    enabled: true,
  };
  await createActionPolicy({
    ...base,
    name: "Approved recipients",
    appliesToKinds: ["send_email", "send_webhook"],
    mode: "auto_approve",
    allowedTargets: { patterns: ["acme.com", "partner.com", "https://hooks.acme.com/*"], on_miss: "next" },
    priority: 100,
  });
  await createActionPolicy({
    ...base,
    name: "Ask for everything else",
    appliesToKinds: [],
    mode: "approval_required",
    allowedTargets: null,
    priority: 950,
  });
}

describeIfDb("action targets read from the payload", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("checks a webhook against payload.url", async () => {
    await withTestOrg(async (orgId) => {
      await seedRules(orgId);
      const request = await createActionRequest({
        orgId,
        scope: "external",
        kind: "send_webhook",
        target: "https://hooks.acme.com/alerts",
        payload: { url: "https://attacker.example/collect" },
        status: "approved",
      });
      expect(request.status).toBe("pending_approval");
      expect(request.target).toBe("https://attacker.example/collect");
    }, "targets");
  });

  it("keeps the agent's target for a kind without a target spec", async () => {
    await withTestOrg(async (orgId) => {
      await seedRules(orgId);
      const request = await createActionRequest({
        orgId,
        scope: "external",
        kind: "send_email",
        target: "acme.com",
        payload: { to: ["x@gmail.com"] },
        status: "pending_approval",
      });
      expect(request.target).toBe("acme.com");
    }, "targets");
  });
});
