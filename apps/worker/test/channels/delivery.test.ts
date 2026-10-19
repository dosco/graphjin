import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// delivery.ts wires several cross-package seams; mock them so the dispatch
// logic (idempotency + routing), durable delivery, and inbound dedup are
// tested in isolation.
vi.mock("@neko/llm/workflows", () => ({
  getActionRequest: vi.fn(),
  approveActionRequest: vi.fn(async () => ({})),
  rejectActionRequest: vi.fn(async () => ({})),
  setWorkflowOutputDeliveryHook: vi.fn(),
}));
vi.mock("@neko/db/jobs", () => ({
  enqueue: vi.fn(async () => "job-1"),
  QUEUE: {
    ACTION_EXECUTE: "action_execute",
    WORK_RUN: "work_run",
    CHANNEL_DELIVER: "channel_deliver",
  },
}));
const h = vi.hoisted(() => ({
  binds: [] as Array<Record<string, unknown>>,
  selectRows: [] as unknown[],
  audience: vi.fn(async (_org: string, audience: string, _run: string) => audience !== "finance"),
}));
vi.mock("@neko/db", () => ({
  db: vi.fn(() => ({
    insert: () => ({
      values: (v: Record<string, unknown>) => {
        h.binds.push(v);
        return { returning: async () => [{ id: "pj-1" }] };
      },
    }),
    select: () => ({
      from: () => ({
        // Awaitable (onOutput awaits .where() directly) AND chainable with
        // .limit() (ensureInboundBinding uses .where().limit(1)).
        where: () =>
          Object.assign(Promise.resolve(h.selectRows), {
            limit: async () => h.selectRows,
          }),
      }),
    }),
  })),
  and: (...a: unknown[]) => a,
  eq: (...a: unknown[]) => a,
  delivery_binding: { org_id: {}, enabled: {}, channel_plugin: {}, id: {} },
  processing_job: { id: {} },
}));
vi.mock("../../src/channels/audience", () => ({ audienceReceivesOutput: h.audience }));
vi.mock("@neko/llm", () => ({ resolveAgentBackend: vi.fn(async () => ({ id: "hermes" })) }));
vi.mock("@neko/llm/work", () => ({
  createWorkThread: vi.fn(async () => ({ id: "thread-1" })),
  getOrCreateChannelThread: vi.fn(async () => ({ id: "thread-1" })),
  createWorkRun: vi.fn(async () => ({ id: "run-1" })),
  createWorkMessage: vi.fn(async () => ({ id: "msg-1" })),
  channelThreadId: vi.fn(() => "tid-1"),
  getWorkThread: vi.fn(async () => null),
}));
vi.mock("@neko/llm/interaction", () => ({
  outputRowToInteractionEvent: (r: unknown) => ({ kind: "inform", ...(r as object) }),
}));
vi.mock("../../src/channels/inbound-store.js", () => ({
  inboundUpdateKey: vi.fn(() => "update-key"),
  beginInboundUpdate: vi.fn(async () => ({ proceed: true, attempts: 0, dead: false })),
  markInboundDone: vi.fn(async () => {}),
  recordInboundFailure: vi.fn(async () => ({ dead: false, attempts: 1 })),
}));
vi.mock("../../src/channels/runtime.js", () => ({
  deliverOnChannel: vi.fn(),
  parseChannelInbound: vi.fn(),
  verifyChannelInbound: vi.fn(),
}));

import { enqueue } from "@neko/db/jobs";
import {
  approveActionRequest,
  getActionRequest,
  rejectActionRequest,
  setWorkflowOutputDeliveryHook,
} from "@neko/llm/workflows";
import { createWorkRun, getOrCreateChannelThread, getWorkThread } from "@neko/llm/work";
import {
  conversationKeyFor,
  deliverChatReply,
  dispatchInboundIntent,
  ensureInboundBinding,
  ingestInboundWebhook,
  registerChannelOutputDelivery,
  runChannelDelivery,
} from "../../src/channels/delivery";
import {
  beginInboundUpdate,
  markInboundDone,
  recordInboundFailure,
} from "../../src/channels/inbound-store.js";
import {
  deliverOnChannel,
  parseChannelInbound,
  verifyChannelInbound,
} from "../../src/channels/runtime.js";

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(beginInboundUpdate).mockResolvedValue({ proceed: true, attempts: 0, dead: false });
  vi.mocked(recordInboundFailure).mockResolvedValue({ dead: false, attempts: 1 });
});
afterEach(() => vi.clearAllMocks());

