import { afterAll, describe, expect, it } from "vitest";
import { db, pack_action_definition, pool } from "@neko/db";
import { dbReachable, withTestOrg } from "@neko/db/test-helpers";
import {
  createActionPolicy,
  createActionRequest,
} from "../../src/workflows";
import { inProcessControlPlane } from "../../src/work/control-plane";

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
  // A pack action that reads its recipients from the payload, as the Resend pack does.
  await db().insert(pack_action_definition).values({
    org_id: orgId,
    kind: "send_email",
    definition: { targets: { fields: ["body.to", "body.cc", "body.bcc"], as: "email_domain" } },
    definition_hash: "fixture",
    readiness: "ready",
  });
}

describeIfDb("action targets read from the payload", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("allows an email when every recipient is on the list", async () => {
    await withTestOrg(async (orgId) => {
      await seedRules(orgId);
      const decision = await inProcessControlPlane.evaluateActionPolicy({
        orgId,
        scope: "external",
        kind: "send_email",
        target: "gmail.com",
        payload: { body: { to: ["ops@acme.com"], cc: "Finance <cfo@partner.com>" } },
      });
      expect(decision.decision).toBe("allow");
    }, "targets");
  });

  it("ignores the agent's target and asks when a recipient is outside the list", async () => {
    await withTestOrg(async (orgId) => {
      await seedRules(orgId);
      const payload = { body: { to: ["ops@acme.com"], bcc: ["x@gmail.com"] } };
      const decision = await inProcessControlPlane.evaluateActionPolicy({
        orgId,
        scope: "external",
        kind: "send_email",
        target: "acme.com",
        payload,
      });
      expect(decision.decision).toBe("needs_approval");

      const request = await createActionRequest({
        orgId,
        scope: "external",
        kind: "send_email",
        target: "acme.com",
        payload,
        status: "approved",
      });
      expect(request.status).toBe("pending_approval");
      expect(request.target).toBe("acme.com, gmail.com");
    }, "targets");
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
        kind: "post_note",
        target: "acme.com",
        payload: { to: ["x@gmail.com"] },
        status: "pending_approval",
      });
      expect(request.target).toBe("acme.com");
    }, "targets");
  });
});
