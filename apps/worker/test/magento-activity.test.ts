import { describe, expect, it } from "vitest";
import {
  buildMagentoActivity,
  isMagentoTestRule,
  type MagentoActivityChangeset,
} from "../src/packs/magento-activity.js";

function changeset(
  input: Partial<MagentoActivityChangeset> & Pick<MagentoActivityChangeset, "id">,
): MagentoActivityChangeset {
  return {
    id: input.id,
    domain: "catalog",
    operationId: "product_bulk_update",
    executionMode: "controlled_automation_eligible",
    status: "applied",
    summary: "Seasonal price update",
    bulkUuid: null,
    inverseOfId: null,
    scope: {},
    capSnapshot: {},
    createdAt: "2026-08-28T16:04:07.740Z",
    reconciledAt: "2026-08-28T16:04:10.374Z",
    rows: [
      {
        entityRef: "SKU-1",
        beforeImage: { price: 10 },
        afterImage: { product: { price: 11 } },
      },
      {
        entityRef: "SKU-2",
        beforeImage: { price: 20 },
        afterImage: { product: { price: 22 } },
      },
    ],
    ...input,
  };
}

describe("Magento user activity", () => {
  it("folds a successful inverse into one plain-language reverted activity", () => {
    const activity = buildMagentoActivity({
      changesets: [
        changeset({ id: "original" }),
        changeset({
          id: "inverse",
          inverseOfId: "original",
          summary: "Undo: Seasonal price update",
          createdAt: "2026-08-28T16:04:11.306Z",
          reconciledAt: "2026-08-28T16:04:13.742Z",
        }),
      ],
      handoffs: [],
    });

    expect(activity).toHaveLength(1);
    expect(activity[0]).toMatchObject({
      id: "inverse",
      title: "Prices restored",
      description: "Restored the original prices for 2 products.",
      outcome: "reverted",
      outcomeLabel: "Reverted",
      currentState: "No current price change",
    });
    expect(activity[0]?.technical).toMatchObject({
      originalRequest: "Seasonal price update",
      inverseOfReference: "original",
    });
  });

  it("keeps a failed restoration separate so the applied change remains visible", () => {
    const activity = buildMagentoActivity({
      changesets: [
        changeset({ id: "original" }),
        changeset({
          id: "inverse",
          inverseOfId: "original",
          status: "failed",
          summary: "Undo: Seasonal price update",
          createdAt: "2026-08-28T16:05:11.306Z",
          reconciledAt: null,
        }),
      ],
      handoffs: [],
    });

    expect(activity.map((item) => item.id)).toEqual(["inverse", "original"]);
    expect(activity[0]).toMatchObject({ outcome: "failed", title: "Price update failed" });
    expect(activity[1]).toMatchObject({ outcome: "completed", title: "Prices updated" });
  });

  it("marks acceptance records as test activity without leaking test copy into the title", () => {
    const [activity] = buildMagentoActivity({
      changesets: [
        changeset({
          id: "acceptance",
          summary: "Acceptance two-product async bulk price change",
          scope: { activityOrigin: "acceptance_test" },
        }),
      ],
      handoffs: [],
    });

    expect(activity).toMatchObject({
      title: "Prices updated",
      description: "Updated prices for 2 products.",
      source: "test",
      sourceLabel: "Local test",
      isTest: true,
    });
    expect(activity?.title).not.toMatch(/acceptance|async|bulk/i);
  });

  it("uses outcome language for approval and Magento Admin handoffs", () => {
    const activity = buildMagentoActivity({
      changesets: [changeset({ id: "approval", status: "pending_approval" })],
      handoffs: [
        {
          id: "refund",
          kind: "online_refund",
          entityRef: "order-100",
          status: "ready_for_human",
          draft: {},
          evidence: {},
          createdAt: "2026-08-28T17:04:07.740Z",
          completedAt: null,
        },
      ],
    });

    expect(activity[0]).toMatchObject({
      title: "Refund ready in Magento Admin",
      outcomeLabel: "Action needed",
      currentState: "OpenNeko did not change Magento",
    });
    expect(activity[1]).toMatchObject({
      title: "Price update awaiting approval",
      outcomeLabel: "Awaiting approval",
      currentState: "Nothing changed yet",
    });
  });
});

describe("Magento test rules", () => {
  it("recognizes explicit and legacy local acceptance rules", () => {
    expect(isMagentoTestRule({
      name: "Any rule name",
      compiledPolicy: { source: "acceptance_test" },
    })).toBe(true);
    expect(isMagentoTestRule({
      name: "Local acceptance openneko-v2-old-run",
      compiledPolicy: { source: "admin_plain_language" },
    })).toBe(true);
  });

  it("keeps administrator-created rules visible", () => {
    expect(isMagentoTestRule({
      name: "Low-stock price correction",
      compiledPolicy: { source: "admin_plain_language" },
    })).toBe(false);
  });
});