describe("dispatchInboundIntent — decision", () => {
  it("approve on a pending request approves + enqueues ACTION_EXECUTE", async () => {
    vi.mocked(getActionRequest).mockResolvedValue({ id: "ar-1", status: "pending_approval", kind: "k" } as never);
    await dispatchInboundIntent("org-1", { kind: "decision", decisionRef: "ar-1", choice: "approve" });
    expect(approveActionRequest).toHaveBeenCalledWith({
      id: "ar-1",
      orgId: "org-1",
      approverUserId: null,
      approver: { userId: null, role: "member" },
    });
    expect(enqueue).toHaveBeenCalledWith("action_execute", { orgId: "org-1", actionRequestId: "ar-1" });
  });

  it("is idempotent — an already-executed request is NOT re-approved or re-enqueued", async () => {
    vi.mocked(getActionRequest).mockResolvedValue({ id: "ar-1", status: "executed", kind: "k" } as never);
    await dispatchInboundIntent("org-1", { kind: "decision", decisionRef: "ar-1", choice: "approve" });
    expect(approveActionRequest).not.toHaveBeenCalled();
    expect(enqueue).not.toHaveBeenCalled();
  });

  it("reject on a pending request rejects it", async () => {
    vi.mocked(getActionRequest).mockResolvedValue({ id: "ar-1", status: "pending_approval", kind: "k" } as never);
    await dispatchInboundIntent("org-1", { kind: "decision", decisionRef: "ar-1", choice: "reject", reason: "nope" });
    expect(rejectActionRequest).toHaveBeenCalledWith({
      id: "ar-1",
      orgId: "org-1",
      approverUserId: null,
      reason: "nope",
      approver: { userId: null, role: "member" },
    });
    expect(enqueue).not.toHaveBeenCalled();
  });

  it("a decision for an unknown action_request is a no-op", async () => {
    vi.mocked(getActionRequest).mockResolvedValue(undefined as never);
    await dispatchInboundIntent("org-1", { kind: "decision", decisionRef: "ghost", choice: "approve" });
    expect(approveActionRequest).not.toHaveBeenCalled();
    expect(rejectActionRequest).not.toHaveBeenCalled();
  });
});

describe("dispatchInboundIntent — utterance", () => {
  it("starts a chat run (thread + WORK_RUN enqueue) carrying the channel + recipient", async () => {
    await dispatchInboundIntent(
      "org-1",
      { kind: "utterance", text: "how are sales?", threadRef: "42" },
      "telegram",
      { chatId: 7 },
    );
    expect(getOrCreateChannelThread).toHaveBeenCalled();
    expect(enqueue).toHaveBeenCalledWith(
      "work_run",
      expect.objectContaining({
        orgId: "org-1",
        message: "how are sales?",
        runId: "run-1",
        threadId: "thread-1",
        channel: "telegram",
        channelPlugin: "telegram",
        recipient: { chatId: 7 },
      }),
    );
  });
});

describe("dispatchInboundIntent — spend budget", () => {
  it("replies with the budget message and starts no run when a budget is full", async () => {
    const { SpendBudgetExceeded } = await import("@neko/llm/spend");
    vi.mocked(createWorkRun).mockRejectedValueOnce(
      new SpendBudgetExceeded("org_daily", 500, 499, 5, 3600, new Date("2026-09-18T00:00:00Z")),
    );
    await dispatchInboundIntent(
      "org-1",
      { kind: "utterance", text: "how are sales?", threadRef: "42" },
      "telegram",
      { chatId: 7 },
    );
    expect(enqueue).not.toHaveBeenCalledWith("work_run", expect.anything());
    expect(enqueue).toHaveBeenCalledWith(
      "channel_deliver",
      expect.objectContaining({
        channelPlugin: "telegram",
        recipient: { chatId: 7 },
        events: [
          expect.objectContaining({
            kind: "converse",
            text: "The daily spending limit of $500.00 for this organization is reached. Try again after 00:00 UTC.",
          }),
        ],
      }),
      expect.anything(),
    );
  });
});

describe("dispatchInboundIntent — select (continuation fallback)", () => {
  it("continues the conversation with the chosen option", async () => {
    await dispatchInboundIntent(
      "org-1",
      { kind: "select", ref: "ar-9", optionId: "Option B" },
      "telegram",
      { chatId: 7 },
    );
    expect(getOrCreateChannelThread).toHaveBeenCalled();
    expect(enqueue).toHaveBeenCalledWith(
      "work_run",
      expect.objectContaining({
        message: "Option B",
        channelPlugin: "telegram",
      }),
    );
  });
});

