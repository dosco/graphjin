import { randomUUID } from "node:crypto";
import { bindStartupRun, failStartupRun, startupEvent, startupPhase, withStartupTrace } from "@neko/telemetry/startup";
import { NextRequest, NextResponse } from "next/server";
import {
  resolveAgentBackend,
  ensureHostConfigProvisioned,
} from "@neko/llm";
import {
  agentRuntimeDepsFromConfig,
  createWorkRun,
  ensureAgentBroker,
  finishWorkRun,
  getWorkRun,
  listPackActionDescriptors,
  registerAgentBrokerEventSink,
  runChatTurn,
} from "@neko/llm/work";
import { createRunSpendGuard, SpendBudgetExceeded } from "@neko/llm/spend";
import { getCurrentActor } from "@/lib/actor";
import { getActionDescriptors } from "@/lib/auth";
import { createCoalescingEmit } from "@/lib/coalescing-emit";
import { getOrgId } from "@/lib/db";
import {
  registerRun,
  unregisterRun,
} from "@/lib/neko-run-registry";
import {
  createWorkMessage,
  setWorkThreadBackendState,
  suggestWorkThreadTitle,
  touchWorkThread,
} from "@/lib/work-store";
import { parseAppWorkContext, parseRecordWorkContext } from "@neko/llm/work";
import {
  AppChatContextError,
  resolveAppChatContext,
} from "@/lib/app-chat-context";
import { getAuthorizedWorkThread } from "@/lib/work-thread-auth";
import { recordsApiError } from "@/lib/records-api";
import { createWebHarnessObserver } from "@/lib/telemetry";
import { errorCodeOf, observeSafely, type HarnessRunSummary } from "@neko/telemetry";

