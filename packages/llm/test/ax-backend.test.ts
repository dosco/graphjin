import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentEvent } from "../src/agent-backend";
import { MODEL_PRICES_VERSION } from "../src/spend/model-prices";

type Spawned = { command: string; env: NodeJS.ProcessEnv; stdin: string };
const spawned: Spawned[] = [];
let script: { lines: object[]; stderr?: string; code?: number } = { lines: [] };
let queued: (typeof script)[] = [];

vi.mock("node:child_process", async () => {
  const actual = await vi.importActual<typeof import("node:child_process")>("node:child_process");
  return {
    ...actual,
    spawn: (command: string, _args: string[], options: { env: NodeJS.ProcessEnv }) => {
      const child = new EventEmitter() as EventEmitter & Record<string, unknown>;
      const stdin = new PassThrough();
      const stdout = new PassThrough();
      const stderr = new PassThrough();
      const record: Spawned = { command, env: options.env, stdin: "" };
      spawned.push(record);
      stdin.on("data", (c: Buffer) => (record.stdin += c.toString()));
      Object.assign(child, { stdin, stdout, stderr, pid: undefined, exitCode: null, kill: () => true });
      const current = queued.shift() ?? script;
      stdin.on("finish", () => {
        const run = JSON.parse(record.stdin) as { run_id: string };
        for (const line of current.lines) stdout.write(`${JSON.stringify({ version: 1, run_id: run.run_id, ...line })}\n`);
        if (current.stderr) stderr.write(current.stderr);
        stdout.end();
        stderr.end();
        child.exitCode = current.code ?? 0;
        setImmediate(() => child.emit("close", current.code ?? 0));
      });
      return child;
    },
  };
});

const { AxBackend } = await import("../src/agent-backends/ax");

const workspace = {
  orgRoot: "/tmp/neko-ax/org",
  skillsRoot: "/tmp/neko-ax/org/skills",
  memoryRoot: "/tmp/neko-ax/org/memory",
  knowledgeRoot: "/tmp/neko-ax/org/knowledge",
  uploadsRoot: "/tmp/neko-ax/org/uploads",
  runsRoot: "/tmp/neko-ax/org/runs",
  threadUploadsRoot: "/tmp/neko-ax/org/uploads/t1",
  runRoot: "/tmp/neko-ax/org/runs/r1",
  artifactRoot: "/tmp/neko-ax/org/runs/r1/artifacts",
  binRoot: "/tmp/neko-ax/org/runs/r1/bin",
};
const config = { route: { provider: "anthropic" as const, model: "claude-sonnet-5-5", keyEnv: "ANTHROPIC_API_KEY" }, reasoningEffort: "high" as const };
const finished = (result: object, sequence: number) => ({ sequence, type: "run.finished", result });

afterEach(() => {
  spawned.length = 0;
  queued = [];
  vi.unstubAllEnvs();
});

