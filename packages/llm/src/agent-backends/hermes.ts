// Always session/new per turn — Hermes session/load replays history that the prompt already carries (see packages/llm/src/work/prompt.ts), double-counting context.

import { nekoProviderFromHermes } from "../provider-runtime";
import { spawn, type ChildProcess } from "node:child_process";
import { readFileSync } from "node:fs";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { parse as parseYaml } from "yaml";
import { agentLimits, hermesLimitEnv } from "../agent-limits";
import {
  agentTurnTimeoutMs,
  type AgentBackend,
  type AgentEvent,
  type AgentModelIdentity,
  type AgentNativeDelegationPolicy,
  type AgentRunOptions,
  type AgentRunResult,
  type AgentTokenUsage,
} from "../agent-backend";
import {
  HERMES_NATIVE_DELEGATION_DISABLED,
  HERMES_NATIVE_DELEGATION_ENV,
} from "../agent-runtime-contract";
import { registerAgentCanceller } from "../agent-shutdown";
import { hermesHomeForOrg } from "../hermes-home";
import {
  A2UI_RENDER_ACP_TITLE,
  A2UI_RENDER_SERVER_NAME,
  A2UI_RENDER_TOOL_NAME,
  validateRenderCardsInput,
} from "../work/a2ui-contract";
import {
  AcpProtocolError,
  createAcpClient,
  type AcpClient,
  type AcpNotification,
  type AcpSessionUpdate,
} from "./hermes-acp-client";
import { extractMarkdownText, outsideFenceText } from "./output";
import { extractSurfaceMessages } from "./surface";

export { extractSurfaceMessages } from "./surface";
export { outsideFenceText, parseJsonFromOutput } from "./output";

export type ProviderSummarySource = "google-gemini" | "anthropic";

function canonicalAcpMcpToolName(title: unknown): string | null {
  if (typeof title !== "string") return null;
  const trimmed = title.trim();
  if (!/^mcp_+/u.test(trimmed)) return null;
  // ACP clients may render the same MCP identity as either
  // mcp_server_tool or mcp__server__tool. AgentEvent consumers need one
  // stable, precise name instead of the coarse ACP kind (`other`).
  return trimmed.replace(/_+/gu, "_");
}

/**
 * ACP calls every provider reasoning stream an `agent_thought_chunk`, so the
 * generated per-org config is the trust boundary. Only providers whose native
 * API explicitly defines these parts as user-visible summaries are exposed:
 * Gemini includeThoughts and Anthropic thinking.display="summarized".
 */
export function providerThoughtSummarySource(
  hermesHome: string | undefined,
): ProviderSummarySource | null {
  if (!hermesHome) return null;
  try {
    const config = parseYaml(
      readFileSync(join(hermesHome, "config.yaml"), "utf8"),
    ) as {
      model?: { default?: unknown; provider?: unknown };
      agent?: { reasoning_effort?: unknown };
    } | null;
    const provider = String(config?.model?.provider ?? "").trim().toLowerCase();
    const model = String(config?.model?.default ?? "").trim().toLowerCase();
    const effort = config?.agent?.reasoning_effort;
    const disabled =
      effort === false ||
      ["none", "off", "false", "no", "0"].includes(
        String(effort ?? "").trim().toLowerCase(),
      );
    if (
      provider === "gemini" &&
      model.startsWith("gemini") &&
      effort !== undefined &&
      !disabled
    ) {
      return "google-gemini";
    }
    if (
      provider === "anthropic" &&
      model.startsWith("claude-") &&
      effort !== undefined &&
      !disabled
    ) {
      return "anthropic";
    }
    return null;
  } catch {
    return null;
  }
}

/** Last error-ish lines of hermes' own agent.log — the file dies with the
 *  sandbox, so a mid-turn death must read it NOW or never. */
function hermesAgentLogTail(hermesHome: string | undefined): string {
  if (!hermesHome) return "";
  try {
    const lines = readFileSync(join(hermesHome, "logs", "agent.log"), "utf8")
      .split("\n")
      .filter(
        (l) =>
          l.trim() &&
          !l.includes("Prompt on session") &&
          !l.includes("conversation turn:"),
      );
    const errs = lines.filter((l) =>
      /ERROR|CRITICAL|Traceback|Unhandled|fatal|Killed|Segmentation/i.test(l),
    );
    return (errs.length ? errs : lines).slice(-8).join("\n").slice(-900);
  } catch {
    return "";
  }
}

/** MemAvailable from /proc/meminfo (Linux only) — distinguishes a memory-
 *  pressure kill from a crash without a debug rerun. */
function memAvailable(): string {
  try {
    const m = /MemAvailable:\s+(\d+) kB/.exec(
      readFileSync("/proc/meminfo", "utf8"),
    );
    return m ? `${Math.round(Number(m[1]) / 1024)}MiB` : "";
  } catch {
    return "";
  }
}

