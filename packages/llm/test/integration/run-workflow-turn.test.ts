import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { afterAll, describe, expect, it } from "vitest";
import { db, eq, pool, work_run, work_thread, workflow_output, workflow_run } from "@neko/db";
import { withTestOrg, dbReachable } from "@neko/db/test-helpers";
import { createWorkThread, listWorkThreads } from "../../src/work/store";
import type {
  AgentBackend,
  AgentEvent,
  AgentRunOptions,
  AgentRunResult,
} from "../../src/agent-backend";
import {
  prepareWorkflowRun,
  runWorkflowTurn,
  saveWorkflow,
  WORKFLOW_API_INPUT_FILE,
} from "../../src/workflows";
import { emitWorkflowOutput } from "../../src/workflows/store";
import { createRunSpendGuard } from "../../src/spend/run-guard";
import { VALUE_ESTIMATE_INSTRUCTIONS } from "../../src/prompts/sections";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn(
    "[run-workflow-turn] skipping: metadata Postgres unreachable.",
  );
}

function fakeBackend(
  onRun: (opts: AgentRunOptions) => Promise<AgentRunResult>,
): AgentBackend {
  return {
    id: "ax",
    capabilities: {
      mcpTools: true,
      sdkStopHook: false,
      sessionResume: false,
      canUseToolGate: true,
    },
    run: onRun,
  };
}

