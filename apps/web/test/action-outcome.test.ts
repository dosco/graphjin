import { describe, expect, it } from "vitest";
import { actionOutcome, describeActionFailure, describeOutcome } from "@/lib/action-outcome";

describe("actionOutcome", () => {
  it("keeps a person's rejection apart from a system failure", () => {
    expect(actionOutcome("rejected")).toBe("rejected");
    expect(actionOutcome("failed", "failed")).toBe("failed");
  });

  it("reports an executed change that was not confirmed as needing a check", () => {
    expect(actionOutcome("executed", "reconcile_required")).toBe("needs_check");
    expect(actionOutcome("executed", "partially_applied")).toBe("partial");
    expect(actionOutcome("executed", "applied")).toBe("completed");
    expect(actionOutcome("executed", "succeeded")).toBe("completed");
  });
});

describe("describeActionFailure", () => {
  it("names an unreachable system", () => {
    expect(
      describeActionFailure("Every Magento change-set row failed: fetch failed (ECONNREFUSED)", "Magento"),
    ).toBe("OpenNeko could not reach Magento, so it changed nothing. Ask a follow-up to propose it again once Magento is back.");
  });

  it("falls back to a plain sentence", () => {
    expect(describeActionFailure("boom", "Magento")).toBe("The change did not run in Magento. OpenNeko changed nothing.");
  });
});

describe("describeOutcome", () => {
  it("says nothing extra for a completed change", () => {
    expect(describeOutcome("completed", null, "Magento")).toBeNull();
  });
});
