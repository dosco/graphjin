import { describe, expect, it } from "vitest";
import {
  policySavedCard,
  subscriptionSavedCard,
  workflowSavedCard,
} from "../src/workflows/builder-cards";
import type { ActionPolicyRecord } from "../src/workflows/action-store";
import type {
  SubscriptionRecord,
  WorkflowRecord,
} from "../src/workflows/store";

const workflowFixture: WorkflowRecord = {
  id: "wf-1",
  orgId: "org-1",
  name: "APAC revenue dip check",
  description: "Daily check on APAC revenue.",
  enabled: true,
  status: "active",
  goal: "Surface meaningful APAC revenue dips.",
  systemPromptOverlay: "Show INR in lakhs.",
  steps: [
    { id: "pull", description: "Pull last 7 days" },
    { id: "compare", description: "Compare against prior 7" },
    { id: "flag", description: "Flag drops over 10%" },
  ],
  cron: "0 9 * * *",
  cronTimezone: "Asia/Kolkata",
  cronEnabled: true,
  dailyRunBudget: null,
  outputContract: null,
  createdByThreadId: "t-1",
  createdByRunId: "r-1",
  createdAt: new Date("2026-05-01T00:00:00Z"),
  updatedAt: new Date("2026-05-15T00:00:00Z"),
};

const policyFixture: ActionPolicyRecord = {
  id: "pol-1",
  orgId: "org-1",
  name: "slack_revenue_alerts_autoapprove",
  description: "Auto-approve low-risk Slack alert messages.",
  appliesToKinds: ["send_message"],
  appliesToScopes: ["external"],
  mode: "auto_approve",
  riskThresholdAutoApprove: "low",
  allowedTargets: null,
  deniedTargets: null,
  limits: { daily_cap: 20 },
  approverRole: null,
  priority: 100,
  enabled: true,
  createdByThreadId: null,
  createdByRunId: null,
  createdAt: new Date("2026-05-01T00:00:00Z"),
  updatedAt: new Date("2026-05-15T00:00:00Z"),
};

describe("workflowSavedCard", () => {
  it("emits a v1.0 createSurface + updateComponents pair", () => {
    const messages = workflowSavedCard({
      workflow: workflowFixture,
      action: "created",
    });
    expect(messages).toHaveLength(2);
    expect(messages[0]).toMatchObject({
      version: "v1.0",
      createSurface: {
        surfaceId: "workflow-save-wf-1",
        catalogId: "urn:app:catalog:briefing:v1",
      },
    });
    expect(messages[1]).toMatchObject({
      version: "v1.0",
      updateComponents: { surfaceId: "workflow-save-wf-1" },
    });
  });

  it("shows the operator-friendly cron summary and a link to the detail page", () => {
    const messages = workflowSavedCard({
      workflow: workflowFixture,
      action: "updated",
    });
    const update = messages[1].updateComponents as { components: Array<{ id: string; text?: string }> };
    const body = update.components.find((c) => c.id === "body");
    expect(body?.text).toContain("3 step(s)");
    expect(body?.text).toContain("0 9 * * *");
    expect(body?.text).toContain("Asia/Kolkata");
    expect(body?.text).toContain("[Open detail](/workflows?id=wf-1)");
  });

  it("uses 'Created' vs 'Updated' verb based on the action", () => {
    const created = workflowSavedCard({ workflow: workflowFixture, action: "created" });
    const updated = workflowSavedCard({ workflow: workflowFixture, action: "updated" });
    const createdRoot = (created[1].updateComponents as { components: Array<{ id: string; label?: string }> }).components.find((c) => c.id === "root");
    const updatedRoot = (updated[1].updateComponents as { components: Array<{ id: string; label?: string }> }).components.find((c) => c.id === "root");
    expect(createdRoot?.label).toBe("Created workflow");
    expect(updatedRoot?.label).toBe("Updated workflow");
  });

  it("falls back to placeholder text when description is empty", () => {
    const messages = workflowSavedCard({
      workflow: { ...workflowFixture, description: "" },
      action: "created",
    });
    const body = (messages[1].updateComponents as { components: Array<{ id: string; text?: string }> }).components.find((c) => c.id === "body");
    expect(body?.text).toContain("_No description._");
  });
});

const subscriptionFixture: SubscriptionRecord = {
  id: "sub-1",
  orgId: "org-1",
  workflowId: "wf-1",
  sourceKind: "source_change",
  filter: {
    table: "productinventory",
    where: { quantity: { lt: { col: "product.reorderpoint" } } },
    primary_key: ["productid", "locationid"],
    version_column: "modifieddate",
  },
  enabled: true,
  debounceMs: 0,
  maxConcurrentRuns: 5,
  maxChainDepthOverride: null,
  idempotencyKeyTemplate: "reorder-{primary_key}",
  createdAt: new Date("2026-05-23T00:00:00Z"),
  updatedAt: new Date("2026-05-23T00:00:00Z"),
};

describe("subscriptionSavedCard", () => {
  it("emits a v1.0 createSurface + updateComponents pair keyed by sub id", () => {
    const messages = subscriptionSavedCard({
      subscription: subscriptionFixture,
      workflowName: "Stock alert",
    });
    expect(messages).toHaveLength(2);
    expect(messages[0]).toMatchObject({
      createSurface: { surfaceId: "subscription-save-sub-1" },
    });
  });

  it("renders table, primary_key, filter columns, and idempotency key", () => {
    const messages = subscriptionSavedCard({
      subscription: subscriptionFixture,
      workflowName: "Stock alert",
    });
    const body = (
      messages[1].updateComponents as {
        components: Array<{ id: string; text?: string }>;
      }
    ).components.find((c) => c.id === "body");
    expect(body?.text).toContain("Stock alert");
    expect(body?.text).toContain("productinventory");
    expect(body?.text).toContain("productid, locationid");
    expect(body?.text).toContain("quantity");
    expect(body?.text).toContain("reorder-{primary_key}");
    expect(body?.text).toContain("[Open detail](/workflows?id=wf-1)");
  });

  it("falls back to '(no filter)' when the where clause is empty", () => {
    const messages = subscriptionSavedCard({
      subscription: {
        ...subscriptionFixture,
        filter: {
          table: "productinventory",
          where: {},
          primary_key: ["productid", "locationid"],
        },
      },
      workflowName: "Stock alert",
    });
    const body = (
      messages[1].updateComponents as {
        components: Array<{ id: string; text?: string }>;
      }
    ).components.find((c) => c.id === "body");
    expect(body?.text).toContain("_(no filter)_");
  });
});

describe("policySavedCard", () => {
  it("renders mode, scopes, kinds, and a link to the detail page", () => {
    const messages = policySavedCard({
      policy: policyFixture,
      action: "created",
    });
    const body = (messages[1].updateComponents as { components: Array<{ id: string; text?: string }> }).components.find((c) => c.id === "body");
    expect(body?.text).toContain("`auto_approve`");
    expect(body?.text).toContain("external");
    expect(body?.text).toContain("`send_message`");
    expect(body?.text).toContain("[Open detail](/admin/rules/pol-1)");
  });

  it("shows '(all kinds)' when appliesToKinds is empty", () => {
    const messages = policySavedCard({
      policy: { ...policyFixture, appliesToKinds: [] },
      action: "created",
    });
    const body = (messages[1].updateComponents as { components: Array<{ id: string; text?: string }> }).components.find((c) => c.id === "body");
    expect(body?.text).toContain("_(all kinds)_");
  });
});
