import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentEvent } from "../src/agent-backend";
import {
  chunkNotification,
  createMockSpawn,
  interimNotification,
  makeController,
  NO_RESPONSE,
  thoughtNotification,
  toolCallNotification,
  toolCallUpdateNotification,
} from "./helpers/fake-acp-process";

const controller = makeController();

vi.mock("node:child_process", async () => {
  const actual = await vi.importActual<typeof import("node:child_process")>(
    "node:child_process",
  );
  return {
    ...actual,
    spawn: createMockSpawn(controller),
  };
});

beforeEach(() => {
  controller.spawnCalls.length = 0;
  controller.setScript({});
});

const temporaryHermesHomes: string[] = [];

async function useHermesConfig(yaml: string): Promise<string> {
  const home = await mkdtemp(join(tmpdir(), "openneko-hermes-test-"));
  temporaryHermesHomes.push(home);
  await writeFile(join(home, "config.yaml"), yaml, "utf8");
  vi.stubEnv("HERMES_HOME", home);
  return home;
}

afterEach(async () => {
  vi.clearAllMocks();
  vi.unstubAllEnvs();
  await Promise.all(
    temporaryHermesHomes.splice(0).map((home) =>
      rm(home, { recursive: true, force: true }),
    ),
  );
});

const { HermesBackend, parseHermesSessionIdentity } = await import(
  "../src/agent-backends/hermes",
);

const FAKE_WORKSPACE = {
  orgRoot: "/tmp/neko-test/org",
  skillsRoot: "/tmp/neko-test/org/skills",
  memoryRoot: "/tmp/neko-test/org/memory",
  knowledgeRoot: "/tmp/neko-test/org/knowledge",
  uploadsRoot: "/tmp/neko-test/org/uploads",
  runsRoot: "/tmp/neko-test/org/runs",
  threadUploadsRoot: "/tmp/neko-test/org/uploads/t1",
  runRoot: "/tmp/neko-test/org/runs/r1",
  artifactRoot: "/tmp/neko-test/org/runs/r1/artifacts",
  binRoot: "/tmp/neko-test/org/runs/r1/bin",
} as const;

type SeenRequest = { method: string; params: unknown };

function captureRequests(): { seen: SeenRequest[]; record: (m: string, p: unknown) => void } {
  const seen: SeenRequest[] = [];
  return { seen, record: (method, params) => seen.push({ method, params }) };
}

