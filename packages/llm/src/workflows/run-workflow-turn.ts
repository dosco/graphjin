import { startupPhase, withStartupTrace } from "@neko/telemetry/startup";
import { graphjinAgentEnabledForOrg, heldItems, pool, resolveUserGroups } from "@neko/db";
import type { GraphjinDataPath } from "../work/graphjin-tool-policy";
import { filterHeldActions, runAllowedLibrary, runEntitlementActor, runHeldItemIds } from "../work/entitlement-scope";
import { getWorkRunActor } from "../work/personas";
import { workflowTurnBudget, type AgentEvent } from "../agent-backend";
import { errorCodeOf, type HarnessObserver } from "@neko/telemetry";
import { SandboxFailure } from "../work/sandbox-failure";
import { resolveAgentBackend as defaultResolveAgentBackend } from "../agent-backend-resolver";
import {
  knowledgePackPaths,
  readKnowledgePack,
} from "../knowledge-pack";
import { formatGlobalMemoryPromptContext as defaultFormatGlobalMemoryPromptContext } from "../work/memory";
import {
  createWorkRun,
  createWorkThread,
  finishWorkRun,
  markWorkRunRunning,
  saveAssistantWorkMessage,
  setWorkRunValue,
} from "../work/store";
import { ensureWorkWorkspace } from "../work/workspace";
import { discoverRunArtifacts } from "../work/artifacts";
import { botThreadForWorkflow } from "../work/bot";
import { inProcessControlPlane } from "../work/control-plane";
import { createAgentEventTelemetry } from "../work/agent-event-telemetry";
import {
  runWorkflowAgentBackend as defaultRunWorkflowAgentBackend,
} from "./agent-core";
import { extractValueFence } from "./fence-parsers";
import { clampAnalysisMinutes } from "./value";
import {
  buildWorkflowRunnerPrompt,
  buildWorkflowSeedMessage,
  type PluginActionPromptDescriptor,
} from "./runner-prompt";
import {
  createWorkflowRun,
  finishWorkflowRun,
  getWorkflow,
  getWorkflowRun,
  type WorkflowRecord,
  type WorkflowRunRecord,
} from "./store";
import { spendCapFromSignal } from "../spend/run-guard";

export class WorkflowNeedsInputError extends Error {
  constructor(message = "Workflow paused awaiting operator input") {
    super(message);
    this.name = "WorkflowNeedsInputError";
  }
}

export type WorkflowTriggerKind =
  | "manual"
  | "cron"
  | "subscription"
  | "watcher"
  | "api";

export type PrepareWorkflowRunOptions = {
  orgId: string;
  workflowId: string;
  triggerKind: WorkflowTriggerKind;
  triggerPayload?: Record<string, unknown>;
  threadId?: string;
  parentChainDepth?: number;
  triggeredBySubscriptionId?: string | null;
  triggeredByOutputId?: string | null;
  triggeredByObservationId?: string | null;
};

export type PreparedWorkflowRun = {
  workflow: WorkflowRecord;
  workflowRun: WorkflowRunRecord;
  threadId: string;
  workRunId: string;
};

export async function prepareWorkflowRun(
  opts: PrepareWorkflowRunOptions,
  deps: Pick<Partial<RunWorkflowTurnDeps>, "resolveAgentBackend"> = {},
): Promise<PreparedWorkflowRun> {
  const resolveAgentBackend =
    deps.resolveAgentBackend ?? defaultResolveAgentBackend;
  const workflow = await startupPhase("workflow.load", async () => getWorkflow(opts.orgId, opts.workflowId));
  if (!workflow) {
    throw new Error(`Workflow ${opts.workflowId} not found for org ${opts.orgId}.`);
  }
  if (!workflow.enabled) {
    throw new Error(`Workflow ${workflow.name} is disabled.`);
  }
  const backend = await startupPhase("config.backend", async () => resolveAgentBackend(opts.orgId));
  let actor: { userId: string | null; role: "admin" | "member" | "service" } = { userId: null, role: "service" };
  if (workflow.ownerUserId) {
    const owner = await pool().query<{ id: string }>("select id from app_user where org_id=$1 and id=$2 and disabled_at is null", [opts.orgId, workflow.ownerUserId]);
    if (!owner.rows[0]) throw new Error("The workflow owner is no longer active");
    const groups = await resolveUserGroups(opts.orgId, workflow.ownerUserId);
    actor = { userId: workflow.ownerUserId, role: groups.administrator ? "admin" : "member" };
  }
  // A bot's routine runs in the bot's chat, so its reply and files land there.
  // Other trigger threads live on the "workflow" channel, never "web", so they
  // can't surface in the human Ask sidebar — even as an orphan whose work_run
  // never persisted (the sidebar lists only "web" threads).
  const threadId =
    opts.threadId ??
    (await botThreadForWorkflow({
      orgId: opts.orgId,
      createdByThreadId: workflow.createdByThreadId,
      ownerUserId: workflow.ownerUserId,
    })) ??
    (await startupPhase("workflow.create_thread", async () => createWorkThread(opts.orgId, workflow.name, "workflow"))).id;
  const created = await startupPhase("workflow.create_work_run", async () => createWorkRun(opts.orgId, threadId, backend.id, actor, {
    source: opts.triggerKind === "cron" ? "cron" : opts.triggerKind === "api" ? "api" : "trigger",
    workflowId: opts.workflowId,
  }));
  const workflowRun = await startupPhase("workflow.create_run", async () => createWorkflowRun({
    orgId: opts.orgId,
    workflowId: opts.workflowId,
    threadId,
    workRunId: created.id,
    triggerKind: opts.triggerKind,
    triggerPayload: opts.triggerPayload,
    chainDepth:
      (opts.parentChainDepth ?? 0) +
      (opts.triggerKind === "subscription" ? 1 : 0),
    triggeredBySubscriptionId: opts.triggeredBySubscriptionId,
    triggeredByOutputId: opts.triggeredByOutputId,
    triggeredByObservationId: opts.triggeredByObservationId,
  }));
  return {
    workflow,
    workflowRun,
    threadId,
    workRunId: created.id,
  };
}

