import { startupPhase, startupEvent, withStartupTrace } from "@neko/telemetry/startup";
import { getGraphjinConfigSettingsForOrg, graphjinAgentEnabledForOrg, heldItems } from "@neko/db";
import { filterHeldActions, runAllowedLibrary, runEntitlementActor, runHeldItemIds } from "./entitlement-scope";
import type {
  AgentChatMessage,
  AgentEvent,
  AgentNativeDelegationPolicy,
} from "../agent-backend";
import { resolveAgentBackend as defaultResolveAgentBackend } from "../agent-backend-resolver";
import { extractMemoryFences } from "../agent-backends/memory-fence";
import {
  extractActionRequestFences,
  extractFollowupsFence,
  extractRuleSaveFence,
  extractValueFence,
  extractVitalsFence,
  extractWorkflowSaveFence,
} from "../workflows/fence-parsers";
import { clampAnalysisMinutes } from "../workflows/value";
import {
  handleWorkActionRequest,
  policySavedCard,
  saveWorkflowWithTrigger,
  subscriptionSavedCard,
  upsertActionPolicyByName,
  workflowSavedCard,
  type ActionPolicyMode,
  type ActionScope,
  type RiskLevel,
} from "../workflows";
import {
  knowledgePackPaths,
  prefetchKnowledgeForOrg as defaultPrefetchKnowledgeForOrg,
  readKnowledgePack,
} from "../knowledge-pack";
import {
  formatWorkMemoryPromptContext as defaultFormatWorkMemoryPromptContext,
  effectiveMemoryLayer,
  rememberWorkMemory,
} from "./memory";
import {
  buildOperatorProfileSection,
  getOperatorProfile,
  getWorkRunActor,
  getSoloSandboxUser,
} from "./personas";
import { buildWorkPrompt } from "./prompt";
import { getThreadBot } from "./bot";
import { syncSkillIndex } from "./skill-index";
import { compactIfNeeded, type ThreadCompaction } from "./compact-transcript";
import { discoverRunArtifacts } from "./artifacts";
import {
  finishWorkRun,
  getWorkRunEvents,
  getWorkThreadBundle,
  markWorkRunRunning,
  saveAssistantWorkMessage,
  setWorkRunValue,
  setWorkThreadBackendState,
} from "./store";
import type { GraphjinDataPath, GraphjinMcpToolPolicy } from "./graphjin-tool-policy";
import type { PluginActionDescriptor } from "./tools";
import { createToolOutputRecorder } from "./tool-output/metrics";
import { runAgentBackend } from "./agent-core";
import { createAgentEventTelemetry } from "./agent-event-telemetry";
import { structuredTurnSummary } from "./structured-turn-summary";
import {
  parseAppWorkContext,
  parseRecordWorkContext,
  type WorkDataSurface,
} from "./data-surface";
import {
  inProcessControlPlane,
  type AgentControlPlane,
  type PluginCatalog,
} from "./control-plane";
import {
  ensureWorkWorkspace as defaultEnsureWorkWorkspace,
  listInstalledSkills as defaultListInstalledSkills,
} from "./workspace";
import { errorCodeOf, type HarnessObserver } from "@neko/telemetry";
import { SandboxFailure } from "./sandbox-failure";
import { spendCapFromSignal } from "../spend/run-guard";

/**
 * Delivery channel for a run. "web" renders a2ui cards (the channel injects the
 * render capability); other channels answer in plain markdown and may inject
 * their own render tool later. See docs/PER_CHANNEL_RENDERING.md.
 */
export type RunChannel = "web" | "telegram" | "slack" | (string & {});

export type RunChatTurnOptions = {
  orgId: string;
  threadId: string;
  runId: string;
  message: string;
  /** Delivery channel; defaults to "web". Gates output rendering. */
  channel?: RunChannel;
  /** Efficacy evals disable the work UX metadata blocks. Defaults to true. */
  includeUxMetadata?: boolean;
  emit: (event: AgentEvent) => Promise<void>;
  signal?: AbortSignal;
  /**
   * Plugin action kinds to surface to the agent as MCP tools, one
   * per kind. The worker passes its plugin registry snapshot here;
   * tests pass an empty array to keep the agent's surface stable.
   * Mounted for Hermes through the sandbox MCP bridge.
   */
  pluginActions?: readonly PluginActionDescriptor[];
  /** Installed pack actions. Kept separate from the plugin registry. */
  packActions?: readonly import("./tools").PackActionDescriptor[];
  /**
   * Control-plane impl for the DB-touching MCP tools. Default (undefined)
   * uses the in-process plane; the agent sandbox injects a broker client.
   */
  controlPlane?: AgentControlPlane;
  /** Optional backend-neutral restriction for this run's GraphJin MCP. */
  graphjinToolPolicy?: GraphjinMcpToolPolicy;
  /**
   * Per-run backend-native sub-agent policy. Omitted preserves the production
   * default; parity evals set `disabled` so backend delegation is not a hidden
   * source of extra model calls.
   */
  nativeDelegation?: AgentNativeDelegationPolicy;
  /** Metadata-only observation stream shared by production and eval runs. */
  observer?: HarnessObserver;
};

