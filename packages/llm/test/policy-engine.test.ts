import { describe, expect, it } from "vitest";
import { evaluateActionPolicy } from "../src/workflows/policy-engine";
import type {
  ActionPolicyMode,
  ActionPolicyRecord,
  ActionScope,
  RiskLevel,
} from "../src/workflows/action-store";

function policy(
  overrides: Partial<ActionPolicyRecord> & { mode: ActionPolicyMode },
): ActionPolicyRecord {
  return {
    id: overrides.id ?? "pol-1",
    orgId: overrides.orgId ?? "org-1",
    name: overrides.name ?? "test",
    description: overrides.description ?? "",
    appliesToKinds: overrides.appliesToKinds ?? [],
    appliesToScopes: overrides.appliesToScopes ?? [],
    mode: overrides.mode,
    riskThresholdAutoApprove:
      overrides.riskThresholdAutoApprove !== undefined
        ? overrides.riskThresholdAutoApprove
        : null,
    allowedTargets: overrides.allowedTargets ?? null,
    deniedTargets: overrides.deniedTargets ?? null,
    limits: overrides.limits ?? {},
    approverRole: overrides.approverRole ?? null,
    priority: overrides.priority ?? 100,
    enabled: overrides.enabled ?? true,
    createdAt: new Date(),
    updatedAt: new Date(),
  };
}

describe("evaluateActionPolicy", () => {
  it("returns no_policy when nothing matches", () => {
    const d = evaluateActionPolicy(
      { scope: "external" as ActionScope, kind: "send_message" },
      [],
    );
    expect(d.decision).toBe("no_policy");
  });

  it("auto_approve allows when no risk threshold", () => {
    const p = policy({
      mode: "auto_approve",
      appliesToScopes: ["internal" as ActionScope],
    });
    const d = evaluateActionPolicy(
      { scope: "internal" as ActionScope, kind: "memory_write" },
      [p],
    );
    expect(d.decision).toBe("allow");
  });

  it("auto_approve escalates to approval when risk exceeds threshold", () => {
    const p = policy({
      mode: "auto_approve",
      appliesToScopes: ["internal" as ActionScope],
      riskThresholdAutoApprove: "medium" as RiskLevel,
    });
    const d = evaluateActionPolicy(
      {
        scope: "internal" as ActionScope,
        kind: "memory_write",
        riskLevel: "high" as RiskLevel,
      },
      [p],
    );
    expect(d.decision).toBe("needs_approval");
  });

  it("approval_required always returns needs_approval", () => {
    const p = policy({
      mode: "approval_required",
      appliesToScopes: ["external" as ActionScope],
    });
    const d = evaluateActionPolicy(
      { scope: "external" as ActionScope, kind: "send_message" },
      [p],
    );
    expect(d.decision).toBe("needs_approval");
  });

  it("never returns deny", () => {
    const p = policy({
      mode: "never",
      appliesToScopes: ["external" as ActionScope],
    });
    const d = evaluateActionPolicy(
      { scope: "external" as ActionScope, kind: "delete_record" },
      [p],
    );
    expect(d.decision).toBe("deny");
  });

  it("denied_targets blocks matching targets", () => {
    const p = policy({
      mode: "auto_approve",
      appliesToScopes: ["external" as ActionScope],
      deniedTargets: { patterns: ["prod-*"] },
    });
    const d = evaluateActionPolicy(
      {
        scope: "external" as ActionScope,
        kind: "send_message",
        target: "prod-channel",
      },
      [p],
    );
    expect(d.decision).toBe("deny");
  });

  it("allowed_targets gates matching targets only", () => {
    const p = policy({
      mode: "auto_approve",
      appliesToScopes: ["external" as ActionScope],
      allowedTargets: { patterns: ["safe-channel"] },
    });
    const inAllow = evaluateActionPolicy(
      {
        scope: "external" as ActionScope,
        kind: "send_message",
        target: "safe-channel",
      },
      [p],
    );
    expect(inAllow.decision).toBe("allow");
    const outOfAllow = evaluateActionPolicy(
      {
        scope: "external" as ActionScope,
        kind: "send_message",
        target: "other",
      },
      [p],
    );
    expect(outOfAllow.decision).toBe("deny");
  });

  it("respects priority — lower number wins", () => {
    const strict = policy({
      id: "strict",
      name: "strict",
      mode: "approval_required",
      appliesToScopes: ["internal" as ActionScope],
      priority: 100,
    });
    const lax = policy({
      id: "lax",
      name: "lax",
      mode: "auto_approve",
      appliesToScopes: ["internal" as ActionScope],
      priority: 900,
    });
    const d = evaluateActionPolicy(
      { scope: "internal" as ActionScope, kind: "memory_write" },
      [lax, strict],
    );
    expect(d.decision).toBe("needs_approval");
    if (d.decision === "needs_approval") {
      expect(d.policy.id).toBe("strict");
    }
  });

  it("scope and kind filters narrow applicability", () => {
    const onlyExternal = policy({
      mode: "approval_required",
      appliesToScopes: ["external" as ActionScope],
    });
    const onlyMemory = policy({
      mode: "auto_approve",
      appliesToKinds: ["memory_write"],
    });
    const d = evaluateActionPolicy(
      { scope: "internal" as ActionScope, kind: "memory_write" },
      [onlyExternal, onlyMemory],
    );
    // onlyExternal doesn't match scope; onlyMemory matches kind → auto_approve.
    expect(d.decision).toBe("allow");
  });
});
