// Ax backend: one ax-harness process per turn. Protocol: apps/ax-harness/docs/RUN-PROTOCOL.md.

import { estimateTokenCostUsd, MODEL_PRICES_VERSION } from "../spend/model-prices";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { agentLimits, axLimitEnv, axRunLimits, type AgentReasoningEffort } from "../agent-limits";
import {
  agentTurnTimeoutMs,
  type AgentBackend,
  type AgentEvent,
  type AgentModelIdentity,
  type AgentRunOptions,
  type AgentRunResult,
  type AgentTokenUsage,
} from "../agent-backend";
import { AX_HARNESS_BINARY } from "../agent-runtime-contract";
import { registerAgentCanceller } from "../agent-shutdown";
import type { AxModelRoute } from "../provider-runtime";
import { A2UI_RENDER_ACP_TITLE, A2UI_RENDER_SERVER_NAME } from "../work/a2ui-contract";
import { extractMarkdownText, outsideFenceText } from "./output";
import { extractSurfaceMessages } from "./surface";

/** Trusted, non-secret model settings, resolved on the host from admin settings. */
export type AxBackendConfig = {
  route: AxModelRoute;
  contextWindowTokens?: number;
  maxOutputTokens?: number;
  reasoningEffort?: AgentReasoningEffort;
};

type AxUsage = {
  requests?: number;
  reported?: number;
  input_tokens?: number;
  output_tokens?: number;
  total_tokens?: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  reasoning_tokens?: number;
  coverage?: "complete" | "partial" | "unavailable";
};

type AxResult = {
  status: "completed" | "failed" | "cancelled";
  kind?: "answer" | "summary" | "clarification" | "failure";
  answer?: string;
  code?: string;
  usage?: AxUsage;
};

type AxEvent = {
  version: number;
  run_id: string;
  sequence: number;
  type: string;
  name?: string;
  operation_id?: number;
  effect?: string;
  observed_model?: string;
  provider?: string;
  error?: string;
  data?: unknown;
  result?: AxResult;
};

const KILL_GRACE_MS = 15_000;

function killProcessGroup(child: ChildProcess, signal: NodeJS.Signals): void {
  if (!child.pid) return;
  try {
    process.kill(-child.pid, signal);
  } catch {
    // Already exited, or not a group leader.
  }
  try {
    child.kill(signal);
  } catch {
    // ignore
  }
}

export function normalizeAxUsage(usage: AxUsage | undefined, route?: { provider: string; model: string }): AgentTokenUsage {
  if (!usage) return { coverage: "unavailable", missingReasons: ["Ax Harness reported no usage"] };
  const tokens = {
    inputTokens: usage.input_tokens,
    outputTokens: usage.output_tokens,
    totalTokens: usage.total_tokens,
    cacheReadTokens: usage.cache_read_tokens,
    cacheWriteTokens: usage.cache_write_tokens,
    reasoningTokens: usage.reasoning_tokens,
  };
  // A partial report is scaled up by the calls it missed.
  const scale = usage.coverage === "complete" ? 1
    : usage.coverage === "partial" && usage.reported && usage.requests ? usage.requests / usage.reported
    : 0;
  const reportedCost = route && scale > 0
    ? estimateTokenCostUsd(route.provider, route.model, {
        inputTokens: usage.input_tokens,
        outputTokens: usage.output_tokens,
        cacheReadTokens: usage.cache_read_tokens,
        cacheWriteTokens: usage.cache_write_tokens,
        totalTokens: usage.total_tokens,
      })
    : undefined;
  const cost = reportedCost === undefined ? undefined : reportedCost * scale;
  return {
    ...Object.fromEntries(Object.entries(tokens).filter(([, v]) => typeof v === "number")),
    coverage: usage.coverage ?? "unavailable",
    ...(cost === undefined
      ? { costStatus: "unknown" as const }
      : { costStatus: "estimated" as const, estimatedCostUsd: cost, currency: "USD", costSource: "official_docs_snapshot", pricingCatalogVersion: MODEL_PRICES_VERSION }),
    ...(usage.coverage === "partial"
      ? { missingReasons: [`The provider reported usage for ${usage.reported ?? 0} of ${usage.requests ?? 0} model calls`] }
      : {}),
  };
}

export class AxBackend implements AgentBackend {
  readonly id = "ax" as const;
  readonly capabilities = { mcpTools: true, sessionResume: false, nativeDelegation: "ax-worker" } as const;
  readonly configuredIdentity?: AgentModelIdentity;
  readonly model?: string;