describe("AxBackend", () => {
  it("streams prose, hides fences and maps tools, skills and usage", async () => {
    const answer = "Revenue rose 4%.\n```neko_followups\n[\"Why?\"]\n```";
    script = {
      lines: [
        { sequence: 1, type: "run.started" },
        { sequence: 0, type: "thought.delta", data: { version: 0, index: 0, text: "Checking orders" } },
        { sequence: 2, type: "tool.started", name: "mcp_neko_graphjin_query", operation_id: 1, data: { query: "{ product { productnumber } }" } },
        { sequence: 3, type: "tool.finished", name: "mcp_neko_graphjin_query", operation_id: 1, data: { rows: 3 } },
        { sequence: 4, type: "tool.started", name: "mcp_neko_graphjin_query", operation_id: 3, data: { query: "{ product { productnumbe } }" } },
        { sequence: 5, type: "tool.finished", name: "mcp_neko_graphjin_query", operation_id: 3, error: "mcp_neko_graphjin_query_failed", data: { errors: [{ message: "did you mean productnumber?" }] } },
        { sequence: 6, type: "tool.started", name: "mcp_neko_ui_render_cards", operation_id: 2 },
        { sequence: 7, type: "tool.finished", name: "mcp_neko_ui_render_cards", operation_id: 2, data: {} },
        { sequence: 8, type: "skill.used", name: "sales-review" },
        { sequence: 9, type: "model.request.finished", observed_model: "claude-sonnet-5-5-20261001", provider: "anthropic" },
        { sequence: 0, type: "answer.delta", data: { version: 0, index: 0, text: "Revenue rose 4%.\n```neko_a2" } },
        finished({ status: "completed", kind: "answer", answer, usage: { requests: 2, reported: 2, input_tokens: 10, output_tokens: 5, total_tokens: 15, coverage: "complete" } }, 10),
      ],
    };
    const events: AgentEvent[] = [];
    const result = await new AxBackend(config).run({ prompt: "Context", userMessage: "How are sales?", workspace, onEvent: (e) => { events.push(e); } });

    expect(result).toMatchObject({ status: "completed", rawText: answer });
    expect(events.filter((e) => e.type === "message").map((e) => (e as { content: string }).content).join("")).toBe("Revenue rose 4%.\n```neko_followups\n[\"Why?\"]\n```");
    expect(events).toContainEqual({ type: "progress", id: "ax-summary-1", content: "Checking orders", source: "provider_summary", provider: "anthropic" });
    expect(events).toContainEqual({ type: "tool_start", id: "ax-op-1", name: "mcp_neko_graphjin_query", input: { query: "{ product { productnumber } }" } });
    expect(events).toContainEqual({ type: "tool_end", id: "ax-op-1", result: { rows: 3 } });
    expect(events).toContainEqual({ type: "tool_end", id: "ax-op-3", error: "mcp_neko_graphjin_query_failed", result: { errors: [{ message: "did you mean productnumber?" }] } });
    expect(events.some((e) => e.type === "tool_start" && e.name === "mcp_neko_ui_render_cards")).toBe(false);
    expect(events).toContainEqual({ type: "tool_start", id: "ax-skill-sales-review", name: "skill_used", input: { path: "/tmp/neko-ax/org/skills/sales-review/SKILL.md" } });
    expect(events).toContainEqual({
      type: "usage",
      source: "outer",
      provider: "anthropic",
      model: "claude-sonnet-5-5-20261001",
      modelIdentity: {
        configured: { provider: "anthropic", model: "claude-sonnet-5-5" },
        observed: { provider: "anthropic", model: "claude-sonnet-5-5-20261001" },
      },
      usage: {
        inputTokens: 10,
        outputTokens: 5,
        totalTokens: 15,
        coverage: "complete",
        costStatus: "estimated",
        costSource: "official_docs_snapshot",
        currency: "USD",
        estimatedCostUsd: expect.closeTo(0.00007, 10),
        pricingCatalogVersion: MODEL_PRICES_VERSION,
      },
    });
  });

  it("gives the harness its model, limits and tools, and keeps credentials from it", async () => {
    vi.stubEnv("ANTHROPIC_API_KEY", "openshell:resolve:env:MODEL_API_KEY");
    vi.stubEnv("MODEL_API_KEY", "openshell:resolve:env:MODEL_API_KEY");
    vi.stubEnv("OPENNEKO_BROKER_TOKEN", "broker-secret");
    vi.stubEnv("OPENNEKO_MCP_BRIDGE", "/app/mcp-bridge.js");
    script = { lines: [finished({ status: "completed", kind: "answer", answer: "ok" }, 1)] };
    await new AxBackend(config).run({
      prompt: "p",
      workspace,
      timeoutMs: 120_000,
      maxToolIterations: 40,
      networkHosts: ["api.example.com"],
      mcpServers: { neko_graphjin_agent: {}, neko_ui: {}, other: {} },
      mcpBridgeEnv: { OPENNEKO_MCP_ORG_ID: "org-1", OPENNEKO_MCP_THREAD_ID: "t-1", OPENNEKO_MCP_SKILLS_ROOT: workspace.skillsRoot },
    });
    const [{ command, env, stdin }] = spawned;
    expect(command).toBe("/usr/local/bin/ax-harness");
    expect(env).toMatchObject({
      HARNESS_MODEL_PROVIDER: "anthropic",
      HARNESS_MODEL: "claude-sonnet-5-5",
      HARNESS_MODEL_API_KEY: "openshell:resolve:env:MODEL_API_KEY",
      OPENNEKO_MCP_SERVERS: "neko_graphjin_agent",
      OPENNEKO_MCP_ORG_ID: "org-1",
      OPENNEKO_BROKER_TOKEN: "broker-secret",
      OPENNEKO_HARNESS_WORKSPACE_DIR: workspace.orgRoot,
      OPENNEKO_HARNESS_SHELL: "1",
      OPENNEKO_HARNESS_SKILLS_READ: "1",
      OPENNEKO_HARNESS_UPLOADS_DIR: workspace.threadUploadsRoot,
      OPENNEKO_HARNESS_WEB_HOSTS: "api.example.com",
      OPENNEKO_HARNESS_TERMINAL_TIMEOUT_SECONDS: "180",
    });
    expect(env.ANTHROPIC_API_KEY).toBeUndefined();
    expect(env.MODEL_API_KEY).toBeUndefined();
    expect(JSON.parse(stdin)).toMatchObject({
      version: 1,
      prompt: "p",
      timeout_ms: 120_000,
      max_actor_steps: 40,
      max_operations: 40,
      max_child_steps: 50,
      reasoning_effort: "high",
    });
  });

  it("admits the child agent unless the run disables delegation", async () => {
    script = { lines: [finished({ status: "completed", kind: "answer", answer: "ok" }, 1)] };
    await new AxBackend(config).run({ prompt: "p", workspace });
    expect(spawned[0].env.OPENNEKO_HARNESS_CHILD_TOOLS).toBe("*");
    script = { lines: [finished({ status: "completed", kind: "answer", answer: "ok" }, 1)] };
    await new AxBackend(config).run({ prompt: "p", workspace, nativeDelegation: "disabled" });
    expect(spawned[1].env.OPENNEKO_HARNESS_CHILD_TOOLS).toBeUndefined();
  });

  it("runs again once after a malformed call when nothing was shown or written", async () => {
    const malformed = {
      lines: [finished({ status: "failed", kind: "failure", code: "model_failed" }, 1)],
      stderr: "model failed: Gemini finish reason was blocked: MALFORMED_FUNCTION_CALL",
      code: 1,
    };
    queued = [malformed, { lines: [finished({ status: "completed", kind: "answer", answer: "ok" }, 1)] }];
    const events: AgentEvent[] = [];
    const result = await new AxBackend(config).run({ prompt: "p", workspace, onEvent: (event) => void events.push(event) });
    expect(result.status).toBe("completed");
    expect(spawned).toHaveLength(2);
    expect(events).toContainEqual({ type: "retry", reason: "malformed_function_call" });

    spawned.length = 0;
    queued = [{ ...malformed, lines: [{ sequence: 1, type: "tool.started", name: "file_write", operation_id: 1, effect: "durable" }, finished({ status: "failed", kind: "failure", code: "model_failed" }, 2)] }];
    const wrote = await new AxBackend(config).run({ prompt: "p", workspace });
    expect(wrote.status).toBe("failed");
    expect(spawned).toHaveLength(1);
  });

  it("drops the broker binding when no bridge server runs", async () => {
    vi.stubEnv("OPENNEKO_BROKER_TOKEN", "broker-secret");
    script = { lines: [finished({ status: "completed", kind: "answer", answer: "ok" }, 1)] };
    await new AxBackend(config).run({ prompt: "p", workspace, orgId: "org-1" });
    expect(spawned[0].env.OPENNEKO_BROKER_TOKEN).toBeUndefined();
    expect(spawned[0].env.OPENNEKO_MCP_ORG_ID).toBe("org-1");
  });

  it("reports a deadline as a timed-out failure", async () => {
    script = { lines: [finished({ status: "failed", kind: "failure", code: "deadline_exceeded" }, 1)] };
    const events: AgentEvent[] = [];
    const result = await new AxBackend(config).run({ prompt: "p", workspace, timeoutMs: 60_000, onEvent: (e) => { events.push(e); } });
    expect(result).toMatchObject({ status: "failed", timedOut: true, errorCode: "deadline_exceeded" });
    expect(result.error).toContain("exceeded its 60s budget");
    expect(events.at(-1)).toMatchObject({ type: "error" });
  });

  it("carries the harness failure code", async () => {
    script = { lines: [finished({ status: "failed", kind: "failure", code: "model_http_429" }, 1)] };
    const result = await new AxBackend(config).run({ prompt: "p", workspace });
    expect(result).toMatchObject({ status: "failed", errorCode: "model_http_429" });
    expect(result.timedOut).toBeUndefined();
  });

  it("marks a budget summary answer degraded and maps route fallbacks to retries", async () => {
    script = {
      lines: [
        { sequence: 1, type: "model.route.fallback", name: "primary", origin: "secondary", error: "transient_provider_failure" },
        { sequence: 2, type: "executor.step.failed", error: "actor_code_error" },
        finished({ status: "completed", kind: "summary", answer: "Partial answer.", code: "actor_steps_exhausted" }, 3),
      ],
    };
    const events: AgentEvent[] = [];
    const result = await new AxBackend(config).run({ prompt: "p", workspace, onEvent: (e) => { events.push(e); } });
    expect(result).toMatchObject({ status: "completed", degraded: true, errorCode: "actor_steps_exhausted", finalText: "Partial answer." });
    expect(events.filter((e) => e.type === "retry")).toEqual([
      { type: "retry", reason: "model_route_fallback" },
      { type: "retry", reason: "executor_step_failed" },
    ]);
  });

  it("reports the stderr tail when the harness ends without a result", async () => {
    script = { lines: [], stderr: "configure HARNESS_MODEL_URL, HARNESS_MODEL and HARNESS_MODEL_API_KEY\n", code: 2 };
    const result = await new AxBackend(config).run({ prompt: "p", workspace });
    expect(result.status).toBe("failed");
    expect(result.error).toContain("code=2");
    expect(result.error).toContain("HARNESS_MODEL_API_KEY");
    expect(result.errorCode).toBe("exit");
  });

  it("refuses to run without a configured model", async () => {
    const result = await new AxBackend().run({ prompt: "p", workspace });
    expect(result.status).toBe("failed");
    expect(spawned).toHaveLength(0);
  });
});