describeIfDb("runWorkflowTurn", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("completes a workflow_run end-to-end, persists summary, and emits message events", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow } = await saveWorkflow({
        orgId,
        name: "smoke test workflow",
        steps: [{ id: "s1", description: "do nothing" }],
      });

      const events: AgentEvent[] = [];
      const emit = async (ev: AgentEvent) => {
        events.push(ev);
      };

      const backend = fakeBackend(async (opts) => {
        expect(opts.prompt).toContain(VALUE_ESTIMATE_INSTRUCTIONS);
        await opts.onEvent?.({
          type: "message",
          role: "assistant",
          content: "All clear.",
        });
        return {
          status: "completed",
          finalText: 'All clear.\n\n```neko_value\n{"minutes_saved":90,"basis":"Cross-checked the report"}\n```',
        };
      });

      const prepared = await prepareWorkflowRun(
        {
          orgId,
          workflowId: workflow.id,
          triggerKind: "manual",
        },
        { resolveAgentBackend: async () => backend },
      );

      const result = await runWorkflowTurn(
        {
          prepared,
          mode: "live",
          emit,
        },
        {
          resolveAgentBackend: async () => backend,
          formatGlobalMemoryPromptContext: async () => "",
        },
      );

      expect(result.status).toBe("completed");
      expect(result.finalText).toBe("All clear.");

      const rows = await db()
        .select()
        .from(workflow_run)
        .where(eq(workflow_run.id, prepared.workflowRun.id));
      expect(rows[0]?.status).toBe("completed");
      expect(rows[0]?.summary).toBe("All clear.");
      const [savedWorkRun] = await db().select().from(work_run)
        .where(eq(work_run.id, prepared.workRunId));
      expect(savedWorkRun?.analysis_minutes_saved).toBe(90);
      expect(savedWorkRun?.analysis_minutes_basis).toBe("Cross-checked the report");
      expect(events).toContainEqual({
        type: "done", result: { status: "completed", minutesSaved: 90 },
      });

      const eventTypes = events.map((e) => e.type);
      expect(eventTypes).toContain("status");
      expect(eventTypes).toContain("message");
      expect(eventTypes).toContain("done");
    });
  });

  it("saves the API input in the run directory before the agent starts", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow } = await saveWorkflow({
        orgId,
        name: "api input workflow",
        steps: [{ id: "s1", description: "read the input" }],
      });
      let saved: unknown = null;
      const backend = fakeBackend(async (opts) => {
        saved = JSON.parse(
          await readFile(join(opts.workspace!.runRoot, WORKFLOW_API_INPUT_FILE), "utf8"),
        );
        return { status: "completed", finalText: "Read it." };
      });
      const prepared = await prepareWorkflowRun(
        { orgId, workflowId: workflow.id, triggerKind: "manual" },
        { resolveAgentBackend: async () => backend },
      );

      await runWorkflowTurn(
        {
          prepared,
          mode: "headless",
          emit: async () => {},
          triggerInput: { accounts: ["A1", "B2"] },
        },
        {
          resolveAgentBackend: async () => backend,
          formatGlobalMemoryPromptContext: async () => "",
        },
      );

      expect(saved).toEqual({ accounts: ["A1", "B2"] });
    });
  });

  it("marks the run failed and surfaces the error when the backend throws", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow } = await saveWorkflow({
        orgId,
        name: "fail test workflow",
        steps: [{ id: "s1", description: "fail" }],
      });

      const backend = fakeBackend(async () => {
        throw new Error("upstream provider down");
      });

      const prepared = await prepareWorkflowRun(
        {
          orgId,
          workflowId: workflow.id,
          triggerKind: "manual",
        },
        { resolveAgentBackend: async () => backend },
      );

      await expect(
        runWorkflowTurn(
          {
            prepared,
            mode: "live",
            emit: async () => {},
          },
          {
            resolveAgentBackend: async () => backend,
            formatGlobalMemoryPromptContext: async () => "",
          },
        ),
      ).rejects.toThrow(/upstream provider down/);

      const rows = await db()
        .select()
        .from(workflow_run)
        .where(eq(workflow_run.id, prepared.workflowRun.id));
      expect(rows[0]?.status).toBe("failed");
      expect(rows[0]?.error).toContain("upstream provider down");
    });
  });

  it("persists a workflow_output row when emitWorkflowOutput is called inside the turn", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow } = await saveWorkflow({
        orgId,
        name: "output test workflow",
        steps: [{ id: "s1", description: "emit a finding" }],
      });

      const prepared = await prepareWorkflowRun(
        {
          orgId,
          workflowId: workflow.id,
          triggerKind: "manual",
        },
        {
          resolveAgentBackend: async () =>
            fakeBackend(async () => ({ status: "completed", finalText: "" })),
        },
      );

      // Simulate the runner emitting an output via the MCP tool — the
      // mechanism the tool exposes is just emitWorkflowOutput.
      const out = await emitWorkflowOutput({
        orgId,
        workflowRunId: prepared.workflowRun.id,
        workRunId: prepared.workRunId,
        kind: "finding",
        title: "fake finding",
        scope: "test_scope",
        mood: "watch",
      });

      const rows = await db()
        .select()
        .from(workflow_output)
        .where(eq(workflow_output.id, out.id));
      expect(rows[0]?.kind).toBe("finding");
      expect(rows[0]?.scope).toBe("test_scope");
      expect(rows[0]?.mood).toBe("watch");
    });
  });

  // A trigger run reuses the work_thread plumbing for its transcript, but it is
  // not a human chat — its thread must never appear in the web Ask sidebar.
  it("creates its thread on the \"workflow\" channel, never \"web\"", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow } = await saveWorkflow({
        orgId,
        name: "low stock slack alert",
        steps: [{ id: "s1", description: "alert on slack" }],
      });

      const prepared = await prepareWorkflowRun(
        { orgId, workflowId: workflow.id, triggerKind: "subscription" },
        {
          resolveAgentBackend: async () =>
            fakeBackend(async () => ({ status: "completed", finalText: "" })),
        },
      );

      const [thread] = await db()
        .select()
        .from(work_thread)
        .where(eq(work_thread.id, prepared.threadId));
      expect(thread?.channel).toBe("workflow");
    });
  });

  it("keeps an orphaned trigger thread (no workflow_run) out of the Ask sidebar", async () => {
    await withTestOrg(async (orgId) => {
      // A genuine human Ask thread, plus a trigger thread whose workflow_run
      // never persisted (a run-row insert that failed mid-trigger) — the leak
      // the web sidebar's NOT EXISTS(workflow_run) filter cannot catch alone.
      const ask = await createWorkThread(orgId, "what were our top products?", "web");
      const orphan = await createWorkThread(orgId, "low stock slack alert", "workflow");

      const webIds = (await listWorkThreads(orgId, "web")).map((t) => t.id);
      expect(webIds).toContain(ask.id);
      expect(webIds).not.toContain(orphan.id);
    });
  });

  // The failure mode behind the "ACP client disposed" run failures: worker
  it("records a spend-cap stop as failed with the cap message, not cancelled", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow } = await saveWorkflow({
        orgId,
        name: "expensive workflow",
        steps: [{ id: "s1", description: "work" }],
      });
      let guard: Awaited<ReturnType<typeof createRunSpendGuard>> | undefined;
      const backend = fakeBackend(async (opts) => {
        await opts.onEvent?.({
          type: "tool_start",
          id: "t1",
          name: "execute",
          input: {},
          usageSnapshot: { estimatedCostUsd: 5.5, costStatus: "estimated", coverage: "complete" },
        });
        return { finalText: "", status: guard?.signal.aborted ? "cancelled" : "completed" };
      });

      const prepared = await prepareWorkflowRun(
        { orgId, workflowId: workflow.id, triggerKind: "cron" },
        { resolveAgentBackend: async () => backend },
      );
      const events: Array<{ type: string; message?: string }> = [];
      guard = await createRunSpendGuard({
        runId: prepared.workRunId,
        emit: async (event) => {
          events.push(event as { type: string; message?: string });
        },
      });

      const result = await runWorkflowTurn(
        { prepared, mode: "live", emit: guard.emit, signal: guard.signal },
        {
          resolveAgentBackend: async () => backend,
          formatGlobalMemoryPromptContext: async () => "",
        },
      );

      const message = "The run exceeded its $5.00 spend cap ($5.50 spent).";
      expect(result).toMatchObject({ status: "failed" });
      expect(events).toContainEqual({ type: "error", message });
      const runs = await db().select().from(workflow_run).where(eq(workflow_run.id, prepared.workflowRun.id));
      expect(runs[0]).toMatchObject({ status: "failed", error: message });
      const workRuns = await db().select().from(work_run).where(eq(work_run.id, prepared.workRunId));
      expect(workRuns[0]).toMatchObject({ status: "failed", error: message });
    });
  });

  // shutdown aborts the run mid-flight. With a signal wired in, that must land
  // as "cancelled" (no error), not a hard failure.
  it("records a shutdown-interrupted run as cancelled, not failed", async () => {
    await withTestOrg(async (orgId) => {
      const { workflow } = await saveWorkflow({
        orgId,
        name: "interruptible workflow",
        steps: [{ id: "s1", description: "work" }],
      });

      // Aborting the signal mid-run simulates cancelAllAgents() on SIGTERM;
      // the backend then surfaces the SIGKILLed child as a disposed-client reject.
      const controller = new AbortController();
      const backend = fakeBackend(async () => {
        controller.abort();
        throw new Error("ACP client disposed before response");
      });

      const prepared = await prepareWorkflowRun(
        { orgId, workflowId: workflow.id, triggerKind: "subscription" },
        { resolveAgentBackend: async () => backend },
      );

      const result = await runWorkflowTurn(
        {
          prepared,
          mode: "live",
          emit: async () => {},
          signal: controller.signal,
        },
        {
          resolveAgentBackend: async () => backend,
          formatGlobalMemoryPromptContext: async () => "",
        },
      );

      expect(result.status).toBe("cancelled");
      const rows = await db()
        .select()
        .from(workflow_run)
        .where(eq(workflow_run.id, prepared.workflowRun.id));
      expect(rows[0]?.status).toBe("cancelled");
      expect(rows[0]?.error).toBeNull();
    });
  });
});