  constructor(readonly ax?: AxBackendConfig) {
    if (ax) {
      this.configuredIdentity = { provider: ax.route.provider, model: ax.route.model };
      this.model = ax.route.model;
    }
  }

  async run(opts: AgentRunOptions): Promise<AgentRunResult> {
    const { onEvent, signal, backendState } = opts;
    if (signal?.aborted) return { finalText: "", status: "cancelled", backendState };
    try {
      let out = await this.runOnce(opts);
      // Gemini sometimes repeats a malformed call past the harness resamples;
      // a fresh run is safe when nothing reached the user and nothing was written.
      if (out.error && out.retrySafe && out.error.includes("MALFORMED_FUNCTION_CALL") && !signal?.aborted) {
        await onEvent?.({ type: "retry", reason: "malformed_function_call" });
        out = await this.runOnce(opts);
      }
      if (out.status === "cancelled" || signal?.aborted) return { finalText: "", status: "cancelled", backendState };
      const code = out.errorCode ? { errorCode: out.errorCode } : {};
      if (out.error) {
        await onEvent?.({ type: "error", message: out.error });
        return { finalText: "", status: "failed", backendState, error: out.error, ...code, ...(out.timedOut ? { timedOut: true } : {}) };
      }
      return { finalText: out.finalText, rawText: out.rawText, status: "completed", backendState, ...code, ...(out.degraded ? { degraded: true } : {}) };
    } catch (error) {
      if (signal?.aborted) return { finalText: "", status: "cancelled", backendState };
      const message = error instanceof Error ? error.message : String(error);
      await onEvent?.({ type: "error", message });
      return { finalText: "", status: "failed", backendState, error: message };
    }
  }