describe("HermesBackend ACP behavior", () => {
  it("always uses session/new — never session/load — even when backendState carries a sessionKey", async () => {
    // Hermes ACP session/load replays prior history as session/update events.
    // The worker already injects history into the prompt
    // (packages/llm/src/work/prompt.ts:108), so session/load would
    // double-count context. We always start a fresh ACP session per turn.
    const cap = captureRequests();
    const sessionId = "sess-new-1";
    controller.setScript({
      responders: {
        "session/new": (p) => {
          cap.record("session/new", p);
          return { sessionId };
        },
        "session/load": (p) => {
          cap.record("session/load", p);
          return { sessionId: "should-never-fire" };
        },
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(chunkNotification(sessionId, "hi"));
          return { stopReason: "end_turn" };
        },
      },
    });
    const backend = new HermesBackend();

    // First turn: no sessionKey in state → session/new.
    await backend.run({ prompt: "p", workspace: FAKE_WORKSPACE });
    // Second turn: sessionKey present in state → STILL session/new, never load.
    await backend.run({
      prompt: "p2",
      workspace: FAKE_WORKSPACE,
      backendState: { hermes: { sessionKey: "old-key-from-prior-turn" } },
    });

    const newCalls = cap.seen.filter((s) => s.method === "session/new");
    const loadCalls = cap.seen.filter((s) => s.method === "session/load");
    expect(newCalls).toHaveLength(2);
    expect(loadCalls).toHaveLength(0);
  });

  it("does not write a sessionKey into backendState (no resumption to support)", async () => {
    const sessionId = "sess-no-persist";
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(chunkNotification(sessionId, "ok"));
          return { stopReason: "end_turn" };
        },
      },
    });
    const backend = new HermesBackend();
    const result = await backend.run({ prompt: "p", workspace: FAKE_WORKSPACE });
    // backendState round-trips unchanged — Hermes branch is empty.
    expect(result.backendState).toEqual({});
  });

  it("emits assistant message events as deltas: chunks 'a','b','c' → ['a','b','c']", async () => {
    const sessionId = "sess-deltas";
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(chunkNotification(sessionId, "a"));
          ctx.emitNotification(chunkNotification(sessionId, "b"));
          ctx.emitNotification(chunkNotification(sessionId, "c"));
          return { stopReason: "end_turn" };
        },
      },
    });
    const events: Array<{ type: string; content?: string }> = [];
    const backend = new HermesBackend();
    const result = await backend.run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (e) => {
        events.push(e as { type: string; content?: string });
      },
    });
    const messageContents = events
      .filter((e) => e.type === "message")
      .map((e) => e.content);
    expect(messageContents).toEqual(["a", "b", "c"]);
    expect(result.finalText).toBe("abc");
  });

  it("keeps configured identity separate from the live Hermes session identity", async () => {
    const sessionId = "sess-identity";
    controller.setScript({
      responders: {
        "session/new": () => ({
          sessionId,
          models: {
            currentModelId: "gemini:gemini-3.6-flash-observed",
          },
        }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(chunkNotification(sessionId, "answer"));
          return {
            stopReason: "end_turn",
            usage: {
              input_tokens: 12,
              outputTokens: 5,
              cachedReadTokens: 3,
              thought_tokens: 2,
              total_tokens: 17,
            },
          };
        },
      },
    });
    const events: Array<Record<string, unknown>> = [];
    const backend = new HermesBackend({
      provider: "google-gemini",
      model: "gemini-3.6-flash-configured",
    });

    await backend.run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (event) => events.push(event as unknown as Record<string, unknown>),
    });

    expect(backend.configuredIdentity).toEqual({
      provider: "google-gemini",
      model: "gemini-3.6-flash-configured",
    });
    expect(backend.model).toBe("gemini-3.6-flash-configured");
    expect(events.find((event) => event.type === "usage")).toEqual({
      type: "usage",
      source: "outer",
      provider: "google-gemini",
      model: "gemini-3.6-flash-observed",
      modelIdentity: {
        configured: {
          provider: "google-gemini",
          model: "gemini-3.6-flash-configured",
        },
        observed: {
          provider: "google-gemini",
          model: "gemini-3.6-flash-observed",
        },
      },
      usage: {
        inputTokens: 12,
        outputTokens: 5,
        cacheReadTokens: 3,
        reasoningTokens: 2,
        totalTokens: 17,
        coverage: "complete",
      },
    });
  });

  it("carries the Hermes per-turn cost from the prompt response meta", async () => {
    const sessionId = "sess-cost";
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(chunkNotification(sessionId, "answer"));
          return {
            stopReason: "end_turn",
            usage: { inputTokens: 2_000_000, outputTokens: 100_000, totalTokens: 2_100_000 },
            _meta: {
              openneko: {
                usage: {
                  api_calls: 7,
                  cost_usd: 1.875,
                  cost_status: "estimated",
                  cost_source: "official_docs_snapshot",
                  pricing_version: "google-pricing-2026-09-17",
                  unknown_cost_calls: 0,
                  provider: "gemini",
                  model: "gemini-3.7-flash",
                },
              },
            },
          };
        },
      },
    });
    const events: AgentEvent[] = [];
    await new HermesBackend().run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (event) => events.push(event),
    });

    expect(events.find((event) => event.type === "usage")).toMatchObject({
      usage: {
        inputTokens: 2_000_000,
        outputTokens: 100_000,
        estimatedCostUsd: 1.875,
        currency: "USD",
        costStatus: "estimated",
        costSource: "official_docs_snapshot",
        pricingCatalogVersion: "google-pricing-2026-09-17",
        coverage: "complete",
      },
    });
  });

  it("parses Hermes model choices without truncating qualified model ids", () => {
    expect(
      parseHermesSessionIdentity({
        models: { current_model_id: "custom:publisher:model-v2" },
      }),
    ).toEqual({ provider: "custom", model: "publisher:model-v2" });
    expect(
      parseHermesSessionIdentity({ models: { currentModelId: "missing-model:" } }),
    ).toBeUndefined();
  });

  it("does not relabel configured identity as observed when ACP omits model state", async () => {
    const sessionId = "sess-configured-only";
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(chunkNotification(sessionId, "answer"));
          return {
            stopReason: "end_turn",
            usage: { inputTokens: 2, outputTokens: 1, totalTokens: 3 },
          };
        },
      },
    });
    const events: Array<Record<string, unknown>> = [];

    await new HermesBackend({ provider: "gemini", model: "configured-model" }).run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (event) => events.push(event as unknown as Record<string, unknown>),
    });

    expect(events.find((event) => event.type === "usage")).toMatchObject({
      modelIdentity: {
        configured: { provider: "gemini", model: "configured-model" },
      },
    });
    expect(events.find((event) => event.type === "usage")).not.toHaveProperty(
      "provider",
    );
    expect(events.find((event) => event.type === "usage")).not.toHaveProperty(
      "model",
    );
  });

  it("preserves observed model identity when ACP omits usage", async () => {
    const sessionId = "sess-identity-without-usage";
    controller.setScript({
      responders: {
        "session/new": () => ({
          sessionId,
          models: { currentModelId: "gemini:gemini-3.6-flash" },
        }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(chunkNotification(sessionId, "answer"));
          return { stopReason: "end_turn" };
        },
      },
    });
    const events: Array<Record<string, unknown>> = [];

    await new HermesBackend({ provider: "google-gemini", model: "gemini-3.6-flash" }).run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (event) => events.push(event as unknown as Record<string, unknown>),
    });

    expect(events.find((event) => event.type === "usage")).toEqual({
      type: "usage",
      source: "outer",
      provider: "google-gemini",
      model: "gemini-3.6-flash",
      modelIdentity: {
        configured: { provider: "google-gemini", model: "gemini-3.6-flash" },
        observed: { provider: "google-gemini", model: "gemini-3.6-flash" },
      },
      usage: {
        coverage: "unavailable",
        missingReasons: ["Hermes ACP session/prompt omitted usage"],
      },
    });
  });

  it("fails a Hermes output-truncation sentinel without streaming it as an answer", async () => {
    const sessionId = "sess-output-truncated";
    controller.setScript({
      responders: {
        "session/new": () => ({
          sessionId,
          models: { currentModelId: "gemini:gemini-3.6-flash" },
        }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(
            chunkNotification(
              sessionId,
              "Response truncated due to output length limit",
            ),
          );
          return { stopReason: "max_tokens" };
        },
      },
    });
    const events: Array<Record<string, unknown>> = [];

    const result = await new HermesBackend({
      provider: "google-gemini",
      model: "gemini-3.6-flash",
    }).run({
      prompt: "p",
      retries: 0,
      workspace: FAKE_WORKSPACE,
      onEvent: (event) => events.push(event as unknown as Record<string, unknown>),
    });

    expect(result).toMatchObject({
      status: "failed",
      finalText: "",
      error:
        "hermes response truncated due to output length limit (stopReason=max_tokens)",
    });
    expect(events.filter((event) => event.type === "message")).toEqual([]);
    expect(events.find((event) => event.type === "usage")).toMatchObject({
      modelIdentity: {
        observed: { provider: "google-gemini", model: "gemini-3.6-flash" },
      },
      usage: { coverage: "unavailable" },
    });
  });

  it("retries a truncated turn after discovery calls, but never after a side-effecting tool", async () => {
    for (const [toolTitle, expectedCalls] of [["tool_describe", 2], ["write: /tmp/file", 1]] as const) {
      let promptCalls = 0;
      controller.setScript({
        responders: {
          "session/new": () => ({ sessionId: `sess-discovery-${promptCalls}` }),
          "session/prompt": (_p, ctx) => {
            promptCalls += 1;
            const sessionId = `sess-discovery-${promptCalls - 1}`;
            if (promptCalls === 1) {
              ctx.emitNotification(toolCallNotification(sessionId, "tc-search", { kind: "search", title: "search: *" }));
              ctx.emitNotification(toolCallUpdateNotification(sessionId, "tc-search", { rawOutput: "files" }));
              ctx.emitNotification(toolCallNotification(sessionId, "tc-1", { title: toolTitle }));
              ctx.emitNotification(toolCallUpdateNotification(sessionId, "tc-1", { rawOutput: "ok" }));
              ctx.emitNotification(chunkNotification(sessionId, "Response truncated due to output length limit"));
            } else {
              ctx.emitNotification(chunkNotification(sessionId, "Answer"));
            }
            return { stopReason: "end_turn" };
          },
        },
      });
      const messages: string[] = [];
      const result = await new HermesBackend().run({
        prompt: "p",
        retries: 1,
        workspace: FAKE_WORKSPACE,
        onEvent: (event) => {
          if (event.type === "message") messages.push(event.content);
        },
      });
      expect(promptCalls).toBe(expectedCalls);
      expect(result.status).toBe(expectedCalls === 2 ? "completed" : "failed");
      expect(messages).toEqual(expectedCalls === 2 ? ["Answer"] : []);
    }
  });

  it("keeps Hermes interim commentary separate from the final answer", async () => {
    const sessionId = "sess-interim";
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(
            interimNotification(sessionId, "I’ll inspect the sales records first."),
          );
          ctx.emitNotification(
            toolCallNotification(sessionId, "tc-sales", {
              kind: "read",
              title: "Inspect sales data",
            }),
          );
          ctx.emitNotification(
            toolCallUpdateNotification(sessionId, "tc-sales", {
              status: "completed",
              rawOutput: "ok",
            }),
          );
          ctx.emitNotification(
            chunkNotification(sessionId, "Revenue increased over the period."),
          );
          return { stopReason: "end_turn" };
        },
      },
    });
    const events: Array<Record<string, unknown>> = [];

    const result = await new HermesBackend().run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (event) => events.push(event as unknown as Record<string, unknown>),
    });

    expect(events.map((event) => event.type)).toEqual([
      "interim",
      "tool_start",
      "tool_end",
      "message",
    ]);
    expect(events[0]).toMatchObject({
      type: "interim",
      content: "I’ll inspect the sales records first.",
      source: "hermes_interim_assistant",
    });
    expect(result.finalText).toBe("Revenue increased over the period.");
  });

  it("emits Gemini thought summaries as same-call progress before tools and prose", async () => {
    await useHermesConfig([
      "model:",
      '  default: "gemini-3.6-flash"',
      '  provider: "gemini"',
      "agent:",
      '  reasoning_effort: "medium"',
      "",
    ].join("\n"));
    const sessionId = "sess-gemini-summaries";
    let promptCalls = 0;
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          promptCalls += 1;
          ctx.emitNotification(thoughtNotification(sessionId, "I’m checking "));
          ctx.emitNotification(thoughtNotification(sessionId, "the sales records."));
          ctx.emitNotification(
            toolCallNotification(sessionId, "tc-sales", {
              kind: "read",
              title: "Inspect sales data",
            }),
          );
          ctx.emitNotification(
            toolCallUpdateNotification(sessionId, "tc-sales", {
              status: "completed",
              rawOutput: "ok",
            }),
          );
          ctx.emitNotification(
            thoughtNotification(sessionId, "I found the top performers."),
          );
          ctx.emitNotification(chunkNotification(sessionId, "Here are the results."));
          return { stopReason: "end_turn" };
        },
      },
    });
    const events: Array<Record<string, unknown>> = [];

    await new HermesBackend().run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (event) => events.push(event as unknown as Record<string, unknown>),
    });

    expect(promptCalls).toBe(1);
    expect(events.map((event) => event.type)).toEqual([
      "progress",
      "tool_start",
      "tool_end",
      "progress",
      "message",
    ]);
    expect(events.filter((event) => event.type === "progress")).toEqual([
      {
        type: "progress",
        id: "google-gemini-summary-sess-gemini-summaries-1",
        content: "I’m checking the sales records.",
        source: "provider_summary",
        provider: "google-gemini",
      },
      {
        type: "progress",
        id: "google-gemini-summary-sess-gemini-summaries-2",
        content: "I found the top performers.",
        source: "provider_summary",
        provider: "google-gemini",
      },
    ]);
  });

  it("emits Anthropic summarized thinking as same-call progress", async () => {
    await useHermesConfig([
      "model:",
      '  default: "claude-sonnet-5"',
      '  provider: "anthropic"',
      "agent:",
      '  reasoning_effort: "medium"',
      "",
    ].join("\n"));
    const sessionId = "sess-anthropic-summaries";
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(thoughtNotification(sessionId, "Checking the quarterly totals."));
          ctx.emitNotification(
            toolCallNotification(sessionId, "tc-revenue", {
              kind: "read",
              title: "Query revenue",
            }),
          );
          ctx.emitNotification(
            toolCallUpdateNotification(sessionId, "tc-revenue", {
              status: "completed",
              rawOutput: "ok",
            }),
          );
          ctx.emitNotification(thoughtNotification(sessionId, "Comparing quarter-over-quarter changes."));
          ctx.emitNotification(chunkNotification(sessionId, "Public answer"));
          return { stopReason: "end_turn" };
        },
      },
    });
    const events: Array<Record<string, unknown>> = [];

    await new HermesBackend().run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (event) => events.push(event as unknown as Record<string, unknown>),
    });

    expect(events.map((event) => event.type)).toEqual([
      "progress",
      "tool_start",
      "tool_end",
      "progress",
      "message",
    ]);
    expect(events.filter((event) => event.type === "progress")).toEqual([
      {
        type: "progress",
        id: "anthropic-summary-sess-anthropic-summaries-1",
        content: "Checking the quarterly totals.",
        source: "provider_summary",
        provider: "anthropic",
      },
      {
        type: "progress",
        id: "anthropic-summary-sess-anthropic-summaries-2",
        content: "Comparing quarter-over-quarter changes.",
        source: "provider_summary",
        provider: "anthropic",
      },
    ]);
  });

  it("does not expose generic ACP reasoning streams from other providers", async () => {
    await useHermesConfig([
      "model:",
      '  default: "gpt-5.4"',
      '  provider: "openai"',
      "agent:",
      '  reasoning_effort: "medium"',
      "",
    ].join("\n"));
    const sessionId = "sess-private-reasoning";
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(thoughtNotification(sessionId, "private reasoning"));
          ctx.emitNotification(chunkNotification(sessionId, "Public answer"));
          return { stopReason: "end_turn" };
        },
      },
    });
    const events: Array<{ type: string }> = [];

    await new HermesBackend().run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (event) => events.push(event),
    });

    expect(events.map((event) => event.type)).toEqual(["message"]);
  });

  it("retries a content-free end_turn, then fails instead of completing empty", async () => {
    let promptCalls = 0;
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId: `sess-empty-${promptCalls}` }),
        "session/prompt": () => {
          promptCalls += 1;
          return { stopReason: "end_turn" };
        },
      },
    });
    const events: Array<{ type: string; message?: string }> = [];
    const backend = new HermesBackend();

    const result = await backend.run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      retries: 1,
      onEvent: (event) => {
        events.push(event as { type: string; message?: string });
      },
    });

    expect(promptCalls).toBe(2);
    expect(result).toMatchObject({
      status: "failed",
      finalText: "",
      error: expect.stringContaining("completed without assistant output"),
      errorCode: "empty_output",
    });
    expect(events).toContainEqual({ type: "retry", reason: "empty_output" });
    expect(events.filter((event) => event.type === "error")).toEqual([
      expect.objectContaining({
        message: expect.stringContaining("stopReason=end_turn"),
      }),
    ]);
  });

  it("awaits async event handlers in chunk order before returning", async () => {
    const sessionId = "sess-ordered-events";
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(chunkNotification(sessionId, "first"));
          ctx.emitNotification(chunkNotification(sessionId, "second"));
          return { stopReason: "end_turn" };
        },
      },
    });
    const contents: string[] = [];
    const backend = new HermesBackend();

    await backend.run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: async (event) => {
        if (event.type !== "message") return;
        if (event.content === "first") {
          await new Promise((resolve) => setTimeout(resolve, 20));
        }
        contents.push(event.content);
      },
    });

    expect(contents).toEqual(["first", "second"]);
  });

  it("emits tool_start then tool_end with matching toolCallId", async () => {
    const sessionId = "sess-tool";
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(
            toolCallNotification(sessionId, "tc-1", { kind: "read", title: "read: /tmp/x.txt", locations: [{ path: "/tmp/x.txt" }] }),
          );
          ctx.emitNotification(
            toolCallUpdateNotification(sessionId, "tc-1", { status: "completed", rawOutput: "file contents" }),
          );
          ctx.emitNotification(chunkNotification(sessionId, "Done"));
          return { stopReason: "end_turn" };
        },
      },
    });
    const events: Array<{ type: string; id?: string; result?: unknown; name?: string }> = [];
    const backend = new HermesBackend();
    await backend.run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (e) => {
        events.push(e as { type: string; id?: string });
      },
    });
    const toolStart = events.find((e) => e.type === "tool_start");
    const toolEnd = events.find((e) => e.type === "tool_end");
    expect(toolStart).toBeDefined();
    expect(toolStart?.id).toBe("tc-1");
    expect(toolStart?.name).toBe("read");
    expect(toolEnd).toBeDefined();
    expect(toolEnd?.id).toBe("tc-1");
    expect(toolEnd?.result).toBe("file contents");
  });

  it("emits a precise canonical MCP name with the actual tool arguments", async () => {
    const sessionId = "sess-mcp-tool-event";
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(
            toolCallNotification(sessionId, "tc-blueprint", {
              kind: "other",
              title: "mcp__neko__records_browse_blueprints",
              rawInput: { blueprint: "crm" },
            }),
          );
          ctx.emitNotification(
            toolCallUpdateNotification(sessionId, "tc-blueprint", {
              status: "completed",
              rawOutput: '{"blueprints":[]}',
            }),
          );
          return { stopReason: "end_turn" };
        },
      },
    });
    const events: AgentEvent[] = [];

    await new HermesBackend().run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (event) => events.push(event),
    });

    expect(events).toContainEqual({
      type: "tool_start",
      id: "tc-blueprint",
      name: "mcp_neko_records_browse_blueprints",
      input: { blueprint: "crm" },
    });
  });

  it("does not accept a legacy fence as a second live render path", async () => {
    const sessionId = "sess-surface";
    const fenced = "Here is the surface:\n```neko_a2ui\n[{\"version\":\"v0.9\",\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"urn:app:catalog:briefing:v1\"}}]\n```\nDone.";
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(chunkNotification(sessionId, fenced));
          return { stopReason: "end_turn" };
        },
      },
    });
    const events: Array<{ type: string }> = [];
    const backend = new HermesBackend();
    const result = await backend.run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (e) => {
        events.push(e);
      },
    });
    expect(events.find((e) => e.type === "surface")).toBeUndefined();
    expect(result.finalText).not.toContain("```neko_a2ui");
    expect(result.finalText).toContain("Done.");
  });

  it("leaves fence in finalText when onEvent is absent (sync callers parse it later)", async () => {
    const sessionId = "sess-surface-sync";
    const fenced = "Here is the surface:\n```neko_a2ui\n[]\n```";
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(chunkNotification(sessionId, fenced));
          return { stopReason: "end_turn" };
        },
      },
    });
    const backend = new HermesBackend();
    const result = await backend.run({ prompt: "p", workspace: FAKE_WORKSPACE });
    expect(result.finalText).toContain("```neko_a2ui");
  });

  it("AbortSignal triggers cancelled status and SIGTERM to process group", async () => {
    const sessionId = "sess-abort";
    let promptStarted = false;
    controller.setScript({
      staysOpen: true,
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": () => {
          promptStarted = true;
          return NO_RESPONSE;
        },
      },
    });
    const ctrl = new AbortController();
    const backend = new HermesBackend();
    const runPromise = backend.run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      signal: ctrl.signal,
    });
    await new Promise<void>((resolve) => {
      const tick = () => (promptStarted ? resolve() : setTimeout(tick, 5));
      tick();
    });
    ctrl.abort();
    const result = await runPromise;
    expect(result.status).toBe("cancelled");
  });

  it("timeout settles with failed status and error event", async () => {
    const sessionId = "sess-timeout";
    controller.setScript({
      staysOpen: true,
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": () => NO_RESPONSE,
      },
    });
    const events: Array<{ type: string; message?: string }> = [];
    const backend = new HermesBackend();
    const result = await backend.run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      timeoutMs: 50,
      onEvent: (e) => {
        events.push(e as { type: string; message?: string });
      },
    });
    expect(result).toMatchObject({ status: "failed", errorCode: "timeout", timedOut: true });
    expect(events.find((e) => e.type === "error")).toBeDefined();
  });

  it("JSON-RPC error response from session/prompt yields failed status with error message", async () => {
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId: "sess-error" }),
        "session/prompt": () => {
          const err: Error & { code?: number } = new Error("Provider gemini returned 503");
          err.code = -32603;
          throw err;
        },
      },
    });
    const events: Array<{ type: string; message?: string }> = [];
    const backend = new HermesBackend();
    const result = await backend.run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      onEvent: (e) => {
        events.push(e as { type: string; message?: string });
      },
    });
    expect(result).toMatchObject({ status: "failed", errorCode: "protocol" });
    expect(result.error).toContain("Provider gemini returned 503");
    expect(events.find((e) => e.type === "error")).toBeDefined();
  });

  it("web turn: mounts the one brokered render server and suppresses an accepted render pill", async () => {
    const previousBridge = process.env.OPENNEKO_MCP_BRIDGE;
    process.env.OPENNEKO_MCP_BRIDGE = "/app/mcp-bridge.js";
    const cap = captureRequests();
    const sessionId = "sess-render";
    const cards = {
      title: "Test",
      keyFigures: ["Test: 42"],
    };
    controller.setScript({
      responders: {
        "session/new": (p) => { cap.record("session/new", p); return { sessionId }; },
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification({
            jsonrpc: "2.0",
            method: "session/update",
            params: {
              sessionId,
              update: {
                sessionUpdate: "tool_call",
                toolCallId: "tc-render",
                kind: "other",
                title: "mcp_neko_ui_render_cards",
                rawInput: cards,
              },
            },
          });
          ctx.emitNotification(
            toolCallUpdateNotification(sessionId, "tc-render", {
              status: "completed",
              content: "Cards shown.",
            }),
          );
          return { stopReason: "end_turn" };
        },
      },
    });
    try {
      const events: Array<{ type: string; messages?: unknown[] }> = [];
      const backend = new HermesBackend();
      await backend.run({
        prompt: "p",
        workspace: FAKE_WORKSPACE,
        wantsCards: true,
        mcpServers: { neko_ui: {} as never },
        mcpBridgeEnv: { OPENNEKO_MCP_ORG_ID: "org-1" },
        onEvent: (e) => { events.push(e as { type: string; messages?: unknown[] }); },
      });

      const sn = cap.seen.find((s) => s.method === "session/new");
      expect(JSON.stringify(sn?.params)).toContain(
        "/app/mcp-bridge.js neko_ui",
      );
      // The neko_ui server emits the surface through the broker event sink;
      // Hermes only classifies its ACP notification and hides the tool pill.
      expect(events.some((e) => e.type === "tool_start")).toBe(false);
      expect(events.some((e) => e.type === "surface")).toBe(false);
    } finally {
      if (previousBridge === undefined) delete process.env.OPENNEKO_MCP_BRIDGE;
      else process.env.OPENNEKO_MCP_BRIDGE = previousBridge;
    }
  });

  it("preserves a rejected render input in tool_start telemetry", async () => {
    const sessionId = "sess-render-rejected";
    const rejectedInput = {
      messages: [{ version: "v1.0", createSurface: { surfaceId: "broken" } }],
    };
    controller.setScript({
      responders: {
        "session/new": () => ({ sessionId }),
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification({
            jsonrpc: "2.0",
            method: "session/update",
            params: {
              sessionId,
              update: {
                sessionUpdate: "tool_call",
                toolCallId: "tc-render-rejected",
                kind: "other",
                title: "mcp_neko_ui_render_cards",
                rawInput: rejectedInput,
              },
            },
          });
          ctx.emitNotification(
            toolCallUpdateNotification(sessionId, "tc-render-rejected", {
              status: "failed",
              content: "Invalid tool arguments",
            }),
          );
          ctx.emitNotification(chunkNotification(sessionId, "I could not render that surface."));
          return { stopReason: "end_turn" };
        },
      },
    });
    const events: Array<Record<string, unknown>> = [];
    await new HermesBackend().run({
      prompt: "p",
      workspace: FAKE_WORKSPACE,
      wantsCards: true,
      onEvent: (event) => events.push(event as unknown as Record<string, unknown>),
    });

    const start = events.find((event) => event.type === "tool_start");
    expect(start).toMatchObject({
      id: "tc-render-rejected",
      name: "render_cards",
      input: {
        title: "mcp_neko_ui_render_cards",
        rawInput: rejectedInput,
      },
    });
    expect(
      (start?.input as { validationIssues?: unknown[] }).validationIssues,
    ).not.toHaveLength(0);
    expect(events).toContainEqual(
      expect.objectContaining({
        type: "tool_end",
        id: "tc-render-rejected",
        error: "Invalid tool arguments",
      }),
    );
  });

  it("non-web turn: does not offer the render MCP server", async () => {
    const cap = captureRequests();
    controller.setScript({
      responders: {
        "session/new": (p) => { cap.record("session/new", p); return { sessionId: "s" }; },
        "session/prompt": (_p, ctx) => {
          ctx.emitNotification(chunkNotification("s", "hi"));
          return { stopReason: "end_turn" };
        },
      },
    });
    const backend = new HermesBackend();
    await backend.run({ prompt: "p", workspace: FAKE_WORKSPACE }); // wantsCards defaults false
    const sn = cap.seen.find((s) => s.method === "session/new");
    expect((sn?.params as { mcpServers?: unknown[] }).mcpServers).toEqual([]);
    expect(JSON.stringify(sn?.params)).not.toContain("neko_render");
  });

  it("mounts all logical OpenNeko tools through one multiplexed MCP child", async () => {
    const previousBridge = process.env.OPENNEKO_MCP_BRIDGE;
    process.env.OPENNEKO_MCP_BRIDGE = "/app/mcp-bridge.js";
    const cap = captureRequests();
    controller.setScript({
      responders: {
        "session/new": (p) => {
          cap.record("session/new", p);
          return { sessionId: "s-multiplexed" };
        },
      },
    });

    try {
      const backend = new HermesBackend();
      await backend.run({
        prompt: "p",
        workspace: FAKE_WORKSPACE,
        mcpServers: {
          neko_memory: {} as never,
          neko_records: {} as never,
          neko_ui: {} as never,
        },
        wantsCards: true,
        mcpBridgeEnv: { OPENNEKO_MCP_ORG_ID: "org-1" },
      });
      const sn = cap.seen.find((s) => s.method === "session/new");
      const mounted = (sn?.params as {
        mcpServers?: Array<{ name: string; args: string[] }>;
      }).mcpServers;
      expect(mounted).toHaveLength(1);
      expect(mounted?.[0]?.name).toBe("neko");
      expect(mounted?.[0]?.args.join(" ")).toContain(
        "/app/mcp-bridge.js neko_memory,neko_records,neko_ui",
      );
    } finally {
      if (previousBridge === undefined) delete process.env.OPENNEKO_MCP_BRIDGE;
      else process.env.OPENNEKO_MCP_BRIDGE = previousBridge;
    }
  });
});


