import { describe, expect, it } from "vitest";
import { actionApprovalAttribution } from "@/lib/action-copy";

describe("action trust copy", () => {
  it("makes policy-approved actions explicit auto-fired rule rows", () => {
    expect(actionApprovalAttribution('rule "Log customer activity"')).toBe(
      'auto-fired by rule "Log customer activity"',
    );
    expect(actionApprovalAttribution("you · Amit")).toBe("approved by you · Amit");
  });
});
