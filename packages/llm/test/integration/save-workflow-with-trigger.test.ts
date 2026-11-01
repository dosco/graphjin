import { afterAll, describe, expect, it } from "vitest";
import { and, db, eq, pool, subscription } from "@neko/db";
import { dbReachable, seedDataSource, withTestOrg } from "@neko/db/test-helpers";
import { saveWorkflowWithTrigger } from "../../src/workflows";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[save-workflow-with-trigger] skipping: Postgres unreachable.");
}

const stockTrigger = {
  table: "productinventory",
  where: { quantity: { lt: { col: "product.reorderpoint" } } },
  primary_key: ["productid", "locationid"],
  version_column: "modifieddate",
};

describeIfDb("saveWorkflowWithTrigger — one-step workflow + data trigger", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("wires a source_change subscription when triggers.when is present", async () => {
    await withTestOrg(async (orgId) => {
      await seedDataSource(orgId);
      const result = await saveWorkflowWithTrigger({
        orgId,
        name: "low stock slack alert",
        steps: [{ id: "dm", description: "DM Amit on Slack with the details" }],
        triggers: { when: stockTrigger },
      });

      expect(result.action).toBe("created");
      expect(result.triggerError).toBeUndefined();
      expect(result.subscription).toBeDefined();
      expect(result.subscription?.sourceKind).toBe("source_change");

      const rows = await db()
        .select()
        .from(subscription)
        .where(
          and(
            eq(subscription.org_id, orgId),
            eq(subscription.workflow_id, result.workflow.id),
          ),
        );
      expect(rows).toHaveLength(1);
      expect((rows[0]?.filter as { table?: string }).table).toBe(
        "productinventory",
      );
    });
  });

  it("saves the workflow but reports triggerError when no data source exists", async () => {
    await withTestOrg(async (orgId) => {
      const result = await saveWorkflowWithTrigger({
        orgId,
        name: "orphan trigger",
        steps: [{ id: "s", description: "do" }],
        triggers: { when: stockTrigger },
      });

      expect(result.workflow.name).toBe("orphan trigger");
      expect(result.subscription).toBeUndefined();
      expect(result.triggerError?.code).toBe("no_data_source");

      const rows = await db()
        .select()
        .from(subscription)
        .where(eq(subscription.workflow_id, result.workflow.id));
      expect(rows).toHaveLength(0);
    });
  });

  it("blocks a self-mutating trigger with a mutation_loop triggerError", async () => {
    await withTestOrg(async (orgId) => {
      await seedDataSource(orgId);
      const result = await saveWorkflowWithTrigger({
        orgId,
        name: "self looping",
        // Mentions the watched table + a mutation verb → loop heuristic fires.
        steps: [
          { id: "w", description: "update productinventory to reset the flag" },
        ],
        triggers: { when: stockTrigger },
      });

      expect(result.triggerError?.code).toBe("mutation_loop");
      expect(result.subscription).toBeUndefined();
    });
  });

  it("allows a self-mutating trigger once idempotency_key_template is set", async () => {
    await withTestOrg(async (orgId) => {
      await seedDataSource(orgId);
      const result = await saveWorkflowWithTrigger({
        orgId,
        name: "self looping ok",
        steps: [
          { id: "w", description: "update productinventory to reset the flag" },
        ],
        triggers: {
          when: { ...stockTrigger, idempotency_key_template: "reorder-{primary_key}" },
        },
      });

      expect(result.triggerError).toBeUndefined();
      expect(result.subscription?.idempotencyKeyTemplate).toBe(
        "reorder-{primary_key}",
      );
    });
  });

  it("leaves no subscription when triggers.when is absent", async () => {
    await withTestOrg(async (orgId) => {
      await seedDataSource(orgId);
      const result = await saveWorkflowWithTrigger({
        orgId,
        name: "manual only",
        steps: [{ id: "s", description: "do" }],
      });
      expect(result.subscription).toBeUndefined();
      expect(result.triggerError).toBeUndefined();
    });
  });
});