function killProcessGroup(child: ChildProcess, signal: NodeJS.Signals): void {
  if (!child.pid) return;
  try {
    process.kill(-child.pid, signal);
  } catch {
    // ESRCH if already exited
  }
  // Group kill fails when child isn't a group leader (e.g. test mocks); send to child too — idempotent for SIGTERM/SIGKILL.
  try {
    child.kill(signal);
  } catch {
    // ignore
  }
}

const HERMES_RESPONSE_TRUNCATED_SENTINEL =
  "Response truncated due to output length limit";

const DEFAULT_TIMEOUT_MS = agentTurnTimeoutMs();

export class HermesBackend implements AgentBackend {
  readonly id = "hermes" as const;
  readonly configuredIdentity?: AgentModelIdentity;
  readonly model?: string;
  readonly capabilities = {
    // ACP mounts MCP servers as stdio children (session/new mcpServers) — the
    // neko servers ride a bridge process (OPENNEKO_MCP_BRIDGE) that rebuilds
    // each one over the broker control plane. Surfaces still come via fence
    // (see surface.ts).
    mcpTools: true,
    sessionResume: false,
    nativeDelegation: "hermes-delegate-task",
  } as const;

  constructor(configuredIdentity?: AgentModelIdentity) {
    this.configuredIdentity = configuredIdentity;
    this.model = configuredIdentity?.model;
  }

  async run(opts: AgentRunOptions): Promise<AgentRunResult> {
    const {
      prompt,
      userMessage,
      timeoutMs = DEFAULT_TIMEOUT_MS,
      retries = 1,
      debug = false,
      tag,
      orgId,
      workspace,
      skills: _skills,
      signal,
      onEvent,
      wantsCards = false,
      backendState = {},
      nativeDelegation = "enabled",
      reasoningEffort,
      maxToolIterations,
    } = opts;

    const fullPrompt = userMessage
      ? `${prompt}\n\nCurrent user message:\n${userMessage}`
      : prompt;

    // Streaming turns normally cannot be retried because replaying tools can
    // repeat side effects. runOnce permits a retry only with no tool activity
    // or when all observed calls were read-only discovery tools.
    const maxAttempts = retries + 1;
    let lastErr: Error | undefined;
    let lastCode: string | undefined;

    for (let attempt = 0; attempt < maxAttempts; attempt++) {
      try {
        const out = await runOnce({
          prompt: fullPrompt,
          timeoutMs,
          debug,
          tag,
          orgId,
          workspace,
          signal,
          onEvent,
          wantsCards,
          configuredIdentity: this.configuredIdentity,
          nativeDelegation,
          reasoningEffort,
          maxToolIterations,
          mcpServerNames: Object.keys(opts.mcpServers ?? {}),
          mcpBridgeEnv: opts.mcpBridgeEnv,
        });
        if (out.error) {
          lastErr = new Error(out.error);
          lastCode = out.errorCode;
          if (debug) {
            console.warn(
              `[hermes] attempt ${attempt + 1}/${maxAttempts} failed: ${out.error}`,
            );
          }
          if (onEvent && !out.retryable) break;
          if (onEvent && attempt + 1 < maxAttempts) {
            await onEvent({ type: "retry", reason: out.errorCode ?? "empty_output" });
            await onEvent({
              type: "status",
              message: "Hermes returned no answer; retrying…",
            });
          }
          continue;
        }
        return {
          finalText: out.finalText,
          rawText: out.rawText,
          status: signal?.aborted ? "cancelled" : "completed",
          backendState,
        };
      } catch (e) {
        lastErr = e instanceof Error ? e : new Error(String(e));
        lastCode = thrownErrorCode(lastErr);
        if (debug) {
          console.warn(
            `[hermes] attempt ${attempt + 1}/${maxAttempts} failed: ${lastErr.message}`,
          );
        }
        if (onEvent) break;
      }
    }

    const message = lastErr?.message ?? "hermes: unknown failure";
    if (signal?.aborted) {
      return { finalText: "", status: "cancelled", backendState };
    }
    if (onEvent) {
      await onEvent({ type: "error", message });
    }
    return {
      finalText: "",
      status: "failed",
      backendState,
      error: message,
      ...(lastCode ? { errorCode: lastCode } : {}),
      ...(message.startsWith(HERMES_TURN_TIMEOUT_PREFIX) ? { timedOut: true } : {}),
    };
  }
}

const HERMES_TURN_TIMEOUT_PREFIX = "hermes turn exceeded its ";

function thrownErrorCode(error: Error): string | undefined {
  if (error.message.startsWith(HERMES_TURN_TIMEOUT_PREFIX)) return "timeout";
  if (error instanceof AcpProtocolError) return "protocol";
  if (/^hermes (?:exited mid-turn|spawn failed)/.test(error.message)) return "exit";
  return undefined;
}