describe("dispatchInboundIntent — channel thread-reply gate (W3.2)", () => {
  it("drops a requireThread reply when no matching thread exists", async () => {
    vi.mocked(getWorkThread).mockResolvedValueOnce(null);
    await dispatchInboundIntent(
      "org-1",
      { kind: "utterance", text: "and last quarter?", threadRef: "171.5" },
      "slack",
      { kind: "slack", channel: "C9", thread_ts: "171.5", requireThread: true },
    );
    expect(getOrCreateChannelThread).not.toHaveBeenCalled();
    expect(enqueue).not.toHaveBeenCalled();
  });

  it("continues a requireThread reply when the thread already exists", async () => {
    vi.mocked(getWorkThread).mockResolvedValueOnce({ id: "thread-1" } as never);
    await dispatchInboundIntent(
      "org-1",
      { kind: "utterance", text: "and last quarter?", threadRef: "171.5" },
      "slack",
      { kind: "slack", channel: "C9", thread_ts: "171.5", requireThread: true },
    );
    expect(getOrCreateChannelThread).toHaveBeenCalled();
    expect(enqueue).toHaveBeenCalledWith(
      "work_run",
      expect.objectContaining({ message: "and last quarter?" }),
    );
  });
});

describe("conversationKeyFor", () => {
  it("keys a Slack DM by channel and a thread by channel:threadRef", () => {
    expect(conversationKeyFor({ kind: "slack", channel: "D9" })).toBe("D9");
    expect(conversationKeyFor({ kind: "slack", channel: "C9" }, "171.5")).toBe("C9:171.5");
  });

  it("keys Telegram by chatId and falls back to recipient address", () => {
    expect(conversationKeyFor({ chatId: 7 }, "42")).toBe("7:42");
    expect(conversationKeyFor({ to: "+15550001111" })).toBe("+15550001111");
  });

  it("returns empty when the recipient has no stable address", () => {
    expect(conversationKeyFor({})).toBe("");
    expect(conversationKeyFor({ foo: "bar" }, "42")).toBe("");
  });
});

describe("runChannelDelivery (durable CHANNEL_DELIVER job body)", () => {
  it("delivers via the built-in channel and resolves on delivered=true", async () => {
    vi.mocked(deliverOnChannel).mockResolvedValue({ delivered: true, ref: "299" });
    await runChannelDelivery({
      orgId: "org-1",
      channelPlugin: "telegram",
      recipient: { chatId: 7 },
      events: [{ kind: "converse", text: "hi" }],
    });
    expect(deliverOnChannel).toHaveBeenCalledWith(
      "org-1",
      "telegram",
      { chatId: 7 },
      [{ kind: "converse", text: "hi" }],
    );
  });

  it("THROWS when the channel reports delivered=false (so pg-boss retries)", async () => {
    vi.mocked(deliverOnChannel).mockResolvedValue({ delivered: false });
    await expect(
      runChannelDelivery({ orgId: "o", channelPlugin: "p", recipient: {}, events: [] }),
    ).rejects.toThrow(/delivered=false/);
  });

  it("THROWS when the channel is not turned on (retry rather than drop)", async () => {
    vi.mocked(deliverOnChannel).mockRejectedValue(new Error("channel p is not turned on"));
    await expect(
      runChannelDelivery({ orgId: "o", channelPlugin: "p", recipient: {}, events: [] }),
    ).rejects.toThrow(/not turned on/);
  });
});

describe("deliverChatReply (channel reply path)", () => {
  it("enqueues a durable converse delivery keyed by run id with backoff opts", async () => {
    await deliverChatReply("org-1", "telegram", { chatId: 7 }, "run-9", "We have 290 employees.");
    expect(enqueue).toHaveBeenCalledTimes(1);
    const [queue, payload, opts] = vi.mocked(enqueue).mock.calls[0];
    expect(queue).toBe("channel_deliver");
    expect(payload).toMatchObject({
      orgId: "org-1",
      channelPlugin: "telegram",
      recipient: { chatId: 7 },
      events: [{ kind: "converse", id: "run-9", role: "assistant", text: "We have 290 employees." }],
    });
    expect(opts).toMatchObject({
      singletonKey: "reply-run-9",
      retryLimit: 8,
      retryDelay: 15,
      retryBackoff: true,
    });
  });

  it("does not enqueue an empty reply", async () => {
    await deliverChatReply("org-1", "p", {}, "run-9", "   ");
    expect(enqueue).not.toHaveBeenCalled();
  });
});