it("delivers the compacted summary to ACP and retains usage before tool-boundary cancellation", async () => {
  const { buildWorkPrompt } = await import("../src/work/prompt");
  const { compactIfNeeded } = await import("../src/work/compact-transcript");
  const summary = "The operator selected AW-RESUME-CODE-7Q4M as the exact resume code.";
  const messages = [{ id: "watermark", role: "user" as const, content: "Earlier decision was compacted." },
    ...Array.from({ length: 12 }, (_, i) => ({ id: `m${i}`, role: "user" as const, content: `Unrelated turn ${i}` }))];
  const compacted = await compactIfNeeded({ messages, prior: { summary, throughMessageId: "watermark", version: 1, updatedAt: "2026-09-13" }, now: "2026-09-13" });
  const prompt = buildWorkPrompt({ backend: "hermes", workspace: FAKE_WORKSPACE,
    knowledge: { mode: "legacy", tables: "{}", namespaces: "{}", insights: "{}", syntax: "{}" },
    messages: compacted.kept, priorSummary: compacted.summary,
    currentUserMessage: "What exact resume code did we choose earlier?", inlineTranscript: true,
    supportsCardTool: false, supportsSkillTool: false, supportsMemoryTool: false,
    supportsWorkflowTool: false, supportsPolicyTool: false, supportsSourceConfigTool: false,
    supportsNativeDelegation: false,
  });
  const abort = new AbortController();
  const events: AgentEvent[] = [];
  let receivedPrompt = "";
  controller.setScript({ responders: {
    "session/new": () => ({ sessionId: "s" }),
    "session/prompt": (params, ctx) => {
      receivedPrompt = JSON.stringify(params);
      ctx.emitNotification({ jsonrpc: "2.0", method: "session/update", params: { sessionId: "s", update: {
        sessionUpdate: "tool_call", toolCallId: "t", title: "terminal", kind: "execute",
        _meta: { openneko: { usage: { input_tokens: 120, output_tokens: 30, total_tokens: 150 } } },
      } } });
      return NO_RESPONSE;
    },
  } });
  await new HermesBackend().run({ prompt, userMessage: "What exact resume code did we choose earlier?", workspace: FAKE_WORKSPACE, signal: abort.signal,
    onEvent: (event) => { events.push(event); if (event.type === "tool_start") abort.abort(); },
  });
  expect(receivedPrompt).toContain(summary);
  expect(receivedPrompt).toContain("Unrelated turn 11");
  expect(receivedPrompt).not.toContain("Earlier decision was compacted.");
  expect(events.find(event => event.type === "tool_start")).toMatchObject({ usageSnapshot: { inputTokens: 120, outputTokens: 30, totalTokens: 150 } });
  expect(events.filter(event => event.type === "usage")).toEqual([
    expect.objectContaining({ source: "outer", usage: expect.objectContaining({ inputTokens: 120, outputTokens: 30, coverage: "partial" }) }),
  ]);
});

it("reports the last usage snapshot when a turn times out", async () => {
  const events: AgentEvent[] = [];
  controller.setScript({ staysOpen: true, responders: {
    "session/new": () => ({ sessionId: "s-timeout" }),
    "session/prompt": (_params, ctx) => {
      ctx.emitNotification({ jsonrpc: "2.0", method: "session/update", params: { sessionId: "s-timeout", update: {
        sessionUpdate: "tool_call", toolCallId: "t", title: "terminal", kind: "execute",
        _meta: { openneko: { usage: { input_tokens: 900, output_tokens: 40, cost_usd: 0.42, cost_status: "estimated" } } },
      } } });
      return NO_RESPONSE;
    },
  } });
  const result = await new HermesBackend().run({ prompt: "p", workspace: FAKE_WORKSPACE, timeoutMs: 200, onEvent: (event) => { events.push(event); } });
  expect(result.status).toBe("failed");
  expect(events.filter(event => event.type === "usage")).toEqual([
    expect.objectContaining({
      source: "outer",
      usage: expect.objectContaining({ inputTokens: 900, estimatedCostUsd: 0.42, coverage: "partial" }),
    }),
  ]);
});