// Tests can substitute any of these without touching the call site. Production
// callers pass nothing and get the real implementations.
export type RunChatTurnDeps = {
  /** Trusted authorization service hook. Without it, only solo admins reuse sandboxes.
   * Revision must cover sources, packs/plugins, memories and library grants. */
  sandboxAuthorizationRevision?: (input: {
    orgId: string; threadId: string; userId: string; role: string | null;
  }) => Promise<string | null>;
  resolveAgentBackend: typeof defaultResolveAgentBackend;
  ensureWorkWorkspace: typeof defaultEnsureWorkWorkspace;
  formatWorkMemoryPromptContext: typeof defaultFormatWorkMemoryPromptContext;
  prefetchKnowledgeForOrg: typeof defaultPrefetchKnowledgeForOrg;
  listInstalledSkills: typeof defaultListInstalledSkills;
  getThreadBot: typeof getThreadBot;
  syncSkillIndex: typeof syncSkillIndex;
  /**
   * Runs the agent loop. Production hosts inject the OpenShell sandbox
   * impl (agentRuntimeDepsFromConfig) — SEC9: the only runtime. The default
   * (runAgentBackend in-process) exists for tests, which exercise the
   * core without a sandbox. The DB-bound prologue/epilogue stay host-side.
   */
  runCore: typeof runAgentBackend;
};

export type RunChatTurnResult = {
  status: "completed" | "failed" | "cancelled" | "needs_input";
  finalText: string;
  error?: string;
  errorCode?: string;
  degraded?: boolean;
};

function needsInputText(
  event: Extract<AgentEvent, { type: "needs_input" }>,
): string {
  const questions = event.questions?.length
    ? event.questions
    : [
        {
          id: "q1",
          question: event.question,
          options: event.options?.map((label) => ({ label })),
        },
      ];
  const lines = [
    ...(event.reason ? [event.reason, ""] : []),
    ...questions.flatMap((question, index) => [
      `${questions.length > 1 ? `${index + 1}. ` : ""}${question.question}`,
      ...(question.options?.length
        ? question.options.map((option) => `- ${option.label}`)
        : []),
      ...(index < questions.length - 1 ? [""] : []),
    ]),
  ];
  return lines.join("\n").trim();
}

function backendLabel(id: string): string {
  return id === "ax" ? "Ax" : "Hermes";
}

/**
 * Strip any unexecuted ACP/hermes tool-call left in display text — e.g.
 * `call:default_api:read_file{limit:100,path:analyze.py}`. A turn cut short
 * mid-step (step budget, loop) can leave one dangling in finalText; it must
 * never reach a user on any channel. Matches `call:<ns>:<tool>{…}` (closed or
 * truncated at end-of-text). Returns the remaining prose, trimmed.
 */
function stripDanglingToolCalls(text: string): string {
  return text
    .replace(/\bcall:[A-Za-z0-9_.:-]+\s*\{[^{}]*(?:\}|$)/g, "")
    .replace(/[ \t]+\n/g, "\n")
    .trim();
}

function collectStrings(value: unknown, depth = 0): string[] {
  if (depth > 5 || value == null) return [];
  if (typeof value === "string") return [value];
  if (Array.isArray(value)) {
    return value.flatMap((item) => collectStrings(item, depth + 1));
  }
  if (typeof value === "object") {
    return Object.values(value as Record<string, unknown>).flatMap((item) =>
      collectStrings(item, depth + 1),
    );
  }
  return [];
}

/**
 * OpenShell returns policy denials inside tool output JSON. Convert that
 * implementation detail into a channel-neutral event while the exact host is
 * still available. Exported for focused regression tests.
 */