describe("output → channel fan-out (registerChannelOutputDelivery)", () => {
  it("enqueues one durable delivery per enabled binding, keyed by output+plugin", async () => {
    h.selectRows = [
      { channel_plugin: "telegram", recipient: { chatId: 1 } },
      { channel_plugin: "@open-neko/channel-slack", recipient: { chatId: 2 } },
      { channel_plugin: "@open-neko/channel-slack", recipient: { chatId: 3 }, audience: "finance" },
    ];
    registerChannelOutputDelivery();
    const hook = vi.mocked(setWorkflowOutputDeliveryHook).mock.calls[0][0];
    await hook("org-1", { id: "o1", workflowRunId: "wr-1", title: "Sales", body: "up 4%", mood: "good" } as never);
    expect(h.audience).toHaveBeenCalledWith("org-1", "finance", "wr-1");
    expect(enqueue).toHaveBeenCalledTimes(2);
    const keys = vi.mocked(enqueue).mock.calls.map((c) => (c[2] as { singletonKey: string }).singletonKey);
    expect(keys).toEqual([
      "output-o1-telegram",
      "output-o1-@open-neko/channel-slack",
    ]);
    expect(vi.mocked(enqueue).mock.calls.every((c) => c[0] === "channel_deliver")).toBe(true);
  });

  it("no bindings → no deliveries", async () => {
    h.selectRows = [];
    registerChannelOutputDelivery();
    const hook = vi.mocked(setWorkflowOutputDeliveryHook).mock.calls[0][0];
    await hook("org-1", { id: "o1", title: "t", body: "b" } as never);
    expect(enqueue).not.toHaveBeenCalled();
  });
});

describe("ingestInboundWebhook", () => {
  const fakeReg = {
    verifyInbound: vi.mocked(verifyChannelInbound),
    parseInbound: vi.mocked(parseChannelInbound),
  };

  beforeEach(() => {
    fakeReg.verifyInbound.mockResolvedValue(true);
    fakeReg.parseInbound.mockResolvedValue({
      intents: [{ kind: "decision", decisionRef: "ar-1", choice: "approve" }],
    });
    vi.mocked(getActionRequest).mockResolvedValue({ id: "ar-1", status: "pending_approval", kind: "k" } as never);
  });

  it("verifies, parses, dispatches, and marks done when the signature is valid", async () => {
    const res = await ingestInboundWebhook("org-1", "telegram", { "x-secret": "s" }, '{"callback_query":{}}');
    expect(res).toEqual({ ok: true, dispatched: 1 });
    expect(fakeReg.verifyInbound).toHaveBeenCalled();
    expect(fakeReg.parseInbound).toHaveBeenCalled();
    expect(approveActionRequest).toHaveBeenCalled();
    expect(markInboundDone).toHaveBeenCalled();
  });

  it("DEDUPES a duplicate update — begin says skip ⇒ no parse, no dispatch, still ok", async () => {
    vi.mocked(beginInboundUpdate).mockResolvedValue({ proceed: false, attempts: 1, dead: false });
    const res = await ingestInboundWebhook("org-1", "telegram", { "x-secret": "s" }, '{"callback_query":{}}');
    expect(res).toEqual({ ok: true, dispatched: 1 });
    expect(fakeReg.parseInbound).not.toHaveBeenCalled();
    expect(approveActionRequest).not.toHaveBeenCalled();
  });

  it("SKIPS a dead-lettered update — never re-dispatches a poison message", async () => {
    vi.mocked(beginInboundUpdate).mockResolvedValue({ proceed: false, attempts: 30, dead: true });
    const res = await ingestInboundWebhook("org-1", "telegram", { "x-secret": "s" }, '{"callback_query":{}}');
    expect(res).toEqual({ ok: true, dispatched: 1 });
    expect(fakeReg.parseInbound).not.toHaveBeenCalled();
  });

  it("records the failure + reports ok:false while still retrying (provider can redeliver)", async () => {
    fakeReg.parseInbound.mockRejectedValue(new Error("VM down"));
    vi.mocked(recordInboundFailure).mockResolvedValue({ dead: false, attempts: 3 });
    const res = await ingestInboundWebhook("org-1", "telegram", { "x-secret": "s" }, '{"callback_query":{}}');
    expect(res).toEqual({ ok: false, dispatched: 0 });
    expect(recordInboundFailure).toHaveBeenCalledWith(
      "org-1",
      "telegram",
      "update-key",
      30,
      expect.anything(),
      "VM down",
    );
  });

  it("DEAD-LETTERS after the attempt cap ⇒ consumed (ok:true), not retried again", async () => {
    fakeReg.parseInbound.mockRejectedValue(new Error("still broken"));
    vi.mocked(recordInboundFailure).mockResolvedValue({ dead: true, attempts: 30 });
    const res = await ingestInboundWebhook("org-1", "telegram", { "x-secret": "s" }, '{"callback_query":{}}');
    expect(res).toEqual({ ok: true, dispatched: 1 });
  });

  it("refuses (no parse/dispatch) when verification fails", async () => {
    fakeReg.verifyInbound.mockResolvedValue(false);
    const res = await ingestInboundWebhook("org-1", "telegram", {}, "{}");
    expect(res).toEqual({ ok: false, dispatched: 0 });
    expect(fakeReg.parseInbound).not.toHaveBeenCalled();
    expect(approveActionRequest).not.toHaveBeenCalled();
  });

  it("returns ok:false for an unknown channel without verifying", async () => {
    const res = await ingestInboundWebhook("org-1", "discord", {}, "{}");
    expect(res).toEqual({ ok: false, dispatched: 0 });
    expect(fakeReg.verifyInbound).not.toHaveBeenCalled();
  });

  it("accepts the legacy plugin package name in the webhook path", async () => {
    const res = await ingestInboundWebhook("org-1", "@open-neko/channel-telegram", { "x-secret": "s" }, '{"callback_query":{}}');
    expect(res).toEqual({ ok: true, dispatched: 1 });
    expect(fakeReg.verifyInbound).toHaveBeenCalledWith("org-1", "telegram", { "x-secret": "s" }, '{"callback_query":{}}');
    expect(fakeReg.parseInbound).toHaveBeenCalledWith("telegram", { callback_query: {} });
  });
});