/** Load the linked rows created transactionally by external API admission. */
export async function loadPreparedWorkflowRun(input: {
  orgId: string;
  workflowId: string;
  workflowRunId: string;
}): Promise<PreparedWorkflowRun> {
  const [workflow, workflowRun] = await Promise.all([
    getWorkflow(input.orgId, input.workflowId),
    getWorkflowRun(input.orgId, input.workflowRunId),
  ]);
  if (
    !workflow ||
    !workflowRun ||
    workflowRun.workflowId !== input.workflowId ||
    workflowRun.triggerKind !== "api"
  ) {
    throw new Error("Admitted workflow API run could not be loaded.");
  }
  return {
    workflow,
    workflowRun,
    threadId: workflowRun.threadId,
    workRunId: workflowRun.workRunId,
  };
}

export type RunWorkflowTurnOptions = {
  prepared: PreparedWorkflowRun;
  /** Efficacy evals disable time-saved metadata. Defaults to true. */
  includeUxMetadata?: boolean;
  userMessage?: string;
  mode: "live" | "headless";
  emit: (event: AgentEvent) => Promise<void>;
  signal?: AbortSignal;
  /** A hard runtime ceiling for the whole run (API runs); disables continuations. */
  timeoutMs?: number;
  /** Tool-call ceiling for the run (API runs). */
  maxToolIterations?: number;
  /** Metadata-only observation stream shared with Ask. */
  observer?: HarnessObserver;
  /**
   * Installed plugin action kinds, so the runner agent proposes real kinds
   * (e.g. send_slack_dm) that policy rules + adapters match — not a generic
   * send_message that stalls at pending_approval. The fire job passes its
   * registry snapshot; tests may omit it.
   */
  pluginActions?: readonly PluginActionPromptDescriptor[];
};

export type RunWorkflowTurnDeps = {
  resolveAgentBackend: typeof defaultResolveAgentBackend;
  formatGlobalMemoryPromptContext: typeof defaultFormatGlobalMemoryPromptContext;
  runCore: typeof defaultRunWorkflowAgentBackend;
};

export type RunWorkflowTurnResult = {
  status:
    | "completed"
    | "failed"
    | "cancelled"
    | "needs_input";
  workflowRunId: string;
  workRunId: string;
  threadId: string;
  finalText: string;
  error?: string;
  errorCode?: string;
  degraded?: boolean;
};

export function runWorkflowTurn(opts: RunWorkflowTurnOptions, deps: Partial<RunWorkflowTurnDeps> = {}): Promise<RunWorkflowTurnResult> {
  return withStartupTrace({ runId: opts.prepared.workRunId, threadId: opts.prepared.threadId, workflowRunId: opts.prepared.workflowRun.id, rootOperationId: `workflow:${opts.prepared.workRunId}`, observer: opts.observer }, () => runWorkflowTurnTraced(opts, deps));
}

