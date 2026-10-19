import { bindStartupRun, failStartupRun, startupEvent, startupPhase, withStartupTrace } from "@neko/telemetry/startup";
import { randomUUID } from "node:crypto";
import { NextRequest, NextResponse } from "next/server";
import { db, eq, workflow_run } from "@neko/db";
import { ensureHostConfigProvisioned } from "@neko/llm";
import {
  ensureAgentBroker,
  finishWorkRun,
  getWorkRun,
  registerAgentBrokerEventSink,
  workflowRuntimeDepsFromConfig,
} from "@neko/llm/work";
import {
  finishWorkflowRun,
  prepareWorkflowRun,
  runWorkflowTurn,
} from "@neko/llm/workflows";
import { errorCodeOf, observeSafely, type HarnessRunSummary } from "@neko/telemetry";
import { getActionDescriptors } from "@/lib/auth";
import { createCoalescingEmit } from "@/lib/coalescing-emit";
import { getOrgId } from "@/lib/db";
import { requireWorkflow } from "@/lib/entitlements";
import {
  registerRun,
  unregisterRun,
} from "@/lib/neko-run-registry";
import { createWebHarnessObserver } from "@/lib/telemetry";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ workflowId: string }>;
};

export async function POST(request: NextRequest, context: RouteContext) {
  return withStartupTrace({ requestId: randomUUID() }, async () => {
    try {
      return await startupPhase("workflow.http_submit", async () => {
        const response = await postWorkflow(request, context);
        startupEvent("workflow.http_response", { statusCode: response.status });
        return response;
      });
    } catch (error) {
      await failPreRun(error);
      throw error;
    }
  });
}

function failPreRun(error: unknown): Promise<void> {
  return failStartupRun(error, (runId) => createWebHarnessObserver(runId).observer, {
    "openneko.run.kind": "production",
    "openneko.product.path": "workflow",
    "openneko.job.kind": "workflow_manual",
    "openneko.trigger.kind": "manual",
  });
}