type RunOnceArgs = {
  prompt: string;
  timeoutMs: number;
  debug: boolean;
  tag: string | undefined;
  orgId: string | undefined;
  workspace: AgentRunOptions["workspace"];
  signal: AbortSignal | undefined;
  onEvent: AgentRunOptions["onEvent"];
  wantsCards: boolean;
  configuredIdentity: AgentModelIdentity | undefined;
  nativeDelegation: AgentNativeDelegationPolicy;
  reasoningEffort: AgentRunOptions["reasoningEffort"];
  maxToolIterations: number | undefined;
  mcpServerNames: string[];
  mcpBridgeEnv: Record<string, string> | undefined;
};

type RunOnceOutcome = {
  finalText: string;
  rawText?: string;
  error?: string;
  errorCode?: string;
  retryable?: boolean;
};

async function runOnce(args: RunOnceArgs): Promise<RunOnceOutcome> {
  const {
    prompt,
    timeoutMs,
    debug,
    tag,
    orgId,
    workspace,
    signal,
    onEvent,
    wantsCards,
    configuredIdentity,
    nativeDelegation,
    reasoningEffort,
    maxToolIterations,
    mcpServerNames,
    mcpBridgeEnv,
  } = args;

  // The latest usage Hermes reported for this turn. A turn that ends early
  // still reports it, so the model calls it already paid for reach the ledger.
  let turnUsage: AgentTokenUsage | undefined;
  let turnUsageFinal = false;
  let turnUsageReported = false;
  let turnIdentity: AgentModelIdentity | undefined;
  let drainEvents = async (): Promise<void> => {};

  let cwd: string;
  let cleanupScratch: (() => Promise<void>) | undefined;
  if (workspace) {
    cwd = workspace.orgRoot;
  } else {
    const safeTag = tag?.replace(/[^a-zA-Z0-9_-]/g, "").slice(0, 64);
    cwd = safeTag
      ? await (async () => {
          const exact = join(tmpdir(), safeTag);
          try {
            await mkdir(exact);
            return exact;
          } catch (e) {
            if ((e as NodeJS.ErrnoException).code !== "EEXIST") throw e;
            return mkdtemp(join(tmpdir(), `${safeTag}-`));
          }
        })()
      : await mkdtemp(join(tmpdir(), "neko-hermes-"));
    cleanupScratch = () =>
      rm(cwd, { recursive: true, force: true }).catch(() => {});
  }

  const env: NodeJS.ProcessEnv = workspace
    ? {
        ...process.env,
        PATH: `${workspace.binRoot}:${process.env.PATH || ""}`,
      }
    : { ...process.env };
  // The model-facing Hermes process never needs the broker bearer token.
  // MCP bridge children receive it explicitly in their clean per-server env
  // below; removing it here prevents terminal tools from bypassing the
  // declared MCP capability surface with a hand-written HTTP request.
  delete env.OPENNEKO_BROKER_URL;
  delete env.OPENNEKO_BROKER_TOKEN;
  // This is a process-local policy. Clear any ambient value first so normal
  // production turns retain Hermes' native delegation unless the caller
  // explicitly disables it for this run (for example, backend parity evals).
  delete env[HERMES_NATIVE_DELEGATION_ENV];
  if (nativeDelegation === "disabled") {
    env[HERMES_NATIVE_DELEGATION_ENV] = HERMES_NATIVE_DELEGATION_DISABLED;
  }
  // Read by scripts/patches/hermes-acp-run-budget.patch.
  Object.assign(env, hermesLimitEnv(agentLimits()));
  delete env.OPENNEKO_HERMES_REASONING_EFFORT;
  delete env.OPENNEKO_HERMES_MAX_ITERATIONS;
  if (reasoningEffort) env.OPENNEKO_HERMES_REASONING_EFFORT = reasoningEffort;
  if (maxToolIterations && maxToolIterations > 0) {
    env.OPENNEKO_HERMES_MAX_ITERATIONS = String(Math.floor(maxToolIterations));
  }
  if (orgId) {
    env.HERMES_HOME = hermesHomeForOrg(orgId);
  }
  const providerSummarySource = providerThoughtSummarySource(
    env.HERMES_HOME,
  );
  // A hard native crash (SIGSEGV/SIGABRT in compiled deps) dies without a
  // Python traceback — faulthandler makes it dump one to stderr, which the
  // mid-turn death message below surfaces.
  env.PYTHONFAULTHANDLER = "1";

  // OpenNeko owns the safety boundary here: the agent runs with the per-run
  // graphjin guard first on PATH and any MCP tools are mounted explicitly.
  // Hermes' internal dangerous-command approval bridge is not stable across
  // releases (v2026.5 passes allow_permanent to an older ACP callback), so
  // bypass it instead of letting headless jobs hang on approval prompts.
  env.HERMES_YOLO_MODE = "1";
  const warm = process.env.OPENNEKO_HERMES_WARM === "1";
  const child = spawn(warm ? "/usr/local/uv/tools/hermes-agent/bin/python" : "hermes",
    warm ? ["/app/hermes-warm.py", "client"] : ["--yolo", "acp"], {
    stdio: ["pipe", "pipe", "pipe"],
    cwd,
    env,
    detached: true,
  });
  const tagSuffix = tag ? ` tag=${tag}` : "";

  // Hermes' ACP adapter logs at INFO to stderr — including a "Prompt on
  // session <id>: <full system prompt>" line. Forwarding that as status
  // events leaks the system prompt into the UI. Useful progress pills
  // ("Queued for…", "Loading skills…") are emitted explicitly by the API
  // route and the worker; the agent-side notifications stream covers the
  // rest. So we just buffer stderr for debug + crash dumps.
  const stderrChunks: Buffer[] = [];
  child.stderr?.on("data", (c: Buffer) => {
    stderrChunks.push(c);
    if (debug) process.stderr.write(c);
  });

  const unregister = registerAgentCanceller(() => killProcessGroup(child, "SIGKILL"));
  const onAbort = () => killProcessGroup(child, "SIGTERM");
  if (signal) {
    if (signal.aborted) onAbort();
    else signal.addEventListener("abort", onAbort, { once: true });
  }

  let timer: NodeJS.Timeout | null = null;
  let timedOut = false;
  let spawnError: Error | undefined;
  child.on("error", (e) => {
    spawnError = new Error(`hermes spawn failed: ${e.message}`);
  });
  const closedPromise = new Promise<{ code: number | null }>((resolve) => {
    child.on("close", (code) => resolve({ code }));
  });
  // The disposed-client error can outrun the child's exit event, in which
  // case exitCode/signalCode still read null — the death certificate must
  // wait for the real values (a SIGSEGV/SIGKILL is the whole diagnosis).
  const exitInfoPromise = new Promise<{
    code: number | null;
    signal: NodeJS.Signals | null;
  }>((resolve) => {
    child.on("exit", (code, sig) => resolve({ code, signal: sig }));
  });

  const cleanup = async () => {
    if (timer) clearTimeout(timer);
    if (signal) signal.removeEventListener("abort", onAbort);
    unregister();
    await cleanupScratch?.();
  };

  let client: AcpClient | undefined;
  try {
    client = createAcpClient(child);

    timer = setTimeout(() => {
      timedOut = true;
      killProcessGroup(child, "SIGTERM");
    }, timeoutMs);

    await client.request("initialize", {
      protocolVersion: 1,
      clientCapabilities: { fs: { readTextFile: false, writeTextFile: false } },
    });

    // session/new per turn — see file header for the session/load double-count rationale.
    // Web turns get the render_cards MCP server so the model renders cards.
    // The neko tool servers mount as stdio bridge children when a bridge entry
    // is available (in-box: entry.ts sets OPENNEKO_MCP_BRIDGE; broker coords
    // ride the process env down to each child).
    const bridgePath = process.env.OPENNEKO_MCP_BRIDGE;
    const bridgeEnv = [
      ...Object.entries(mcpBridgeEnv ?? {}),
      // Hermes spawns MCP children with a CLEAN env + this list — nothing
      // inherits. Without the proxy vars the child dials direct and the
      // sandbox firewall refuses; broker traffic must ride the egress proxy.
      ...[
        "OPENNEKO_BROKER_URL",
        "OPENNEKO_BROKER_TOKEN",
        "ALL_PROXY",
        "HTTP_PROXY",
        "HTTPS_PROXY",
        "NO_PROXY",
        "no_proxy",
        "NODE_USE_ENV_PROXY",
      ]
        .map((k) => [k, process.env[k] ?? ""])
        .filter(([, v]) => v),
    ].map(([name, value]) => ({ name, value }));
    const safeBridgeNames = mcpServerNames
      // names are our map keys — the regex guards the shell interpolation.
      .filter(
        (name) =>
          /^neko_[a-z0-9_]+$/.test(name) &&
          (name !== A2UI_RENDER_SERVER_NAME || wantsCards),
      );
    const bridgeServers =
      bridgePath && mcpBridgeEnv
        ? safeBridgeNames.length > 0
          ? [{
              // The bridge multiplexes all logical OpenNeko MCP servers in
              // one Node process while preserving mcp_neko_* tool names.
              name: "neko",
              // cd /app first: hermes spawns MCP children with the session
              // cwd (/sandbox/…), where `--import tsx/esm` cannot resolve.
              // A plain-JS bundle skips the tsx loader entirely.
              command: "/bin/sh",
              args: [
                "-c",
                bridgePath.endsWith(".ts")
                  ? `cd /app && exec node --import tsx/esm ${bridgePath} ${safeBridgeNames.join(",")}`
                  : `cd /app && exec node ${bridgePath} ${safeBridgeNames.join(",")}`,
              ],
              env: bridgeEnv,
            }]
          : []
        : [];
    const mcpServers = bridgeServers;
    const fresh = await client.request<{
      sessionId: string;
      models?: unknown;
    }>("session/new", {
      cwd,
      mcpServers,
    });
    const sessionId = fresh.sessionId;
    const reported = parseHermesSessionIdentity(fresh);
    const observedIdentity = reported
      ? { ...reported, provider: nekoProviderFromHermes(reported.provider, configuredIdentity) }
      : undefined;
    turnIdentity = observedIdentity;

    let accumulatedText = "";
    let emittedOutsideLen = 0;
    let surfaceEmittedDuringStream = false;
    let toolActivityObserved = false;
    let unsafeToolActivityObserved = false;
    const discoveryToolCallIds = new Set<string>();
    const pendingValidRenderToolCalls = new Map<string, unknown>();
    let pendingProviderSummary = "";
    let providerSummarySequence = 0;
    let eventQueue = Promise.resolve();
    let eventError: Error | undefined;
    const emitQueued = (event: AgentEvent): void => {
      if (!onEvent) return;
      eventQueue = eventQueue.then(async () => {
        try {
          await onEvent(event);
        } catch (error) {
          eventError ??= error instanceof Error ? error : new Error(String(error));
        }
      });
    };
    drainEvents = () => eventQueue;
    const flushProviderSummary = (): void => {
      const content = pendingProviderSummary.trim();
      pendingProviderSummary = "";
      if (!content || !onEvent) return;
      providerSummarySequence += 1;
      emitQueued({
        type: "progress",
        id: `${providerSummarySource}-summary-${sessionId}-${providerSummarySequence}`,
        content,
        source: "provider_summary",
        provider: providerSummarySource!,
      });
    };
    let interimSequence = 0;
    const interimMeta = (update: AcpSessionUpdate): { alreadyStreamed: boolean } | null => {
      if (update.sessionUpdate !== "agent_message_chunk") return null;
      const meta = (update.fieldMeta ?? update._meta) as { hermes?: { interim?: unknown; already_streamed?: unknown } } | undefined;
      const marker = meta?.hermes;
      return marker?.interim === true
        ? { alreadyStreamed: marker.already_streamed === true }
        : null;
    };

    client.onNotification((notif) => {
      const update = notif.update;
      switch (update.sessionUpdate) {
        case "agent_message_chunk": {
          const text = update.content?.text ?? "";
          if (!text) return;
          const interim = interimMeta(update);
          if (interim) {
            if (!interim.alreadyStreamed && onEvent) {
              interimSequence += 1;
              emitQueued({
                type: "interim",
                id: `hermes-interim-${sessionId}-${interimSequence}`,
                content: text,
                source: "hermes_interim_assistant",
              });
            }
            return;
          }
          flushProviderSummary();
          accumulatedText += text;
          if (onEvent) {
            const trimmed = accumulatedText.trim();
            if (
              trimmed.length > 0 &&
              HERMES_RESPONSE_TRUNCATED_SENTINEL.startsWith(trimmed)
            ) {
              return;
            }
            const outside = outsideFenceText(accumulatedText);
            const delta = outside.slice(emittedOutsideLen);
            if (delta) {
              emittedOutsideLen = outside.length;
              emitQueued({ type: "message", role: "assistant", content: delta });
            }
          }
          return;
        }
        case "agent_thought_chunk": {
          if (providerSummarySource) {
            const remaining = 6_000 - pendingProviderSummary.length;
            if (remaining > 0) {
              pendingProviderSummary += (update.content?.text ?? "").slice(
                0,
                remaining,
              );
            }
          }
          return;
        }
        case "tool_call": {
          toolActivityObserved = true;
          if (update.title === "tool_describe" || update.title?.startsWith("search:")) {
            discoveryToolCallIds.add(update.toolCallId);
          } else {
            unsafeToolActivityObserved = true;
          }
          flushProviderSummary();
          const meta = (update.fieldMeta ?? update._meta) as { openneko?: { usage?: unknown } } | undefined;
          const usageSnapshot = normalizeHermesUsage(meta?.openneko?.usage);
          if (usageSnapshot && !turnUsageFinal) turnUsage = usageSnapshot;
          if (!onEvent) return;
          const mcpToolName = canonicalAcpMcpToolName(update.title);
          // The brokered neko_ui server is the sole surface emitter. Suppress
          // a successful render's tool pill, but preserve the exact rejected
          // input as tool_start telemetry so envelope failures are diagnosable.
          if (mcpToolName === A2UI_RENDER_ACP_TITLE) {
            const validation = validateRenderCardsInput(update.rawInput);
            if (validation.success) {
              pendingValidRenderToolCalls.set(update.toolCallId, update.rawInput);
              return;
            }
            emitQueued({
              type: "tool_start",
              id: update.toolCallId,
              ...(usageSnapshot ? { usageSnapshot } : {}),
              name: A2UI_RENDER_TOOL_NAME,
              input: {
                title: update.title,
                rawInput: update.rawInput,
                validationIssues: validation.issues,
              },
            });
            return;
          }
          emitQueued({
            type: "tool_start",
            id: update.toolCallId,
            ...(usageSnapshot ? { usageSnapshot } : {}),
            name: mcpToolName ?? update.kind ?? "tool",
            input:
              mcpToolName && update.rawInput !== undefined
                ? update.rawInput
                : {
                    ...(update.title ? { title: update.title } : {}),
                    ...(update.locations ? { locations: update.locations } : {}),
                    ...(update.rawInput !== undefined
                      ? { rawInput: update.rawInput }
                      : {}),
                  },
          });
          return;
        }
        case "tool_call_update": {
          toolActivityObserved = true;
          if (!discoveryToolCallIds.has(update.toolCallId)) unsafeToolActivityObserved = true;
          if (!onEvent) return;
          const id = update.toolCallId;
          if (pendingValidRenderToolCalls.has(id)) {
            if (update.status === "completed") {
              pendingValidRenderToolCalls.delete(id);
              surfaceEmittedDuringStream = true;
              return;
            }
            if (update.status !== "failed") return;
            const rawInput = pendingValidRenderToolCalls.get(id);
            pendingValidRenderToolCalls.delete(id);
            // The input passed structural validation, but the sole render server
            // rejected execution. Surface that failure with the original input.
            emitQueued({
              type: "tool_start",
              id,
              name: A2UI_RENDER_TOOL_NAME,
              input: { title: A2UI_RENDER_ACP_TITLE, rawInput },
            });
          }
          const status = update.status;
          if (status === "completed" || status === "failed") {
            emitQueued({
              type: "tool_end",
              id,
              result: status === "completed" ? update.rawOutput ?? update.content : undefined,
              error: status === "failed" ? extractErrorText(update.content ?? update.rawOutput) : undefined,
            });
          } else {
            emitQueued({
              type: "tool_delta",
              id,
              delta: { status: status ?? "in_progress", content: update.content, rawOutput: update.rawOutput },
            });
          }
          return;
        }
        case "plan": {
          if (!onEvent) return;
          const next = update.entries.find((e) => e.status !== "completed");
          if (next) emitQueued({ type: "status", message: next.content });
          return;
        }
        default:
          return;
      }
    });

    let promptError: string | undefined;
    let promptStopReason: string | undefined;
    let promptUsage: ReturnType<typeof normalizeHermesUsage>;
    try {
      const promptResponse = await client.request<{
        usage?: unknown;
        stopReason?: string;
        _meta?: { openneko?: { usage?: unknown } };
        fieldMeta?: { openneko?: { usage?: unknown } };
      }>("session/prompt", {
        sessionId,
        prompt: [{ type: "text", text: prompt }],
      });
      promptStopReason = promptResponse.stopReason;
      const promptMeta = promptResponse.fieldMeta ?? promptResponse._meta;
      promptUsage = normalizeHermesUsage(promptResponse.usage, promptMeta?.openneko?.usage);
      if (promptUsage) {
        turnUsage = promptUsage;
        turnUsageFinal = true;
      }
      flushProviderSummary();
    } catch (e) {
      if (e instanceof AcpProtocolError) {
        promptError = `hermes: ${e.message}`;
      } else {
        throw e;
      }
    }

    if (timedOut) {
      throw new Error(
        `hermes turn exceeded its ${Math.round(timeoutMs / 1000)}s budget and was terminated`,
      );
    }
    if (promptError) {
      return { finalText: "", error: promptError, errorCode: "protocol" };
    }
    await eventQueue;
    if (eventError) throw eventError;

    // ACP considers `end_turn` with no chunks a valid protocol response. It is
    // not a valid OpenNeko answer: accepting it used to persist a green run
    // with no assistant message, leaving the UI apparently dead. Retry only
    // when no tool ran, then fail with an explicit diagnostic.
    if (!accumulatedText.trim() && !surfaceEmittedDuringStream) {
      return {
        finalText: "",
        error:
          `hermes completed without assistant output or surface` +
          ` (stopReason=${promptStopReason ?? "unknown"})`,
        errorCode: "empty_output",
        retryable: !toolActivityObserved,
      };
    }

    if (promptUsage || configuredIdentity || observedIdentity) {
      const modelIdentity =
        configuredIdentity || observedIdentity
          ? {
              ...(configuredIdentity ? { configured: configuredIdentity } : {}),
              ...(observedIdentity ? { observed: observedIdentity } : {}),
            }
          : undefined;
      turnUsageReported = true;
      emitQueued({
        type: "usage",
        source: "outer",
        ...(observedIdentity ?? {}),
        ...(modelIdentity ? { modelIdentity } : {}),
        usage: promptUsage ?? {
          coverage: "unavailable",
          missingReasons: ["Hermes ACP session/prompt omitted usage"],
        },
      });
      await eventQueue;
      if (eventError) throw eventError;
    }

    if (accumulatedText.trim() === HERMES_RESPONSE_TRUNCATED_SENTINEL) {
      return {
        finalText: "",
        error:
          `hermes response truncated due to output length limit` +
          ` (stopReason=${promptStopReason ?? "unknown"})`,
        errorCode: "output_truncated",
        retryable: !toolActivityObserved || (!unsafeToolActivityObserved && !surfaceEmittedDuringStream),
      };
    }

    let finalText = accumulatedText;
    if (onEvent) {
      const parsed = extractSurfaceMessages(accumulatedText);
      const markdownText = extractMarkdownText(parsed.messages);
      finalText = (markdownText || parsed.text).trim();
    }

    // rawText keeps every fence (a2ui + builder); finalText above dropped the
    // builder fences when it collapsed to the a2ui markdown. run-chat-turn
    // parses action/workflow/rule/memory fences out of rawText.
    return { finalText: finalText.trim(), rawText: accumulatedText };
  } catch (e) {
    if (spawnError) throw spawnError;
    // "ACP client disposed" alone means the hermes child died mid-turn with
    // its actual cause buried in the buffered stderr — surface the tail so
    // each occurrence is diagnosable without a debug rerun. Lines echoing
    // session prompts are dropped: they carry the system prompt.
    if (e instanceof Error && e.message.includes("ACP client disposed")) {
      // The timer's SIGTERM rejects the in-flight prompt request, so the
      // post-await timedOut check below this try never runs — without this
      // branch a plain timeout masquerades as an unexplained agent death
      // (this WAS the long-undiagnosed "hermes exited mid-turn" flake).
      if (timedOut) {
        const activity = hermesAgentLogTail(env.HERMES_HOME);
        throw new Error(
          `hermes turn exceeded its ${Math.round(timeoutMs / 1000)}s budget and was terminated ` +
            `(OPENNEKO_AGENT_TURN_TIMEOUT_MS overrides)` +
            (activity ? `; agent activity: ${activity}` : ""),
          { cause: e },
        );
      }
      const exit = await Promise.race([
        exitInfoPromise,
        new Promise<null>((r) => setTimeout(r, 3000)),
      ]);
      const tail = Buffer.concat(stderrChunks)
        .toString("utf8")
        .split("\n")
        .filter((l) => l.trim() && !l.includes("Prompt on session"))
        .slice(-6)
        .join("\n")
        .slice(-600);
      const agentLog = hermesAgentLogTail(env.HERMES_HOME);
      const mem = memAvailable();
      throw new Error(
        `hermes exited mid-turn (code=${exit ? exit.code ?? "null" : "?"} signal=${
          exit ? exit.signal ?? "null" : "?"
        }${mem ? ` mem-available=${mem}` : ""})${
          tail ? `; last stderr: ${tail}` : ""
        }${agentLog ? `; agent.log tail: ${agentLog}` : ""}`,
        { cause: e },
      );
    }
    throw e;
  } finally {
    if (onEvent && turnUsage && !turnUsageReported) {
      await drainEvents().catch(() => {});
      try {
        await onEvent({
          type: "usage",
          source: "outer",
          ...(turnIdentity ?? {}),
          usage: turnUsageFinal
            ? turnUsage
            : {
                ...turnUsage,
                coverage: "partial",
                missingReasons: [
                  ...(turnUsage.missingReasons ?? []),
                  "The turn ended early; usage covers the model calls made before its last tool call.",
                ],
              },
        });
      } catch (error) {
        console.warn(`[hermes] could not report usage for an early-ended turn: ${error instanceof Error ? error.message : error}`);
      }
    }
    client?.dispose();
    if (child.exitCode == null && !signal?.aborted) {
      killProcessGroup(child, "SIGTERM");
    }
    await closedPromise.catch(() => {});
    if (debug) {
      const stderr = Buffer.concat(stderrChunks).toString("utf8");
      if (stderr) process.stderr.write(stderr);
    }
    await cleanup();
  }
}

