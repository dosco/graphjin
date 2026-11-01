import { describe, expect, it } from "vitest";
import { assertPackActionReady } from "../src/packs/action-preflight.js";

describe("pack action preflight", () => {
  it("accepts an enabled ready action", () => {
    expect(() =>
      assertPackActionReady("magento.add_internal_order_comment", {
        enabled: true,
        readiness: "ready",
        reason: null,
      }),
    ).not.toThrow();
  });

  it("rejects an action while its operator is blocked", () => {
    expect(() =>
      assertPackActionReady("magento.add_internal_order_comment", {
        enabled: true,
        readiness: "blocked",
        reason: "GraphJin REST mutations require a newer release",
      }),
    ).toThrow(
      "pack action magento.add_internal_order_comment is blocked: GraphJin REST mutations require a newer release",
    );
  });

  it("rejects a disabled action even when marked ready", () => {
    expect(() =>
      assertPackActionReady("magento.add_internal_order_comment", {
        enabled: false,
        readiness: "ready",
        reason: null,
      }),
    ).toThrow("pack action magento.add_internal_order_comment is blocked");
  });
});