async function postWorkflow(request: NextRequest, context: RouteContext) {
  const telemetryStartedAt = Date.now();
  const { workflowId } = await context.params;
  const deniedWorkflow = await requireWorkflow(workflowId);
  if (deniedWorkflow) return deniedWorkflow;
  const body = await request.json().catch(() => ({}));
  const userMessage =
    typeof body.userMessage === "string" ? body.userMessage.trim() : undefined;

  const orgId = await getOrgId();
  const agentRuntime = await startupPhase("config.provision", async () => ensureHostConfigProvisioned(orgId));

  let prepared;
  try {
    prepared = await startupPhase("workflow.prepare", async () => prepareWorkflowRun({
      orgId,
      workflowId,
      triggerKind: "manual",
      triggerPayload: { userMessage: userMessage ?? null },
    }));
  } catch (e) {
    await failPreRun(e);
    const msg = e instanceof Error ? e.message : String(e);
    return NextResponse.json({ error: msg }, { status: 400 });
  }

  const abortController = new AbortController();
  registerRun({
    runId: prepared.workRunId,
    threadId: prepared.threadId,
    orgId,
    abortController,
    subscribers: new Set(),
  });

  const { emit, finalize } = createCoalescingEmit({
    orgId,
    threadId: prepared.threadId,
    runId: prepared.workRunId,
  });

  const runTelemetry = createWebHarnessObserver(prepared.workRunId);
  const telemetryOperationId = `workflow:${prepared.workRunId}`;
  await observeSafely(runTelemetry.observer, {
    kind: "run.start",
    timestamp: new Date(telemetryStartedAt).toISOString(),
    operationId: telemetryOperationId,
    attributes: {
      "openneko.run.kind": "production",
      "openneko.product.path": "workflow",
      "openneko.job.kind": "workflow_manual",
      "openneko.workflow.id": prepared.workflow.id,
      "openneko.workflow_run.id": prepared.workflowRun.id,
      "openneko.trigger.kind": "manual",
    },
    measurements: {
      queueDurationMs: 0,
      attempts: 1,
      coverage: "unavailable",
    },
  });

  await bindStartupRun(prepared.workRunId, runTelemetry.observer, telemetryOperationId);
  startupEvent("workflow.link", { workflowRunId: prepared.workflowRun.id, threadId: prepared.threadId });

  void (async () => {
    let unregisterBrokerEvents: () => void = () => undefined;
    try {
      const pluginActions = await startupPhase("context.plugin_actions", async () => getActionDescriptors());
      const broker = await startupPhase("broker.ready", async () => ensureAgentBroker());
      unregisterBrokerEvents = registerAgentBrokerEventSink(
        prepared.workRunId,
        emit,
      );
      const result = await runWorkflowTurn(
        {
          prepared,
          userMessage,
          mode: "live",
          emit,
          signal: abortController.signal,
          pluginActions,
          observer: runTelemetry.observer,
        },
        workflowRuntimeDepsFromConfig(agentRuntime, broker),
      );
      await observeSafely(runTelemetry.observer, {
        kind: "output.contract",
        operationId: `${telemetryOperationId}:terminal-output`,
        parentOperationId: telemetryOperationId,
        status:
          result.status === "completed" || result.status === "needs_input"
            ? "ok"
            : "error",
        ...(result.error ? { errorType: "workflow_run_error" } : {}),
        attributes: { "openneko.output.kind": "workflow_result" },
      });
      await observeSafely(runTelemetry.observer, {
        kind: "run.end",
        operationId: telemetryOperationId,
        status:
          result.status === "completed" || result.status === "needs_input"
            ? "ok"
            : "error",
        ...(result.error
          ? { errorType: result.errorCode ?? "workflow_run_error", errorMessage: result.error }
          : {}),
        ...(result.errorCode ? { errorCode: result.errorCode } : {}),
        attributes: { "openneko.outcome": result.degraded ? "degraded" : result.status },
        measurements: {
          durationMs: Date.now() - telemetryStartedAt,
          queueDurationMs: 0,
          coverage: "unavailable",
        },
      });
      await persistManualWorkflowTelemetry(
        prepared.workflowRun.id,
        emit,
        runTelemetry.snapshot(),
      );
    } catch (err) {
      await observeSafely(runTelemetry.observer, {
        kind: "run.end",
        operationId: telemetryOperationId,
        status: "error",
        errorType: err instanceof Error ? err.name : "unknown",
        errorCode: errorCodeOf(err),
        errorMessage: err instanceof Error ? err.message : String(err),
        attributes: { "openneko.outcome": "failed" },
        measurements: {
          durationMs: Date.now() - telemetryStartedAt,
          queueDurationMs: 0,
          coverage: "unavailable",
        },
      });
      await persistManualWorkflowTelemetry(
        prepared.workflowRun.id,
        emit,
        runTelemetry.snapshot(),
      );
      console.error(
        `[workflow-run] run ${prepared.workflowRun.id} threw:`,
        err,
      );
      try {
        const current = await getWorkRun(orgId, prepared.workRunId);
        const terminal =
          current?.status === "completed" ||
          current?.status === "failed" ||
          current?.status === "cancelled";
        if (terminal) return;
        const errMsg = err instanceof Error ? err.message : String(err);
        await finishWorkRun(prepared.workRunId, "failed", errMsg);
        await finishWorkflowRun({
          workflowRunId: prepared.workflowRun.id,
          status: "failed",
          error: errMsg,
        });
        await emit({ type: "error", message: errMsg });
        await emit({ type: "done", result: { status: "failed" } });
      } catch (cleanupErr) {
        console.error(
          `[workflow-run] cleanup failed for ${prepared.workflowRun.id}:`,
          cleanupErr,
        );
      }
    } finally {
      console.log(
        `[workflow-run.telemetry] ${JSON.stringify(runTelemetry.snapshot())}`,
      );
      unregisterBrokerEvents();
      try {
        await finalize();
      } catch (err) {
        console.error(
          `[workflow-run] finalize failed for ${prepared.workflowRun.id}:`,
          err,
        );
      }
      unregisterRun(prepared.workRunId);
    }
  })();

  return NextResponse.json({
    workflowRunId: prepared.workflowRun.id,
    workRunId: prepared.workRunId,
    threadId: prepared.threadId,
  });
}

async function persistManualWorkflowTelemetry(
  workflowRunId: string,
  emit: ReturnType<typeof createCoalescingEmit>["emit"],
  summary: HarnessRunSummary,
): Promise<void> {
  try {
    await emit({ type: "telemetry", summary });
  } catch {
    // Run telemetry must remain fail-open.
  }
  try {
    await db()
      .update(workflow_run)
      .set({ telemetry_summary: summary, updated_at: new Date() })
      .where(eq(workflow_run.id, workflowRunId));
  } catch {
    console.warn(
      `[workflow-run.telemetry] failed to persist summary ${workflowRunId}`,
    );
  }
}