type RouteContext = {
  params: Promise<{ threadId: string }>;
};

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(request: NextRequest, context: RouteContext) {
  const { threadId } = await context.params;
  return withStartupTrace({ requestId: randomUUID(), threadId }, async () => {
    try {
      return await startupPhase("http.submit", async () => {
        const response = await postRun(request, context);
        startupEvent("http.response", { statusCode: response.status });
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
    "openneko.product.path": "work",
    "openneko.job.kind": "work_run",
    "openneko.delivery.channel": "web",
  });
}

async function postRun(request: NextRequest, context: RouteContext) {
  const telemetryStartedAt = Date.now();
  const { threadId } = await context.params;
  const body = await request.json().catch(() => ({}));
  const message = typeof body.message === "string" ? body.message.trim() : "";
  if (!message) {
    return NextResponse.json({ error: "message is required" }, { status: 400 });
  }

  const orgId = await startupPhase("http.organization", () => getOrgId());
  const actor = await startupPhase("http.identity", () => getCurrentActor());
  const thread = await startupPhase("http.authorize", () => getAuthorizedWorkThread(orgId, threadId, actor));
  if (!thread) {
    return NextResponse.json({ error: "Thread not found" }, { status: 404 });
  }

  const backendState = thread.backend_state && typeof thread.backend_state === "object"
    && !Array.isArray(thread.backend_state)
    ? thread.backend_state as Record<string, unknown>
    : {};
  const appContext = parseAppWorkContext(backendState.appContext);
  const legacyRecordContext = parseRecordWorkContext(backendState.recordContext);
  if (body.recordContext !== undefined) {
    const owningAppId = appContext?.appId ?? legacyRecordContext?.appId;
    if (!owningAppId) {
      return NextResponse.json(
        { error: "Page context is only accepted by app chat" },
        { status: 400 },
      );
    }
    try {
      const resolved = await resolveAppChatContext({
        orgId,
        appId: owningAppId,
        actorRole: actor.role,
        recordContext: body.recordContext,
      });
      const nextState = { ...backendState };
      nextState.appContext = resolved.appContext;
      if (resolved.recordContext) nextState.recordContext = resolved.recordContext;
      else delete nextState.recordContext;
      await setWorkThreadBackendState(threadId, nextState);
    } catch (error) {
      if (error instanceof AppChatContextError) {
        return NextResponse.json({ error: error.message }, { status: 400 });
      }
      return recordsApiError(error);
    }
  }

  // Memory writes are agent-driven through the brokered memory MCP tool.
  const backend = await startupPhase("config.backend", () => resolveAgentBackend(orgId));

  // Derive the agent-sandbox env (model egress, gateway provider, key alias)
  // before creating the run. If gateway sync is temporarily unavailable, the
  // memoized provision attempt is cleared and the caller can retry cleanly.
  const agentRuntime = await startupPhase("config.provision", () => ensureHostConfigProvisioned(orgId));

  const [pluginActions, packActions] = await Promise.all([
    startupPhase("context.plugin_actions", () => getActionDescriptors()),
    startupPhase("context.pack_actions", () => listPackActionDescriptors(orgId)),
  ]);

  // The agent loop runs in an OpenShell sandbox (SEC9: the only runtime).
  // The web server stays the control plane, launches the box, and relays
  // events over the existing SSE.
  const broker = await startupPhase("broker.ready", () => ensureAgentBroker());

  let run: Awaited<ReturnType<typeof createWorkRun>>;
  try {
    run = await startupPhase("run.create", () => createWorkRun(orgId, threadId, backend.id, actor, { source: "chat" }));
  } catch (error) {
    if (error instanceof SpendBudgetExceeded) {
      await failPreRun(error);
      return NextResponse.json(
        {
          error: error.message,
          code: error.code,
          budget: error.budget,
          limitUsd: error.limitUsd,
          committedUsd: error.committedUsd,
          reservationUsd: error.reservationUsd,
          retryAfterSeconds: error.retryAfterSeconds,
        },
        { status: 429, headers: { "Retry-After": String(error.retryAfterSeconds) } },
      );
    }
    throw error;
  }
  const runTelemetry = createWebHarnessObserver(run.id);
  const telemetryOperationId = `work:${run.id}`;
  await observeSafely(runTelemetry.observer, {
    kind: "run.start",
    timestamp: new Date(telemetryStartedAt).toISOString(),
    operationId: telemetryOperationId,
    attributes: {
      "openneko.run.kind": "production",
      "openneko.product.path": "work",
      "openneko.job.kind": "work_run",
      "openneko.delivery.channel": "web",
      "openneko.backend": backend.id,
    },
  });

  await bindStartupRun(run.id, runTelemetry.observer);

  if (!thread.title) {
    await touchWorkThread(threadId, { title: suggestWorkThreadTitle(message) });
  }
  await startupPhase("run.save_user_message", () => createWorkMessage({
    orgId,
    threadId,
    runId: run.id,
    role: "user",
    content: message,
  }));

  const abortController = new AbortController();
  registerRun({
    runId: run.id,
    threadId,
    orgId,
    abortController,
    subscribers: new Set(),
  });

  const { emit, finalize } = createCoalescingEmit({
    orgId,
    threadId,
    runId: run.id,
  });

  const spendGuard = await createRunSpendGuard({ runId: run.id, emit, signal: abortController.signal });
  const unregisterBrokerEvents = registerAgentBrokerEventSink(run.id, spendGuard.emit);

  void runChatTurn(
    {
      orgId,
      threadId,
      runId: run.id,
      message,
      channel: "web",
      emit: spendGuard.emit,
      signal: spendGuard.signal,
      pluginActions,
      packActions,
      observer: runTelemetry.observer,
    },
    agentRuntimeDepsFromConfig(agentRuntime, broker),
  )
    .then(async (result) => {
      await observeSafely(runTelemetry.observer, {
        kind: "run.end",
        operationId: telemetryOperationId,
        status:
          result.status === "completed" || result.status === "needs_input"
            ? "ok"
            : "error",
        ...(result.error
          ? { errorType: result.errorCode ?? "work_run_error", errorMessage: result.error }
          : {}),
        ...(result.errorCode ? { errorCode: result.errorCode } : {}),
        attributes: { "openneko.outcome": result.degraded ? "degraded" : result.status },
        measurements: {
          durationMs: Date.now() - telemetryStartedAt,
          coverage: "unavailable",
        },
      });
      await emitTelemetrySafely(emit, runTelemetry.snapshot());
    })
    .catch(async (err) => {
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
          coverage: "unavailable",
        },
      });
      await emitTelemetrySafely(emit, runTelemetry.snapshot());
      console.error(`[work-run/inproc] run ${run.id} threw:`, err);
      try {
        const current = await getWorkRun(orgId, run.id);
        const terminal =
          current?.status === "completed" ||
          current?.status === "failed" ||
          current?.status === "cancelled" ||
          current?.status === "needs_input";
        if (terminal) return;

        const errMsg = err instanceof Error ? err.message : String(err);
        await emit({ type: "error", message: errMsg });
        await emit({ type: "done", result: { status: "failed" } });
        await finishWorkRun(run.id, "failed", errMsg);
      } catch (cleanupErr) {
        console.error(
          `[work-run/inproc] cleanup failed for ${run.id}:`,
          cleanupErr,
        );
      }
    })
    .finally(async () => {
      console.log(
        `[work-run.telemetry] ${JSON.stringify(runTelemetry.snapshot())}`,
      );
      unregisterBrokerEvents();
      spendGuard.dispose();
      try {
        await finalize();
      } catch (err) {
        console.error(
          `[work-run/inproc] finalize failed for ${run.id}:`,
          err,
        );
      }
      unregisterRun(run.id);
    });

  startupEvent("http.ack_ready", { durationMs: Date.now() - telemetryStartedAt, execution: "in_process" });
  return NextResponse.json({
    runId: run.id,
    threadId,
    backend: backend.id,
    actorRole: actor.role,
  });
}

async function emitTelemetrySafely(
  emit: ReturnType<typeof createCoalescingEmit>["emit"],
  summary: HarnessRunSummary,
): Promise<void> {
  try {
    await emit({ type: "telemetry", summary });
  } catch {
    // Summary persistence must not change the work-run outcome.
  }
}