async function runWorkflowTurnTraced(
  opts: RunWorkflowTurnOptions,
  deps: Partial<RunWorkflowTurnDeps> = {},
): Promise<RunWorkflowTurnResult> {
  const { prepared, userMessage, mode, emit, signal } = opts;
  const { workflow, workflowRun, threadId, workRunId } = prepared;
  const orgId = workflow.orgId;
  const triggerKind = workflowRun.triggerKind;

  const resolveAgentBackend =
    deps.resolveAgentBackend ?? defaultResolveAgentBackend;
  const formatGlobalMemoryPromptContext =
    deps.formatGlobalMemoryPromptContext ?? defaultFormatGlobalMemoryPromptContext;
  const runCore = deps.runCore ?? defaultRunWorkflowAgentBackend;

  const backend = await startupPhase("config.backend", async () => resolveAgentBackend(orgId));
  await startupPhase("run.mark_running", async () => markWorkRunRunning(workRunId));

  let assistantText = "";
  let needsInput = false;
  const eventTelemetry = createAgentEventTelemetry({
    observer: opts.observer,
    operationId: `workflow:${workRunId}`,
  });
  const wrappedEmit = async (event: AgentEvent): Promise<void> => {
    if (event.type === "message" && event.role === "assistant") {
      assistantText += event.content;
    }
    if (event.type === "needs_input") {
      needsInput = true;
    }
    await eventTelemetry.observeEvent(event);
    await emit(event);
  };

  const workspace = await startupPhase("workspace.prepare", async () => ensureWorkWorkspace(orgId, threadId, workRunId));

  try {
    if (!backend.capabilities.mcpTools) {
      throw new Error(
        "Workflow data access requires the native GraphJin broker tool; this backend does not support MCP tools.",
      );
    }
    await wrappedEmit({
      type: "status",
      message: `Starting workflow "${workflow.name}" (${triggerKind})…`,
    });

    const runActor = await runEntitlementActor(orgId, await getWorkRunActor(workRunId), { workflowId: workflow.id });
    const memoryContext = await startupPhase("context.memory", async () =>
      formatGlobalMemoryPromptContext(orgId, 5, await heldItems(runActor, "team_memory")));

    const graphjinDataPath: GraphjinDataPath =
      await startupPhase("config.graphjin_agent", () => graphjinAgentEnabledForOrg(orgId)) ? "agent" : "direct";
    const knowledge = await startupPhase("knowledge.read_pack", async () => readKnowledgePack(
      knowledgePackPaths(workspace.knowledgeRoot),
    ));

    const prompt = buildWorkflowRunnerPrompt({
      workflow,
      includeUxMetadata: opts.includeUxMetadata,
      mode,
      memoryContext,
      mcpTools: backend.capabilities.mcpTools,
      backend: backend.id,
      workspace,
      knowledge,
      graphjinDataPath,
      pluginActions: await filterHeldActions(runActor, opts.pluginActions ?? []),
    });

    const seedMessage = buildWorkflowSeedMessage({
      workflow,
      triggerKind,
      userMessage,
      triggerPayload: workflowRun.triggerPayload,
    });

    await eventTelemetry.startAgent({
      backend: backend.id,
      ...(backend.model ? { model: backend.model } : {}),
      inputBytes: Buffer.byteLength(`${prompt}\n\n${seedMessage}`, "utf8"),
    });
    const [allowedSkills, allowedLibrary] = await startupPhase("identity.entitlements", () =>
      Promise.all([runHeldItemIds(runActor, "skill"), runAllowedLibrary(runActor)]));
    const budget = workflowTurnBudget();
    const coreResult = await runCore({
      ...(allowedSkills ? { allowedSkills } : {}),
      ...(allowedLibrary ? { allowedLibrary } : {}),
      backend,
      prompt,
      userMessage: seedMessage,
      orgId,
      threadId,
      runId: workRunId,
      workflowRunId: workflowRun.id,
      mode,
      networkHosts: workflow.networkHosts,
      ...(graphjinDataPath === "agent" ? { graphjinDataPath } : {}),
      triggeredByObservationId:
        workflowRun.triggeredByObservationId ?? null,
      workspace,
      controlPlane: inProcessControlPlane,
      emit: wrappedEmit,
      tag: `workflow ${workflow.name} ${workflowRun.id}`,
      signal,
      steps: workflow.steps,
      input: workflowRun.triggerPayload,
      timeoutMs: opts.timeoutMs ?? budget.timeoutMs,
      reasoningEffort: budget.reasoningEffort,
      maxToolIterations: opts.maxToolIterations ?? budget.maxToolIterations,
      maxContinuations: opts.timeoutMs ? 0 : budget.maxContinuations,
    });
    const spendStop = spendCapFromSignal(signal);
    const result = spendStop
      ? { ...coreResult, status: "failed" as const, error: spendStop.message, errorCode: spendStop.code }
      : coreResult;
    await eventTelemetry.finishAgent({
      status: result.status === "completed" ? "ok" : "error",
      ...(result.error
        ? {
            errorType: result.errorCode ?? "agent_backend_error",
            errorMessage: result.error,
          }
        : {}),
      ...(result.errorCode ? { errorCode: result.errorCode } : {}),
      ...(result.timedOut ? { timedOut: true } : {}),
      ...(result.degraded ? { degraded: true } : {}),
      outputBytes: Buffer.byteLength(result.finalText, "utf8"),
    });

    if (needsInput) {
      throw new WorkflowNeedsInputError();
    }

    if (result.status === "completed") {
      for (const artifact of await discoverRunArtifacts(workspace)) {
        await wrappedEmit({ type: "artifact", artifact });
      }
    }

    let persistedText = result.finalText.trim() || assistantText.trim();

    // Per-run analysis value estimate (works for both backends — the
    // `neko_value` fence rides in the agent's final text). Parse, clamp,
    // strip from the persisted text so it never shows in the summary.
    const valueFence = extractValueFence(persistedText);
    persistedText = valueFence.text;
    const analysisMinutes = clampAnalysisMinutes(valueFence.payload?.minutes_saved);

    await finishWorkRun(workRunId, result.status, result.error ?? null);
    if (valueFence.payload) {
      try {
        await setWorkRunValue(workRunId, {
          minutes: analysisMinutes,
          basis: valueFence.payload.basis ?? null,
        });
      } catch (err) {
        console.warn(
          `[workflow-run] setWorkRunValue failed: ${err instanceof Error ? err.message : err}`,
        );
      }
    }
    const summary =
      persistedText.slice(0, 4000) ||
      (result.status === "completed"
        ? "Looked at the data; nothing to flag."
        : null);
    await startupPhase("workflow.persist_result", async () => finishWorkflowRun({
      workflowRunId: workflowRun.id,
      status: result.status,
      summary,
      error: result.error ?? null,
    }));

    if (persistedText) {
      await saveAssistantWorkMessage({
        orgId,
        threadId,
        runId: workRunId,
        content: persistedText,
      });
    }

    await wrappedEmit({
      type: "done",
      result: { status: result.status, minutesSaved: analysisMinutes ?? 0 },
    });

    return {
      status: result.status,
      workflowRunId: workflowRun.id,
      workRunId: workRunId,
      threadId,
      finalText: persistedText,
      error: result.error,
      ...(result.errorCode ? { errorCode: result.errorCode } : {}),
      ...(result.degraded ? { degraded: true } : {}),
    };
  } catch (error) {
    if (error instanceof WorkflowNeedsInputError || needsInput) {
      await eventTelemetry.closeOpen({
        status: "ok",
        outcome: "needs_input",
        usageMissingReason: "workflow paused for operator input",
      });
      await finishWorkRun(workRunId, "failed", null);
      await startupPhase("workflow.persist_result", async () => finishWorkflowRun({
        workflowRunId: workflowRun.id,
        status: "needs_input",
        summary: assistantText.slice(0, 4000) || null,
        error: null,
      }));
      await wrappedEmit({ type: "done", result: { status: "needs_input" } });
      return {
        status: "needs_input",
        workflowRunId: workflowRun.id,
        workRunId: workRunId,
        threadId,
        finalText: assistantText,
      };
    }

    const spendStop = spendCapFromSignal(signal);
    const aborted =
      !spendStop &&
      (signal?.aborted ||
        (error instanceof Error &&
          (error.name === "AbortError" || error.message.includes("aborted"))));
    const status: "failed" | "cancelled" = aborted ? "cancelled" : "failed";
    const errMsg = spendStop
      ? spendStop.message
      : aborted
        ? "Cancelled by user."
        : error instanceof Error
          ? error.message
          : "Workflow run failed unexpectedly.";

    await eventTelemetry.closeOpen({
      status: "error",
      outcome: status,
      errorType: aborted
        ? "cancelled"
        : spendStop?.code ?? (error instanceof SandboxFailure ? "sandbox_error" : "agent_backend_error"),
      ...(aborted ? {} : { errorCode: spendStop?.code ?? errorCodeOf(error), errorMessage: errMsg }),
      usageMissingReason: "workflow model call did not complete with normalized usage",
    });

    await wrappedEmit({ type: "error", message: errMsg });
    await finishWorkRun(workRunId, status, aborted ? null : errMsg);
    await startupPhase("workflow.persist_result", async () => finishWorkflowRun({
      workflowRunId: workflowRun.id,
      status,
      summary: assistantText.slice(0, 4000) || null,
      error: aborted ? null : errMsg,
    }));
    await wrappedEmit({ type: "done", result: { status } });
    if (!aborted && !spendStop) throw error;
    return {
      status,
      ...(spendStop ? { error: errMsg } : {}),
      workflowRunId: workflowRun.id,
      workRunId: workRunId,
      threadId,
      finalText: assistantText,
    };
  }
}
