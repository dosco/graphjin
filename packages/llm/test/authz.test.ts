import { describe, expect, it } from "vitest";
import { assertCan, can, ForbiddenError } from "../src/work/authz";

const admin = { userId: "u-admin", role: "admin" } as const;
const member = { userId: "u-member", role: "member" } as const;
const service = { userId: null, role: "service" } as const;

describe("can() — the K2 seam", () => {
  it("admin can do everything", () => {
    expect(can(admin, "write", { kind: "org_settings" })).toBe(true);
    expect(
      can(admin, "approve", { kind: "action_approval", approverRole: "admin" }),
    ).toBe(true);
    expect(
      can(admin, "write", { kind: "personal", ownerUserId: "someone-else" }),
    ).toBe(true);
  });

  it("member reads org settings but never writes them", () => {
    expect(can(member, "read", { kind: "org_settings" })).toBe(true);
    expect(can(member, "write", { kind: "org_settings" })).toBe(false);
  });

  it("member approves unless the policy demands admin", () => {
    expect(
      can(member, "approve", { kind: "action_approval", approverRole: null }),
    ).toBe(true);
    expect(
      can(member, "approve", { kind: "action_approval", approverRole: "member" }),
    ).toBe(true);
    expect(
      can(member, "approve", { kind: "action_approval", approverRole: "admin" }),
    ).toBe(false);
  });

  it("member owns only their personal layer", () => {
    expect(
      can(member, "write", { kind: "personal", ownerUserId: "u-member" }),
    ).toBe(true);
    expect(
      can(member, "write", { kind: "personal", ownerUserId: "u-other" }),
    ).toBe(false);
    expect(can(member, "write", { kind: "personal", ownerUserId: null })).toBe(
      false,
    );
  });

  it("service principals only read", () => {
    expect(can(service, "read", { kind: "org_settings" })).toBe(true);
    expect(
      can(service, "approve", { kind: "action_approval", approverRole: null }),
    ).toBe(false);
  });

  it("assertCan throws ForbiddenError with the actor and resource", () => {
    expect(() =>
      assertCan(member, "write", { kind: "org_settings" }),
    ).toThrowError(ForbiddenError);
    expect(() =>
      assertCan(member, "write", { kind: "org_settings" }),
    ).toThrow(/member u-member may not write org_settings/);
  });
});