  private async runOnce(opts: AgentRunOptions): Promise<{
    finalText: string;
    rawText?: string;
    error?: string;
    errorCode?: string;
    degraded?: boolean;
    timedOut?: boolean;
    retrySafe?: boolean;
    status?: "cancelled";
  }> {
    if (!this.ax) throw new Error("Ax backend has no model configured. Set the primary provider in admin settings.");
    const { workspace, onEvent, signal } = opts;
    const timeoutMs = opts.timeoutMs ?? agentTurnTimeoutMs();
    const runId = (opts.tag ?? `ax-${Date.now()}`).replace(/[^\w.:-]/g, "").slice(0, 128) || "ax";

    let cwd = workspace?.orgRoot;
    let cleanupScratch: (() => Promise<void>) | undefined;
    if (!cwd) {
      const scratch = await mkdtemp(join(tmpdir(), "neko-ax-"));
      cwd = scratch;
      cleanupScratch = () => rm(scratch, { recursive: true, force: true }).catch(() => {});
    }

    const limits = agentLimits("chat");
    const env = this.environment(opts, cwd, axLimitEnv(limits));
    const spec = {
      version: 1,
      run_id: runId,
      input_id: runId,
      prompt: opts.userMessage ? `${opts.prompt}\n\nCurrent user message:\n${opts.userMessage}` : opts.prompt,
      ...(opts.userMessage ? { skill_query: opts.userMessage } : {}),
      stream_responses: Boolean(onEvent),
      ...axRunLimits({
        ...limits,
        timeoutMs: Math.min(timeoutMs, 1_800_000),
        ...(opts.maxToolIterations ? { maxToolCalls: opts.maxToolIterations } : {}),
        ...((opts.reasoningEffort ?? this.ax.reasoningEffort)
          ? { reasoningEffort: opts.reasoningEffort ?? this.ax.reasoningEffort }
          : {}),
      }),
      ...(this.ax.contextWindowTokens ? { context_window_tokens: this.ax.contextWindowTokens } : {}),
      ...(this.ax.maxOutputTokens ? { max_output_tokens: this.ax.maxOutputTokens } : {}),
      ...(opts.debug ? { debug: true } : {}),
    };

    const child = spawn(AX_HARNESS_BINARY, [], { stdio: ["pipe", "pipe", "pipe"], cwd, env, detached: true });
    const stderr: Buffer[] = [];
    child.stderr?.on("data", (c: Buffer) => {
      stderr.push(c);
      if (opts.debug) process.stderr.write(c);
    });
    let spawnError: Error | undefined;
    child.on("error", (e) => {
      spawnError = new Error(`ax-harness spawn failed: ${e.message}`);
    });
    const exited = new Promise<number | null>((resolve) => child.on("close", (code) => resolve(code)));
    const unregister = registerAgentCanceller(() => killProcessGroup(child, "SIGKILL"));
    let cancelled = false;
    const onAbort = () => {
      cancelled = true;
      killProcessGroup(child, "SIGTERM");
    };
    signal?.addEventListener("abort", onAbort, { once: true });
    // The harness enforces timeout_ms itself; this only reaps a stuck process.
    const reaper = setTimeout(() => killProcessGroup(child, "SIGKILL"), timeoutMs + KILL_GRACE_MS);

    let queue = Promise.resolve();
    let eventError: unknown;
    let shownText = false;
    const emit = (event: AgentEvent) => {
      if ((event.type === "message" || event.type === "interim") && event.content) shownText = true;
      if (!onEvent) return;
      queue = queue.then(() => onEvent(event)).catch((e) => {
        eventError ??= e;
      });
    };

    const mapper = new AxEventMapper(emit, this.ax.route, workspace?.skillsRoot, opts.debug === true);
    let result: AxResult | undefined;
    let protocolError: string | undefined;
    try {
      // An early exit (invalid configuration) closes stdin; the result check reports it.
      child.stdin?.on("error", () => {});
      child.stdin?.end(JSON.stringify(spec));
      const lines = createInterface({ input: child.stdout! });
      let last = 0;
      for await (const line of lines) {
        if (!line.trim()) continue;
        let event: AxEvent;
        try {
          event = JSON.parse(line) as AxEvent;
        } catch {
          protocolError ??= "ax-harness wrote a line that is not JSON";
          continue;
        }
        if (event.version !== 1 || event.run_id !== runId || (event.sequence !== 0 && event.sequence <= last)) {
          protocolError ??= `ax-harness event out of order: ${event.type}`;
          continue;
        }
        if (event.sequence) last = event.sequence;
        if (event.type === "run.finished") result = event.result;
        else mapper.handle(event);
      }
      const code = await exited;
      await queue;
      if (eventError) throw eventError;
      if (spawnError) throw spawnError;
      if (cancelled || signal?.aborted) return { finalText: "", status: "cancelled" };
      if (!result) {
        const tail = Buffer.concat(stderr).toString("utf8").trim().split("\n").slice(-6).join("\n").slice(-600);
        return {
          finalText: "",
          error: protocolError ?? `ax-harness exited (code=${code ?? "null"}) without a result${tail ? `: ${tail}` : ""}`,
          errorCode: protocolError ? "protocol" : "exit",
        };
      }
      mapper.usage(result.usage);
      await queue;
      if (result.status === "cancelled") return { finalText: "", status: "cancelled" };
      const answer = result.answer ?? "";
      if (result.status !== "completed" || !answer.trim()) {
        const reason = result.code ?? "no answer";
        const tail = Buffer.concat(stderr).toString("utf8").trim().slice(-600);
        return {
          finalText: "",
          errorCode: result.code ?? "empty_output",
          retrySafe: !shownText && !mapper.wroteState,
          error: reason === "deadline_exceeded"
            ? `ax turn exceeded its ${Math.round(timeoutMs / 1000)}s budget and was terminated (OPENNEKO_AGENT_TURN_TIMEOUT_MS overrides)`
            : `ax run failed: ${reason}${tail ? `: ${tail}` : ""}`,
          ...(reason === "deadline_exceeded" ? { timedOut: true } : {}),
        };
      }
      mapper.finish(answer);
      await queue;
      if (eventError) throw eventError;
      let finalText = answer;
      if (onEvent) {
        const parsed = extractSurfaceMessages(answer);
        finalText = extractMarkdownText(parsed.messages) || parsed.text;
      }
      return {
        finalText: finalText.trim(),
        rawText: answer,
        ...(result.kind === "summary" ? { degraded: true, ...(result.code ? { errorCode: result.code } : {}) } : {}),
      };
    } finally {
      clearTimeout(reaper);
      signal?.removeEventListener("abort", onAbort);
      unregister();
      if (child.exitCode == null) killProcessGroup(child, "SIGTERM");
      await cleanupScratch?.();
    }
  }