describe("ensureInboundBinding (auto-bind on first inbound)", () => {
  beforeEach(() => {
    h.binds.length = 0;
    h.selectRows = [];
  });

  it("writes a binding when none exists for (org, channel)", async () => {
    await ensureInboundBinding("org-1", "telegram", { kind: "telegram", chatId: 99 });
    expect(h.binds).toHaveLength(1);
    expect(h.binds[0]).toMatchObject({
      org_id: "org-1",
      channel_plugin: "telegram",
      recipient: { kind: "telegram", chatId: 99 },
      enabled: true,
    });
  });

  it("is idempotent — skips writing when a binding already exists", async () => {
    h.selectRows = [{ id: "b1" }];
    await ensureInboundBinding("org-1", "telegram", { kind: "telegram", chatId: 99 });
    expect(h.binds).toHaveLength(0);
  });
});

describe("deliverChatReply threads the a2ui surface as converse.enrichment", () => {
  const deliveredEvent = (): Record<string, unknown> => {
    const call = vi.mocked(enqueue).mock.calls.find(([q]) => q === "channel_deliver");
    expect(call).toBeDefined();
    const payload = call![1] as { events: Array<Record<string, unknown>> };
    return payload.events[0]!;
  };

  it("attaches the agent's surface so a rich channel can render it", async () => {
    vi.mocked(enqueue).mockClear();
    const surfaces = [{ version: "v0.9", createSurface: { surfaceId: "s1", catalogId: "x" } }];
    await deliverChatReply("org-1", "telegram", { chatId: 1 }, "run-1", "hi", surfaces);
    const ev = deliveredEvent();
    expect(ev.kind).toBe("converse");
    expect(ev.text).toBe("hi");
    expect((ev.enrichment as { surfaces: unknown[] }).surfaces).toEqual(surfaces);
  });

  it("delivers plain text with no enrichment when there is no surface", async () => {
    vi.mocked(enqueue).mockClear();
    await deliverChatReply("org-1", "p", { chatId: 1 }, "run-2", "just text");
    const ev = deliveredEvent();
    expect(ev.kind).toBe("converse");
    expect(ev.text).toBe("just text");
    expect(ev.enrichment).toBeUndefined();
  });

  it("delivers a surface-only reply even when the text body is empty", async () => {
    vi.mocked(enqueue).mockClear();
    const surfaces = [{ version: "v0.9", updateComponents: { surfaceId: "s1", components: [] } }];
    await deliverChatReply("org-1", "p", { chatId: 1 }, "run-3", "", surfaces);
    expect(vi.mocked(enqueue).mock.calls.some(([q]) => q === "channel_deliver")).toBe(true);
  });

  it("drops an empty reply with no surface", async () => {
    vi.mocked(enqueue).mockClear();
    await deliverChatReply("org-1", "p", { chatId: 1 }, "run-4", "   ");
    expect(vi.mocked(enqueue).mock.calls.some(([q]) => q === "channel_deliver")).toBe(false);
  });
});
