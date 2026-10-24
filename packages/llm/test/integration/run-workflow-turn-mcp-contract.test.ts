import { afterAll, describe, expect, it, vi } from "vitest";
import { db, eq, pool, work_run, workflow_run } from "@neko/db";
import { dbReachable, withTestOrg } from "@neko/db/test-helpers";
import type { AgentBackend, AgentEvent } from "../../src/agent-backend";
import {
  prepareWorkflowRun,
  runWorkflowTurn,
  saveWorkflow,
} from "../../src/workflows";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn(
    "[run-workflow-turn-mcp-contract] skipping: metadata Postgres unreachable.",
  );
}

describeIfDb("runWorkflowTurn — native MCP contract", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("fails closed before invoking a backend without native MCP support", async () => {
    await withTestOrg(async (orgId) => {
      const run = vi.fn();
      const backend: AgentBackend = {
        id: "ax",
        capabilities: {
          mcpTools: false,
          sessionResume: false,
        },
        run,
      };
      const { workflow } = await saveWorkflow({
        orgId,
        name: "native MCP contract test",
        steps: [{ id: "s1", description: "read operational data" }],
      });
      const prepared = await prepareWorkflowRun(
        { orgId, workflowId: workflow.id, triggerKind: "manual" },
        { resolveAgentBackend: async () => backend },
      );
      const events: AgentEvent[] = [];

      await expect(
        runWorkflowTurn(
          {
            prepared,
            mode: "headless",
            emit: async (event) => {
              events.push(event);
            },
          },
          {
            resolveAgentBackend: async () => backend,
            formatGlobalMemoryPromptContext: async () => "",
          },
        ),
      ).rejects.toThrow(/requires the native GraphJin broker tool/);

      expect(run).not.toHaveBeenCalled();
      expect(events).toContainEqual(
        expect.objectContaining({
          type: "error",
          message: expect.stringContaining("native GraphJin broker tool"),
        }),
      );
      expect(events).toContainEqual({
        type: "done",
        result: { status: "failed" },
      });

      const [workRun] = await db()
        .select({ status: work_run.status, error: work_run.error })
        .from(work_run)
        .where(eq(work_run.id, prepared.workRunId));
      expect(workRun?.status).toBe("failed");
      expect(workRun?.error).toContain("native GraphJin broker tool");

      const [workflowRun] = await db()
        .select({ status: workflow_run.status, error: workflow_run.error })
        .from(workflow_run)
        .where(eq(workflow_run.id, prepared.workflowRun.id));
      expect(workflowRun?.status).toBe("failed");
      expect(workflowRun?.error).toContain("native GraphJin broker tool");
    });
  });
});