  private environment(opts: AgentRunOptions, cwd: string, limitEnv: Record<string, string>): NodeJS.ProcessEnv {
    const route = this.ax!.route;
    const { workspace } = opts;
    const env: NodeJS.ProcessEnv = { ...process.env };
    if (workspace) env.PATH = `${workspace.binRoot}:${process.env.PATH || ""}`;
    // The key is an OpenShell placeholder; only the harness may hold it.
    const key = (route.keyEnv && process.env[route.keyEnv]) || process.env.MODEL_API_KEY || "none";
    for (const name of ["MODEL_API_KEY", route.keyEnv, "HARNESS_MODEL_ROUTES"]) if (name) delete env[name];
    Object.assign(env, {
      HARNESS_MODEL_PROVIDER: route.provider,
      HARNESS_MODEL: route.model,
      HARNESS_MODEL_API_KEY: key,
      HARNESS_MODEL_URL: route.url ?? "",
      HARNESS_MODEL_OPTIONS: route.options ? JSON.stringify(route.options) : "",
    });
    Object.assign(env, limitEnv);

    const names = Object.keys(opts.mcpServers ?? {}).filter(
      (name) => /^neko_[a-z0-9_]+$/.test(name) && (name !== A2UI_RENDER_SERVER_NAME || opts.wantsCards),
    );
    const bridge = Boolean(process.env.OPENNEKO_MCP_BRIDGE && opts.mcpBridgeEnv && names.length > 0);
    if (bridge) {
      Object.assign(env, opts.mcpBridgeEnv, { OPENNEKO_MCP_SERVERS: names.join(",") });
    } else {
      delete env.OPENNEKO_MCP_BRIDGE;
      delete env.OPENNEKO_BROKER_URL;
      delete env.OPENNEKO_BROKER_TOKEN;
    }
    env.OPENNEKO_MCP_ORG_ID = opts.mcpBridgeEnv?.OPENNEKO_MCP_ORG_ID ?? opts.orgId ?? "local";
    env.OPENNEKO_MCP_THREAD_ID = opts.mcpBridgeEnv?.OPENNEKO_MCP_THREAD_ID ?? opts.tag ?? "local";
    env.OPENNEKO_HARNESS_WORKSPACE_DIR = cwd;
    env.OPENNEKO_HARNESS_SHELL = "1";
    if (workspace?.threadUploadsRoot) env.OPENNEKO_HARNESS_UPLOADS_DIR = workspace.threadUploadsRoot;
    else delete env.OPENNEKO_HARNESS_UPLOADS_DIR;
    if (workspace?.skillsRoot) {
      env.OPENNEKO_HARNESS_SKILLS_READ = "1";
      env.OPENNEKO_MCP_SKILLS_ROOT = opts.mcpBridgeEnv?.OPENNEKO_MCP_SKILLS_ROOT ?? workspace.skillsRoot;
    }
    if (opts.networkHosts?.length) env.OPENNEKO_HARNESS_WEB_HOSTS = opts.networkHosts.join(",");
    else delete env.OPENNEKO_HARNESS_WEB_HOSTS;
    if (opts.debug) env.OPENNEKO_HARNESS_DEBUG = "1";
    else delete env.OPENNEKO_HARNESS_DEBUG;
    if (opts.nativeDelegation === "disabled") delete env.OPENNEKO_HARNESS_CHILD_TOOLS;
    else env.OPENNEKO_HARNESS_CHILD_TOOLS = "*";
    return env;
  }
}

/** Maps harness events onto AgentEvents. Exported for tests. */
export class AxEventMapper {
  private text = "";
  private textVersion = -1;
  private emittedOutside = "";
  private thought = "";
  private thoughtCount = 0;
  private observed?: AgentModelIdentity;
  private readonly renderCalls = new Set<number>();
  wroteState = false;

  constructor(
    private readonly emit: (event: AgentEvent) => void,
    private readonly route: AxModelRoute,
    private readonly skillsRoot?: string,
    private readonly debug = false,
  ) {}

  private get summaryProvider(): "anthropic" | "google-gemini" | undefined {
    return this.route.provider === "anthropic" || this.route.provider === "google-gemini" ? this.route.provider : undefined;
  }

