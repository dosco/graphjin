import { describe, expect, it } from "vitest";
import { deriveActionTargets } from "../src/workflows/action-targets";
import { evaluateActionPolicy } from "../src/workflows/policy-engine";
import type { ActionPolicyRecord } from "../src/workflows/action-store";

const emailSpec = { fields: ["to", "cc", "bcc"], as: "email_domain" as const };

function rule(overrides: Partial<ActionPolicyRecord>): ActionPolicyRecord {
  return {
    id: overrides.id ?? "p",
    orgId: "org-1",
    name: overrides.name ?? "rule",
    description: "",
    appliesToKinds: overrides.appliesToKinds ?? ["send_email"],
    appliesToScopes: ["external"],
    mode: overrides.mode ?? "auto_approve",
    riskThresholdAutoApprove: null,
    allowedTargets: overrides.allowedTargets ?? null,
    deniedTargets: overrides.deniedTargets ?? null,
    limits: {},
    approverRole: null,
    priority: overrides.priority ?? 100,
    enabled: true,
    createdAt: new Date(),
    updatedAt: new Date(),
  };
}

const approvedDomains = rule({
  id: "approved",
  allowedTargets: { patterns: ["acme.com", "*.acme.com", "partner.com"], on_miss: "next" },
});
const askEverythingElse = rule({ id: "ask", mode: "approval_required", appliesToKinds: [], priority: 950 });

function decide(payload: Record<string, unknown>, policies = [approvedDomains, askEverythingElse]) {
  const targets = deriveActionTargets(emailSpec, payload);
  return evaluateActionPolicy({ scope: "external", kind: "send_email", targets }, policies);
}

describe("deriveActionTargets", () => {
  it("reads lowercase domains from every recipient field and form", () => {
    expect(
      deriveActionTargets(emailSpec, {
        to: ["Ops <ops@Acme.com>", "cfo@acme.com"],
        cc: "a@eu.acme.com, b@partner.com",
        bcc: null,
      }),
    ).toEqual(["acme.com", "eu.acme.com", "partner.com"]);
  });

  it("marks an address without a domain as invalid", () => {
    expect(deriveActionTargets(emailSpec, { to: "acme.com" })).toEqual(["invalid:acme.com"]);
  });

  it("reads targets nested in the payload, as a pack action's body.to", () => {
    expect(
      deriveActionTargets(
        { fields: ["body.to", "body.cc", "body.bcc"], as: "email_domain" },
        { body: { to: ["a@acme.com"], cc: "b@partner.com" }, to: "ignored@other.com" },
      ),
    ).toEqual(["acme.com", "partner.com"]);
    expect(deriveActionTargets({ fields: ["body.to"], as: "value" }, { body: "x" })).toEqual([]);
  });

  it("returns no targets when the fields are missing", () => {
    expect(deriveActionTargets(emailSpec, {})).toEqual([]);
  });
});

describe("rules on several targets", () => {
  it("sends without approval when every recipient is on the list", () => {
    const d = decide({ to: ["a@acme.com", "b@partner.com"], cc: ["c@eu.acme.com"] });
    expect(d.decision).toBe("allow");
  });

  it("asks when one recipient is outside the list", () => {
    const d = decide({ to: ["a@acme.com"], bcc: ["x@gmail.com"] });
    expect(d.decision).toBe("needs_approval");
    expect(d.decision !== "no_policy" && d.policy.id).toBe("ask");
  });

  it("asks when there are no recipients", () => {
    expect(decide({}).decision).toBe("needs_approval");
  });

  it("matches *.domain on subdomains only", () => {
    const onlySubdomains = rule({ allowedTargets: { patterns: ["*.acme.com"], on_miss: "next" } });
    expect(decide({ to: "a@eu.acme.com" }, [onlySubdomains, askEverythingElse]).decision).toBe("allow");
    expect(decide({ to: "a@acme.com" }, [onlySubdomains, askEverythingElse]).decision).toBe("needs_approval");
    expect(decide({ to: "a@evilacme.com" }, [onlySubdomains, askEverythingElse]).decision).toBe("needs_approval");
  });

  it("keeps the hard deny for a miss when the rule does not set on_miss", () => {
    const strict = rule({ allowedTargets: { patterns: ["acme.com"] } });
    expect(decide({ to: "x@gmail.com" }, [strict, askEverythingElse]).decision).toBe("deny");
  });

  it("denies when any recipient is on a denied list", () => {
    const blocked = rule({ id: "blocked", mode: "never", deniedTargets: { patterns: ["gmail.com"] }, priority: 10 });
    expect(decide({ to: ["a@acme.com", "x@gmail.com"] }, [blocked, approvedDomains]).decision).toBe("deny");
  });
});