/**
 * Hermes reports the live session identity as `<provider>:<model>` in
 * session/new.models.currentModelId. This is process-observed evidence, not a
 * copy of OpenNeko's configured identity. Split only on the first colon so
 * provider-qualified model ids remain intact.
 */
export function parseHermesSessionIdentity(
  response: unknown,
): AgentModelIdentity | undefined {
  if (!response || typeof response !== "object" || Array.isArray(response)) {
    return undefined;
  }
  const models = (response as { models?: unknown }).models;
  if (!models || typeof models !== "object" || Array.isArray(models)) {
    return undefined;
  }
  const modelState = models as Record<string, unknown>;
  const raw = modelState.currentModelId ?? modelState.current_model_id;
  if (typeof raw !== "string") return undefined;
  const separator = raw.indexOf(":");
  if (separator <= 0 || separator >= raw.length - 1) return undefined;
  const provider = raw.slice(0, separator).trim();
  const model = raw.slice(separator + 1).trim();
  if (!provider || !model) return undefined;
  return { provider, model };
}

const HERMES_COST_STATUSES = new Set(["actual", "estimated", "included", "unknown"]);

function plainRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

export function normalizeHermesUsage(
  value: unknown,
  costMeta?: unknown,
): AgentTokenUsage | undefined {
  const tokens = plainRecord(value);
  const cost = plainRecord(costMeta);
  if (!tokens && !cost) return undefined;
  const usage = { ...cost, ...tokens };
  const number = (...keys: string[]): number | undefined => {
    for (const key of keys) {
      const parsed = Number(usage[key]);
      if (Number.isFinite(parsed) && parsed >= 0) return parsed;
    }
    return undefined;
  };
  const inputTokens = number("inputTokens", "input_tokens", "promptTokens", "prompt_tokens");
  const outputTokens = number(
    "outputTokens",
    "output_tokens",
    "completionTokens",
    "completion_tokens",
  );
  const cacheReadTokens = number(
    "cacheReadTokens",
    "cache_read_tokens",
    "cachedReadTokens",
    "cached_read_tokens",
  );
  const cacheWriteTokens = number(
    "cacheWriteTokens",
    "cache_write_tokens",
    "cachedWriteTokens",
    "cached_write_tokens",
  );
  const reasoningTokens = number(
    "reasoningTokens",
    "reasoning_tokens",
    "thoughtTokens",
    "thought_tokens",
  );
  const reportedTotal = number("totalTokens", "total_tokens");
  const rawStatus = usage.cost_status ?? usage.costStatus;
  const costStatus =
    typeof rawStatus === "string" && HERMES_COST_STATUSES.has(rawStatus)
      ? (rawStatus as NonNullable<AgentTokenUsage["costStatus"]>)
      : undefined;
  const rawSource = usage.cost_source ?? usage.costSource;
  const costSource = typeof rawSource === "string" && rawSource ? rawSource : undefined;
  const rawVersion = usage.pricing_version ?? usage.pricingVersion;
  const pricingVersion = typeof rawVersion === "string" && rawVersion ? rawVersion : undefined;
  const reportedCost = number("costUsd", "cost_usd", "billedCostUsd", "billed_cost_usd");
  const unknownCostCalls = number("unknown_cost_calls", "unknownCostCalls");
  // Only provider-reported cost is billed; an unpriced call never counts as zero.
  const billedCostUsd =
    costStatus === undefined || costStatus === "actual" ? reportedCost : undefined;
  const estimatedCostUsd =
    costStatus === "estimated" || costStatus === "included" ? reportedCost : undefined;
  if (
    inputTokens === undefined &&
    outputTokens === undefined &&
    reportedTotal === undefined &&
    reportedCost === undefined &&
    costStatus === undefined
  ) {
    return undefined;
  }
  const complete = inputTokens !== undefined && outputTokens !== undefined;
  const missingReasons = [
    ...(!complete ? ["Hermes ACP usage omitted input or output token counts"] : []),
    ...(costStatus === "unknown"
      ? [`Hermes could not price ${unknownCostCalls ?? "some"} model call(s)`]
      : []),
  ];
  return {
    ...(inputTokens !== undefined ? { inputTokens } : {}),
    ...(outputTokens !== undefined ? { outputTokens } : {}),
    ...(cacheReadTokens !== undefined ? { cacheReadTokens } : {}),
    ...(cacheWriteTokens !== undefined ? { cacheWriteTokens } : {}),
    ...(reasoningTokens !== undefined ? { reasoningTokens } : {}),
    ...(reportedTotal !== undefined
      ? { totalTokens: reportedTotal }
      : complete
        ? { totalTokens: inputTokens + outputTokens }
        : {}),
    ...(billedCostUsd !== undefined ? { billedCostUsd, currency: "USD" } : {}),
    ...(estimatedCostUsd !== undefined ? { estimatedCostUsd, currency: "USD" } : {}),
    ...(costStatus ? { costStatus } : {}),
    ...(costSource ? { costSource } : {}),
    ...(pricingVersion ? { pricingCatalogVersion: pricingVersion } : {}),
    coverage: complete ? "complete" : "partial",
    ...(missingReasons.length > 0 ? { missingReasons } : {}),
  };
}

function extractErrorText(raw: unknown): string {
  if (typeof raw === "string") return raw;
  if (raw && typeof raw === "object") return JSON.stringify(raw);
  return "Tool failed";
}