export function extractNetworkPolicyDenial(
  event: AgentEvent,
): Extract<AgentEvent, { type: "capability_denied" }> | null {
  if (event.type !== "tool_end") return null;
  const text = [event.error ?? "", ...collectStrings(event.result)].join("\n");
  if (!/policy_denied|not permitted by policy/i.test(text)) return null;
  const match = text.match(
    /\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+([a-z0-9.-]+)(?::(\d+))?(\/[^\s"'\\]*)?\s+not permitted by policy/i,
  );
  if (!match) return null;
  const port = match[3] ? Number(match[3]) : undefined;
  return {
    type: "capability_denied",
    capability: "network_egress",
    reason: "policy_denied",
    host: match[2].toLowerCase(),
    ...(Number.isFinite(port) ? { port } : {}),
    method: match[1].toUpperCase(),
    ...(match[4] ? { path: match[4] } : {}),
  };
}

export function runChatTurn(opts: RunChatTurnOptions, deps: Partial<RunChatTurnDeps> = {}): Promise<RunChatTurnResult> {
  return withStartupTrace({ runId: opts.runId, threadId: opts.threadId, observer: opts.observer }, () => runChatTurnTraced(opts, deps));
}

async function runChatTurnTraced(
  opts: RunChatTurnOptions,
  deps: Partial<RunChatTurnDeps> = {},
): Promise<RunChatTurnResult> {
  const { orgId, threadId, runId, message, emit, signal } = opts;

  const resolveAgentBackend = deps.resolveAgentBackend ?? defaultResolveAgentBackend;
  const ensureWorkWorkspace = deps.ensureWorkWorkspace ?? defaultEnsureWorkWorkspace;
  const formatWorkMemoryPromptContext =
    deps.formatWorkMemoryPromptContext ?? defaultFormatWorkMemoryPromptContext;
  const prefetchKnowledgeForOrg =
    deps.prefetchKnowledgeForOrg ?? defaultPrefetchKnowledgeForOrg;
  const listInstalledSkills =
    deps.listInstalledSkills ?? defaultListInstalledSkills;
  const runCore = deps.runCore ?? runAgentBackend;

  await startupPhase("run.mark_running", () => markWorkRunRunning(runId));

  const bundle = await startupPhase("run.load_thread", () => getWorkThreadBundle(orgId, threadId));
  if (!bundle) {
    const errMsg = "Thread deleted before run start.";
    await finishWorkRun(runId, "failed", errMsg);
    console.warn(
      `[work-run] thread ${threadId} not found for run ${runId}; marking failed and skipping`,
    );
    return { status: "failed", finalText: "", error: errMsg };
  }
  const appContext = parseAppWorkContext(
    bundle.thread.backendState.appContext,
  );
  const recordContext = parseRecordWorkContext(
    bundle.thread.backendState.recordContext,
  );
  const dataSurface: WorkDataSurface = appContext || recordContext
    ? "records"
    : "customer";
  const threadBot = dataSurface === "customer" && (opts.channel ?? "web") === "web"
    ? await startupPhase("bot.load", () => (deps.getThreadBot ?? getThreadBot)(orgId, threadId))
    : null;

  const backend = await startupPhase("config.backend", () => resolveAgentBackend(orgId));
  const workspace = await startupPhase("workspace.prepare", () => ensureWorkWorkspace(orgId, threadId, runId));

  // An eval's direct-tool policy keeps the direct path.
  const graphjinDataPath: GraphjinDataPath =
    dataSurface === "customer" && !opts.graphjinToolPolicy &&
    await startupPhase("config.graphjin_agent", () => graphjinAgentEnabledForOrg(orgId))
      ? "agent"
      : "direct";

  // Knowledge layering: agentic deployments (auth_mode=jwt) get the slim
  // gj_catalog bootstrap; legacy ones keep the broad discovery dumps.
  if (dataSurface === "customer") {
    const refresh = await startupPhase("knowledge.prefetch", () => prefetchKnowledgeForOrg(orgId, workspace.knowledgeRoot));
    if (!refresh.ok) {
      console.warn(
        `[work-run] org=${orgId} knowledge refresh failed (${refresh.error}); proceeding with on-disk pack`,
      );
    }
  }
  const knowledge = dataSurface === "records"
    ? { mode: "legacy" as const, tables: "{}", namespaces: "{}", insights: "{}", syntax: "{}" }
    : await startupPhase("knowledge.read_pack", () => readKnowledgePack(knowledgePackPaths(workspace.knowledgeRoot)));

  let assistantText = "";
  let turnUsageReported = false;
  let lastUsageSnapshot: Extract<AgentEvent, { type: "tool_start" }>["usageSnapshot"];
  const surfaceMessages: Extract<AgentEvent, { type: "surface" }>["messages"] = [];
  let firstOutputLogged = false;
  let needsInputEvent: Extract<
    AgentEvent,
    { type: "needs_input" }
  > | null = null;
  let needsInputFinished = false;
  const operationId = `work:${runId}`;
  const eventTelemetry = createAgentEventTelemetry({
    observer: opts.observer,
    operationId,
  });
  // Token instrumentation: correlate each tool_end back to its tool_start name
  // and record output size. Flag-gated (OPENNEKO_TOOL_OUTPUT_METRICS) — see
  // work/tool-output/metrics.ts.
  const toolRecorder = createToolOutputRecorder();
  const emittedCapabilityDenials = new Set<string>();
  const wrappedEmit = async (event: AgentEvent): Promise<void> => {
    if (
      needsInputEvent &&
      (event.type === "message" ||
        event.type === "surface" ||
        event.type === "vitals" ||
        event.type === "followups" ||
        event.type === "needs_input")
    ) {
      // The clarification is now the canonical output. A model that ignores
      // the tool's stop instruction cannot append an answer behind the form.
      return;
    }
    if (event.type === "message" && event.role === "assistant") {
      assistantText += event.content;
    }
    if (event.type === "surface") surfaceMessages.push(...event.messages);
    if (event.type === "usage" && event.source === "outer") turnUsageReported = true;
    if (event.type === "tool_start" && event.usageSnapshot) lastUsageSnapshot = event.usageSnapshot;
    if (event.type === "needs_input" && !needsInputEvent) {
      needsInputEvent = event;
    }
    toolRecorder.observe(event);
    await eventTelemetry.observeEvent(event);
    await emit(event);
    if (((event.type === "message" && event.role === "assistant" && event.content) || event.type === "surface") && !firstOutputLogged) {
      firstOutputLogged = true;
      startupEvent("run.first_assistant_output", {});
    }
    const denial = extractNetworkPolicyDenial(event);
    if (denial) {
      const key = `${denial.host}:${denial.port ?? 443}`;
      if (!emittedCapabilityDenials.has(key)) {
        emittedCapabilityDenials.add(key);
        await emit(denial);
      }
    }
  };

  const finishNeedsInput = async (): Promise<RunChatTurnResult> => {
    if (!needsInputEvent) {
      throw new Error(
        "Cannot finish needs_input without a clarification event",
      );
    }
    const finalText = needsInputText(needsInputEvent);
    if (!needsInputFinished) {
      needsInputFinished = true;
      await saveAssistantWorkMessage({
        orgId,
        threadId,
        runId,
        content: finalText,
      });
      await finishWorkRun(runId, "needs_input", null);
      await emit({ type: "done", result: { status: "needs_input" } });
    }
    return { status: "needs_input", finalText };
  };

  // K1 actor drives the brokered GraphJin identity, persona (CV3), and
  // memory layer (CV2). GraphJin credentials never enter the agent sandbox.
  const actor = await startupPhase("identity.resolve", () => getWorkRunActor(runId));

  try {
    if (dataSurface === "customer" && !backend.capabilities.mcpTools) {
      throw new Error(
        "Customer data access requires the native GraphJin broker tool; this backend does not support MCP tools.",
      );
    }
    await wrappedEmit({
      type: "status",
      message: `Starting ${backendLabel(backend.id)}…`,
    });

    // Rendering is a per-channel capability: a channel that can render the
    // a2ui surface gets it (web as cards, telegram as HTML via its plugin
    // projection). Thin channels answer in plain markdown — no rendering
    // vocabulary in their prompt. See docs/PER_CHANNEL_RENDERING.md.
    const RENDERING_CHANNELS = new Set(["web", "telegram"]);
    const customerSurface = dataSurface === "customer";
    const channelWantsCards = RENDERING_CHANNELS.has(opts.channel ?? "web");
    const wantsCards = customerSurface && channelWantsCards;
    const richCards = wantsCards && (opts.channel ?? "web") === "web";
    const supportsCardTool = customerSurface && backend.capabilities.mcpTools;
    const supportsClarificationTool = backend.capabilities.mcpTools;
    const supportsSkillTool = customerSurface && backend.capabilities.mcpTools;
    const supportsMemoryTool = customerSurface && backend.capabilities.mcpTools;
    const supportsWorkflowTool = customerSurface && backend.capabilities.mcpTools;
    const supportsPolicyTool = customerSurface && backend.capabilities.mcpTools;
    const supportsPluginManagerTool =
      customerSurface && backend.capabilities.mcpTools;
    const supportsBotTool = Boolean(threadBot) && backend.capabilities.mcpTools;
    const sourceConfigSettings = dataSurface === "customer"
      ? await startupPhase("config.graphjin", () => getGraphjinConfigSettingsForOrg(orgId))
      : { sourceConfigEnabled: false };
    const supportsSourceConfigTool =
      backend.capabilities.mcpTools &&
      actor.role === "admin" &&
      (opts.channel ?? "web") === "web" &&
      sourceConfigSettings.sourceConfigEnabled;
    const inlineTranscript = !backend.capabilities.sessionResume;

    // Inline-transcript backends grow unbounded on long threads — fold older
    // turns into a rolling summary, keeping the recent tail verbatim.
    let priorSummary: string | undefined;
    let transcriptRows: typeof bundle.messages = bundle.messages;
    let newCompaction: ThreadCompaction | undefined;
    let retainedCompaction: ThreadCompaction | undefined;
    if (inlineTranscript) {
      const prior = (
        bundle.thread.backendState as { compaction?: ThreadCompaction }
      ).compaction;
      retainedCompaction = prior;
      const compacted = await startupPhase("context.compaction", () => compactIfNeeded({
        messages: bundle.messages,
        prior,
        orgId,
        now: new Date().toISOString(),
      }));
      priorSummary = compacted.summary;
      transcriptRows = compacted.kept;
      newCompaction = compacted.newCompaction;
    }

    const messages: AgentChatMessage[] = transcriptRows.map((row) => ({
      id: row.id,
      role: row.role,
      content: row.content,
      runId: row.runId,
      createdAt: row.createdAt,
    }));

    await wrappedEmit({
      type: "status",
      message: "Retrieving relevant context…",
    });

    const runActor = await runEntitlementActor(orgId, actor);
    const [allowedSkills, allowedLibrary, teamMemory, heldPluginActions, heldPackActions] = await startupPhase("identity.entitlements", () =>
      Promise.all([
        runHeldItemIds(runActor, "skill"),
        runAllowedLibrary(runActor),
        heldItems(runActor, "team_memory"),
        filterHeldActions(runActor, opts.pluginActions ?? []),
        filterHeldActions(runActor, opts.packActions ?? []),
      ]));
    const [memoryContext, installedSkills, profile] = await Promise.all([
      customerSurface
        ? startupPhase("context.memory", () => formatWorkMemoryPromptContext(
            {
              orgId,
              threadId,
              runId,
              userId: effectiveMemoryLayer(orgId, actor),
              teamMemory,
            },
            // Use the latest user message as the retrieval query so we pull
            // memories semantically close to what the operator just asked.
            { contextQuery: message, contextLimit: 5 },
          ))
        : Promise.resolve(""),
      startupPhase("context.skills", () => listInstalledSkills(workspace.skillsRoot)).then(async (skills) => {
        // Ax finds skills by meaning, so its index holds every installed skill;
        // search filters to what the actor holds.
        if (customerSurface && backend.id === "ax") {
          await startupPhase("context.skill_index", () => (deps.syncSkillIndex ?? syncSkillIndex)(orgId, skills))
            .catch((error) => console.warn("[work-run] skill index sync failed:", error instanceof Error ? error.message : error));
        }
        return customerSurface
          ? skills.filter((skill) => !allowedSkills || allowedSkills.includes(skill.name))
          : skills.filter((skill) => skill.name === "records");
      }),
      startupPhase("context.persona", () => getOperatorProfile(orgId, actor.userId)),
    ]);
    const operatorProfile = buildOperatorProfileSection(profile);
    let pluginCatalog: PluginCatalog | undefined;
    const mayNeedCapabilityRecovery =
      /\b(integration|plugin|network|egress|live data|external api|weather)\b/i.test(
        message,
      );
    if (
      !supportsPluginManagerTool &&
      actor.role === "admin" &&
      mayNeedCapabilityRecovery
    ) {
      try {
        pluginCatalog = await startupPhase("context.plugin_catalog", () => (opts.controlPlane ?? inProcessControlPlane).listPlugins({ orgId }));
      } catch (error) {
        console.warn(
          `[work-run] marketplace lookup failed: ${error instanceof Error ? error.message : error}`,
        );
      }
    }

    const prompt = buildWorkPrompt({
      backend: backend.id,
      workspace,
      knowledge,
      messages,
      currentUserMessage: message,
      priorSummary,
      memoryContext,
      operatorProfile,
      installedSkills,
      wantsCards,
      includeUxMetadata: opts.includeUxMetadata,
      supportsCardTool,
      richCards,
      supportsSkillTool,
      supportsMemoryTool,
      supportsWorkflowTool,
      supportsPolicyTool,
      supportsSourceConfigTool,
      supportsClarificationTool,
      supportsPluginManagerTool,
      supportsNativeDelegation:
        backend.capabilities.nativeDelegation !== undefined &&
        opts.nativeDelegation !== "disabled",
      pluginCatalog,
      inlineTranscript,
      pluginActions: customerSurface ? heldPluginActions : [],
      dataSurface,
      graphjinDataPath,
      ...(appContext ? { appContext } : {}),
      ...(recordContext ? { recordContext } : {}),
      ...(threadBot ? { bot: threadBot, supportsBotTool } : {}),
    });

    await eventTelemetry.startAgent({
      backend: backend.id,
      ...(backend.model ? { model: backend.model } : {}),
      inputBytes: Buffer.byteLength(`${prompt}\n\n${message}`, "utf8"),
    });
    const authorizationRevision = actor.userId && deps.sandboxAuthorizationRevision
      ? await startupPhase("identity.authorization_revision", () => deps.sandboxAuthorizationRevision!({ orgId, threadId, userId: actor.userId!, role: actor.role }))
      : null;
    const sandboxUser = deps.sandboxAuthorizationRevision
      ? actor.userId && authorizationRevision
        ? { principalId: actor.userId, authorizationRevision }
        : null
      : await startupPhase("identity.sandbox", () => getSoloSandboxUser(orgId, actor));
    const coreResult = await runCore({
      ...(sandboxUser ? { sandboxUser } : {}),
      ...(allowedSkills && customerSurface ? { allowedSkills } : {}),
      ...(allowedLibrary ? { allowedLibrary } : {}),
      backend,
      prompt,
      userMessage: message,
      orgId,
      threadId,
      runId,
      workspace,
      backendState: bundle.thread.backendState,
      pluginActions: customerSurface ? heldPluginActions : [],
      packActions: customerSurface ? heldPackActions : [],
      sourceConfigEnabled: supportsSourceConfigTool,
      botEnabled: supportsBotTool,
      dataSurface,
      ...(opts.graphjinToolPolicy
        ? { graphjinToolPolicy: opts.graphjinToolPolicy }
        : {}),
      ...(graphjinDataPath === "agent" ? { graphjinDataPath } : {}),
      ...(opts.nativeDelegation
        ? { nativeDelegation: opts.nativeDelegation }
        : {}),
      controlPlane: opts.controlPlane ?? inProcessControlPlane,
      // The clarification server can render its deterministic form in any web
      // Work chat, including records-scoped app chat. Generated answer-card
      // vocabulary remains gated separately in the prompt above.
      wantsCards: channelWantsCards,
      cardSchema: richCards ? "rich" : "core",
      emit: wrappedEmit,
      signal,
    });
    if (!turnUsageReported && lastUsageSnapshot) {
      // A sandbox stopped on cancel never reports its turn usage.
      await wrappedEmit({
        type: "usage",
        source: "outer",
        usage: {
          ...lastUsageSnapshot,
          coverage: "partial",
          missingReasons: [
            ...(lastUsageSnapshot.missingReasons ?? []),
            "The turn ended early; usage covers the model calls made before its last tool call.",
          ],
        },
      });
    }
    const spendStop = spendCapFromSignal(signal);
    const result = spendStop
      ? { ...coreResult, status: "failed" as const, error: spendStop.message, errorCode: spendStop.code }
      : coreResult;
    const agentStatus = result.status === "completed" ? "ok" : "error";
    await eventTelemetry.finishAgent({
      status: agentStatus,
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

    const backendStateChanged =
      result.backendState && result.backendState !== bundle.thread.backendState;
    if (backendStateChanged || newCompaction) {
      const base = (
        backendStateChanged ? result.backendState : bundle.thread.backendState
      ) as Record<string, unknown>;
      const protectedBase = {
        ...base,
        ...(appContext ? { appContext } : {}),
        ...(recordContext ? { recordContext } : {}),
      };
      // Transcript compaction is host-owned state. Inline backends commonly
      // return a fresh backend-state object every turn; replacing the stored
      // object must not silently discard an existing rolling summary.
      const compaction = newCompaction ?? retainedCompaction;
      await setWorkThreadBackendState(
        threadId,
        compaction
          ? { ...protectedBase, compaction }
          : protectedBase,
      );
    }

    // Asking is a terminal outcome for this run. The next operator message is
    // a new run in the same thread, with the persisted question + submitted
    // answer in its transcript. Do not parse or emit answer fences after the
    // model has handed control back to the operator.
    if (needsInputEvent) {
      return await finishNeedsInput();
    }

    // OpenShell has pulled the run artifact directory back to the host before
    // runCore resolves. Publish every regular, non-symlink file from that
    // directory as an explicit event; the web download route authorizes only
    // these emitted paths.
    if (result.status === "completed") {
      for (const artifact of await discoverRunArtifacts(workspace)) {
        await wrappedEmit({ type: "artifact", artifact });
      }
    }

    // Hermes /work emits plugin action calls as `neko_action_request`
    // fences (no MCP tool registry to use). Parse them out and route
    // each through the same policy + DB + emit path the MCP tools
    // use, so the agent's tool surface is identical across backends
    // from the user's perspective.
    // Side-effect fences are parsed from the RAW agent output, not finalText:
    // Hermes hides builder fences from finalText (it collapses to the a2ui
    // markdown) and from the message stream, so only rawText still carries
    // them. This falls back to finalText when rawText is absent. Each fence type is parsed independently
    // off the same source — they're distinct delimited blocks.
    const fenceSource =
      (result.rawText ?? result.finalText).trim() || assistantText.trim();
    const actionFences = extractActionRequestFences(fenceSource);
    for (const payload of actionFences.payloads) {
      try {
        await handleWorkActionRequest(
          {
            orgId,
            workRunId: runId,
            threadId,
            emit: wrappedEmit,
          },
          payload,
          payload.summary,
        );
      } catch (err) {
        console.warn(
          `[work-run] handleWorkActionRequest failed: ${err instanceof Error ? err.message : err}`,
        );
      }
    }

    // Workflow / policy save fences are a compatibility fallback. Same chain
    // pattern as action fences above: persist, emit a confirmation
    // surface, strip the fence body from the displayed text.
    const workflowFence = extractWorkflowSaveFence(fenceSource);
    if (workflowFence.payload) {
      try {
        const saved = await saveWorkflowWithTrigger({
          orgId,
          name: workflowFence.payload.name,
          description: workflowFence.payload.description,
          goal: workflowFence.payload.goal,
          systemPromptOverlay: workflowFence.payload.systemPromptOverlay,
          steps: workflowFence.payload.steps,
          batch: workflowFence.payload.batch,
          triggers: workflowFence.payload.triggers,
          createdByThreadId: threadId,
          createdByRunId: runId,
        });
        await wrappedEmit({
          type: "surface",
          messages: workflowSavedCard({
            workflow: saved.workflow,
            action: saved.action,
          }),
        });
        if (saved.subscription) {
          await wrappedEmit({
            type: "surface",
            messages: subscriptionSavedCard({
              subscription: saved.subscription,
              workflowName: saved.workflow.name,
            }),
          });
        } else if (saved.triggerError) {
          await wrappedEmit({
            type: "error",
            message: `workflow saved, but its data trigger was not wired (${saved.triggerError.code}): ${saved.triggerError.message}`,
          });
        }
      } catch (err) {
        await wrappedEmit({
          type: "error",
          message: `workflow save failed: ${err instanceof Error ? err.message : err}`,
        });
      }
    } else if (workflowFence.errors.length > 0) {
      const reasons = workflowFence.errors.map((e) => e.reason).join("; ");
      await wrappedEmit({
        type: "error",
        message: `workflow save fence invalid: ${reasons}`,
      });
    }

    const policyFence = extractRuleSaveFence(fenceSource);
    if (policyFence.payload) {
      try {
        const saved = await upsertActionPolicyByName({
          orgId,
          name: policyFence.payload.name,
          description: policyFence.payload.description ?? "",
          appliesToKinds: policyFence.payload.applies_to_kinds,
          appliesToScopes: policyFence.payload.applies_to_scopes as ActionScope[],
          mode: policyFence.payload.mode as ActionPolicyMode,
          riskThresholdAutoApprove:
            (policyFence.payload.risk_threshold_auto_approve as
              | RiskLevel
              | undefined) ?? null,
          allowedTargets: policyFence.payload.allowed_targets ?? null,
          deniedTargets: policyFence.payload.denied_targets ?? null,
          limits: policyFence.payload.limits,
          approverRole: policyFence.payload.approver_role === "admin" ? "admin" : null,
          priority: policyFence.payload.priority,
          enabled: policyFence.payload.enabled,
          createdByThreadId: threadId,
          createdByRunId: runId,
        });
        await wrappedEmit({
          type: "surface",
          messages: policySavedCard({
            policy: saved.policy,
            action: saved.action,
          }),
        });
      } catch (err) {
        await wrappedEmit({
          type: "error",
          message: `policy save failed: ${err instanceof Error ? err.message : err}`,
        });
      }
    } else if (policyFence.errors.length > 0) {
      const reasons = policyFence.errors.map((e) => e.reason).join("; ");
      await wrappedEmit({
        type: "error",
        message: `policy save fence invalid: ${reasons}`,
      });
    }

    // Per-run analysis value estimate (the human time the answer saved,
    // excluding any actions which carry their own estimate). Parsed from the
    // same raw source, server-clamped, persisted, and echoed in `done` so the
    // UI can show it live. Best-effort: a missing/invalid fence leaves it null.
    const valueFence = extractValueFence(fenceSource);
    const analysisMinutes = clampAnalysisMinutes(valueFence.payload?.minutes_saved);
    if (valueFence.payload) {
      try {
        await setWorkRunValue(runId, {
          minutes: analysisMinutes,
          basis: valueFence.payload.basis ?? null,
        });
      } catch (err) {
        console.warn(
          `[work-run] setWorkRunValue failed: ${err instanceof Error ? err.message : err}`,
        );
      }
    }

    // Suggested follow-up questions — channel-agnostic content, emitted as an
    // event any channel (Ask rail, Telegram, …) can surface as "ask next".
    const followups = extractFollowupsFence(fenceSource);
    if (followups.payload) {
      await wrappedEmit({
        type: "followups",
        items: followups.payload.followups,
      });
    }

    // The answer's headline numbers — channel-agnostic content, emitted as an
    // event each channel renders in the answer that produced them.
    const vitals = extractVitalsFence(fenceSource);
    if (vitals.payload) {
      await wrappedEmit({
        type: "vitals",
        items: vitals.payload.vitals,
      });
    }

    // Pull any neko_memory fences out of the raw agent response and persist
    // them. This is harmless when Hermes used the MCP save tool.
    const { ops: memoryOps } = extractMemoryFences(fenceSource);
    for (const op of memoryOps) {
      try {
        await rememberWorkMemory({
          orgId,
          threadId,
          runId,
          text: op.text,
          kind: "business_rule",
          scope: op.scope ?? "global",
          pinned: op.pinned ?? true,
        });
      } catch (err) {
        console.error(
          "[work-memory] fence-driven save failed:",
          err instanceof Error ? err.message : err,
        );
      }
    }

    // The persisted assistant message is the cleaned DISPLAY text (finalText),
    // with any fence bodies that leaked into it stripped — never the raw
    // source, which still holds the a2ui block + builder fences. The a2ui card
    // and builder confirmation cards were already emitted as surfaces.
    let persistedText = extractActionRequestFences(result.finalText.trim()).text;
    persistedText = extractWorkflowSaveFence(persistedText).text;
    persistedText = extractRuleSaveFence(persistedText).text;
    persistedText = extractValueFence(persistedText).text;
    persistedText = extractFollowupsFence(persistedText).text;
    persistedText = extractVitalsFence(persistedText).text;
    persistedText = extractMemoryFences(persistedText).text;
    // A turn cut short mid-step (budget/loop) can end with an unexecuted
    // tool-call like `call:default_api:read_file{…}` left in the text. Never
    // surface that — strip it so a broken turn degrades to its prose (or empty,
    // which delivery + persistence then skip) instead of leaking a raw call.
    persistedText = stripDanglingToolCalls(persistedText);
    // Structured-only turns used to leave no assistant row at all. The next
    // turn then saw only the user's previous question and lost the result.
    if (!persistedText && result.status === "completed") {
      // Sandboxed MCP tools post their cards through the broker, which
      // persists them without passing through this turn's emit.
      const stored = await getWorkRunEvents(orgId, runId).catch(() => []);
      const streamed = stripDanglingToolCalls(stored
        .map(event => event.type === "message" && event.role === "assistant" ? event.content : "")
        .join(""));
      const interim = stored.flatMap(event => event.type === "interim" ? [stripDanglingToolCalls(event.content)] : []);
      persistedText = structuredTurnSummary(
        vitals.payload,
        [...surfaceMessages, ...stored.flatMap(event => event.type === "surface" ? event.messages : [])],
        [...interim, streamed],
      );
    }
    if (persistedText) {
      await saveAssistantWorkMessage({
        orgId,
        threadId,
        runId,
        content: persistedText,
      });
    }

    // A terminal run is visible to the next turn. Publish that state only
    // after its final assistant context has been saved, including a summary
    // for structured-only answers.
    await finishWorkRun(runId, result.status, result.error ?? null);

    await wrappedEmit({
      type: "done",
      result: { status: result.status, minutesSaved: analysisMinutes ?? 0 },
    });

    return {
      status: result.status,
      // The cleaned display text (machine fences — neko_value/neko_vitals,
      // action/workflow/rule/followups/memory — stripped), never the raw source.
      // Channels deliver this as the converse body; leaking fences render as raw
      // JSON (and break Telegram's HTML parser) on a channel that shows the text.
      finalText: persistedText,
      error: result.error,
      ...(result.errorCode ? { errorCode: result.errorCode } : {}),
      ...(result.degraded ? { degraded: true } : {}),
    };
  } catch (error) {
    if (needsInputEvent) {
      await eventTelemetry.closeOpen({
        status: "ok",
        outcome: "needs_input",
        usageMissingReason: "turn paused for operator input",
      });
      return await finishNeedsInput();
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
          : "Work run failed unexpectedly.";
    await eventTelemetry.closeOpen({
      status: "error",
      outcome: status,
      errorType: aborted
        ? "cancelled"
        : spendStop?.code ?? (error instanceof SandboxFailure ? "sandbox_error" : "agent_backend_error"),
      ...(aborted ? {} : { errorCode: spendStop?.code ?? errorCodeOf(error), errorMessage: errMsg }),
      usageMissingReason: "model call did not complete with normalized usage",
    });
    await wrappedEmit({ type: "error", message: errMsg });
    await finishWorkRun(runId, status, aborted ? null : errMsg);
    await wrappedEmit({ type: "done", result: { status } });
    if (!aborted && !spendStop) throw error;
    return { status, finalText: assistantText, ...(spendStop ? { error: errMsg } : {}) };
  }
}