  handle(event: AxEvent): void {
    const data = (event.data ?? {}) as { version?: number; text?: string; code?: string; error?: string };
    switch (event.type) {
      case "answer.delta": {
        if (typeof data.text !== "string") return;
        this.flushThought();
        if (data.version !== this.textVersion) {
          this.textVersion = data.version ?? 0;
          this.text = "";
        }
        this.text += data.text;
        this.emitOutside(outsideFenceText(this.text));
        return;
      }
      case "thought.delta":
        if (this.summaryProvider && typeof data.text === "string") this.thought = (this.thought + data.text).slice(0, 6_000);
        return;
      case "actor.step":
        this.flushThought();
        if (typeof data.text === "string" && data.text.trim()) this.emit({ type: "status", message: data.text.trim() });
        return;
      case "tool.started": {
        this.flushThought();
        const id = event.operation_id ?? 0;
        if (event.effect === "durable") this.wroteState = true;
        if (event.name === A2UI_RENDER_ACP_TITLE) {
          this.renderCalls.add(id);
          return;
        }
        this.emit({ type: "tool_start", id: `ax-op-${id}`, name: event.name ?? "tool", ...(event.data !== undefined ? { input: event.data } : {}) });
        return;
      }
      case "tool.finished": {
        const id = event.operation_id ?? 0;
        // The neko_ui bridge server emits the surface; hide a successful render.
        if (this.renderCalls.delete(id)) {
          if (!event.error) return;
          this.emit({ type: "tool_start", id: `ax-op-${id}`, name: event.name ?? "tool" });
        }
        this.emit({
          type: "tool_end",
          id: `ax-op-${id}`,
          ...(event.data !== undefined ? { result: event.data } : {}),
          ...(event.error ? { error: event.error } : {}),
        });
        return;
      }
      case "skill.shortlist": {
        const names = Array.isArray(event.data) ? event.data.filter((n: unknown) => typeof n === "string") : [];
        this.emit({ type: "status", message: event.error ? `Skill search failed: ${event.error}` : `Skill guides that may fit: ${names.join(", ")}` });
        return;
      }
      case "skill.loaded": {
        if (!event.name || !this.skillsRoot) return;
        const id = `ax-skill-load-${event.name}`;
        this.emit({ type: "tool_start", id, name: "skill_view", input: { path: join(this.skillsRoot, event.name, "SKILL.md") } });
        this.emit({ type: "tool_end", id });
        return;
      }
      case "skill.used": {
        if (!event.name || !this.skillsRoot) return;
        // skill-usage.ts records a skill from its SKILL.md path.
        const id = `ax-skill-${event.name}`;
        this.emit({ type: "tool_start", id, name: "skill_used", input: { path: join(this.skillsRoot, event.name, "SKILL.md") } });
        this.emit({ type: "tool_end", id });
        return;
      }
      case "actor.code":
        if (this.debug && typeof data.code === "string") {
          this.emit({ type: "status", message: `actor code${typeof data.error === "string" ? ` failed (${data.error})` : ""}:\n${data.code}` });
        }
        return;
      case "model.route.fallback":
        this.emit({ type: "retry", reason: "model_route_fallback" });
        return;
      case "executor.step.failed":
        this.emit({ type: "retry", reason: "executor_step_failed" });
        return;
      case "model.request.finished":
        if (event.observed_model) this.observed = { provider: event.provider ?? this.route.provider, model: event.observed_model };
        return;
      default:
        return;
    }
  }

  finish(answer: string): void {
    this.flushThought();
    // The stream holds back a tail that could open a hidden fence; the answer is complete, so release it.
    const outside = outsideFenceText(`${answer}\n`);
    this.emitOutside(answer.endsWith("\n") ? outside : outside.replace(/\n$/, ""));
  }

  usage(usage: AxUsage | undefined): void {
    const configured = { provider: this.route.provider, model: this.route.model };
    this.emit({
      type: "usage",
      source: "outer",
      ...(this.observed ?? {}),
      modelIdentity: { configured, ...(this.observed ? { observed: this.observed } : {}) },
      usage: normalizeAxUsage(usage, configured),
    });
  }

  private emitOutside(outside: string): void {
    // A new responder version can replace earlier text. Stop the stream then;
    // the final answer is still the run result.
    if (!outside.startsWith(this.emittedOutside)) return;
    const delta = outside.slice(this.emittedOutside.length);
    if (!delta) return;
    this.emittedOutside = outside;
    this.emit({ type: "message", role: "assistant", content: delta });
  }

  private flushThought(): void {
    const content = this.thought.trim();
    this.thought = "";
    if (!content || !this.summaryProvider) return;
    this.thoughtCount += 1;
    this.emit({ type: "progress", id: `ax-summary-${this.thoughtCount}`, content, source: "provider_summary", provider: this.summaryProvider });
  }
}
