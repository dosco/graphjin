// CH4 — the Slack channel surface: projection, inbound normalization
// (incl. CH1 sender capture), request-signature verification, delivery.

import { createHmac } from "node:crypto";
import { describe, expect, it } from "vitest";
import { SLACK_PROFILE, type CapabilityProfile } from "@neko/interaction";
import {
  deliverSlack,
  parseSlackUpdate,
  slackProjection as projectSlack,
  verifySlackRequest,
  verifySlackSignature,
  type SlackDeliverOptions,
} from "../../src/index.js";

// The tests drive credentials through process.env; the channel takes them as env.
const env = () => ({
  SLACK_BOT_TOKEN: process.env.SLACK_BOT_TOKEN,
  SLACK_SIGNING_SECRET: process.env.SLACK_SIGNING_SECRET,
});
const parseInbound = (params: { raw: unknown }) => parseSlackUpdate(params.raw);
const verifyInbound = (params: { headers: Record<string, string>; body: string }) => ({
  ok: verifySlackRequest(params.headers, params.body, env()),
});
const deliver = (params: Record<string, unknown>, options?: SlackDeliverOptions) =>
  deliverSlack({ ...(params as never), env: env() }, options);

const profile: CapabilityProfile = SLACK_PROFILE;

describe("projectSlack", () => {
  it("renders an approval ask as Approve/Reject buttons carrying the decisionRef", () => {
    const { blocks } = projectSlack(
      [{ kind: "ask", ask: "approval", prompt: "Send the report?", decisionRef: "ar-1" }],
      profile,
    );
    const actions = blocks.find((b) => b.type === "actions") as {
      elements: Array<{ action_id: string; value: string }>;
    };
    expect(actions.elements.map((e) => [e.action_id, e.value])).toEqual([
      ["approve", "ar-1"],
      ["reject", "ar-1"],
    ]);
  });

  it("degrades a chart to a web-dashboard note (charts: false)", () => {
    const { blocks } = projectSlack(
      [
        {
          kind: "inform",
          title: "Revenue",
          body: "Up 4%.",
          series: [1, 2, 3],
        } as never,
      ],
      profile,
    );
    expect(JSON.stringify(blocks)).toContain("web dashboard");
  });
});

describe("parseInbound", () => {
  it("normalizes a block_actions approve tap with sender + recipient", () => {
    const raw = {
      type: "block_actions",
      team: { id: "T1" },
      user: { id: "U7", name: "ada" },
      container: { channel_id: "C9" },
      actions: [{ action_id: "approve", value: "ar-42" }],
    };
    const result = parseInbound({ raw, headers: {}, body: "" } as never);
    expect(result.intents).toEqual([
      { kind: "decision", decisionRef: "ar-42", choice: "approve" },
    ]);
    expect(result.sender).toEqual({ id: "U7", displayName: "ada", workspaceId: "T1" });
    expect(result.recipient).toEqual({ kind: "slack", channel: "C9" });
  });

  it("normalizes a DM (channel_type=im) and skips bot echoes", () => {
    const human = parseInbound({
      raw: {
        type: "event_callback",
        team_id: "T1",
        event: { type: "message", channel_type: "im", text: "hi neko", user: "U7", channel: "D9" },
      },
      headers: {},
      body: "",
    } as never);
    expect(human.intents).toEqual([{ kind: "utterance", text: "hi neko", threadRef: undefined }]);
    expect(human.sender).toEqual({ id: "U7", workspaceId: "T1" });
    expect(human.recipient).toEqual({ kind: "slack", channel: "D9" });

    const bot = parseInbound({
      raw: {
        type: "event_callback",
        event: { type: "message", channel_type: "im", text: "echo", bot_id: "B1", channel: "D9" },
      },
      headers: {},
      body: "",
    } as never);
    expect(bot.intents).toEqual([]);
    expect(bot.sender).toBeUndefined();
  });

  it("ignores a channel message that does not @-mention the bot", () => {
    const out = parseInbound({
      raw: {
        type: "event_callback",
        team_id: "T1",
        event: {
          type: "message",
          channel_type: "channel",
          text: "watercooler chatter",
          user: "U7",
          channel: "C9",
        },
      },
      headers: {},
      body: "",
    } as never);
    expect(out.intents).toEqual([]);
  });

  it("continues a channel thread reply with a requireThread recipient for the worker gate", () => {
    const out = parseInbound({
      raw: {
        type: "event_callback",
        team_id: "T1",
        event: {
          type: "message",
          channel_type: "channel",
          text: "and last quarter?",
          user: "U7",
          channel: "C9",
          thread_ts: "171.5",
        },
      },
      headers: {},
      body: "",
    } as never);
    expect(out.intents).toEqual([
      { kind: "utterance", text: "and last quarter?", threadRef: "171.5" },
    ]);
    expect(out.recipient).toEqual({
      kind: "slack",
      channel: "C9",
      thread_ts: "171.5",
      requireThread: true,
    });
  });

  it("normalizes an @-mention into a threaded utterance", () => {
    const out = parseInbound({
      raw: {
        type: "event_callback",
        team_id: "T1",
        event: {
          type: "app_mention",
          text: "<@U0BOT> what's revenue?",
          user: "U7",
          channel: "C9",
          ts: "171.5",
        },
      },
      headers: {},
      body: "",
    } as never);
    expect(out.intents).toEqual([
      { kind: "utterance", text: "what's revenue?", threadRef: "171.5" },
    ]);
    expect(out.recipient).toEqual({ kind: "slack", channel: "C9", thread_ts: "171.5" });
  });

  it("maps a slash command to an invoke with an ephemeral recipient", () => {
    const out = parseInbound({
      raw: {
        command: "/openneko",
        text: "rules-list customer policies",
        channel_id: "C9",
        user_id: "U7",
        user_name: "ada",
        team_id: "T1",
      },
      headers: {},
      body: "",
    } as never);
    expect(out.intents).toEqual([
      { kind: "invoke", command: "rules-list", args: { text: "customer policies" } },
    ]);
    expect(out.recipient).toEqual({ kind: "slack", channel: "C9", user: "U7", ephemeral: true });
    expect(out.sender).toEqual({ id: "U7", displayName: "ada", workspaceId: "T1" });
  });

  it("turns a choice button tap into the option label as a threaded reply", () => {
    const out = parseInbound({
      raw: {
        type: "block_actions",
        team: { id: "T1" },
        user: { id: "U7", name: "ada" },
        container: { channel_id: "C9", message_ts: "171.2" },
        message: { ts: "171.2" },
        actions: [
          {
            action_id: "select:opt-1",
            value: "ar-9:opt-1",
            text: { type: "plain_text", text: "Option B" },
          },
        ],
      },
      headers: {},
      body: "",
    } as never);
    expect(out.intents).toEqual([{ kind: "utterance", text: "Option B", threadRef: "171.2" }]);
    expect(out.recipient).toEqual({ kind: "slack", channel: "C9", thread_ts: "171.2" });
  });
});

describe("verifySlackSignature", () => {
  const secret = "8f742231b10e8888abcd99yyyzzz85a5";
  const sign = (ts: string, body: string) =>
    `v0=${createHmac("sha256", secret).update(`v0:${ts}:${body}`).digest("hex")}`;

  it("accepts a fresh, correctly signed request", () => {
    const now = 1_700_000_000;
    const body = '{"type":"event_callback"}';
    const headers = {
      "X-Slack-Request-Timestamp": String(now),
      "X-Slack-Signature": sign(String(now), body),
    };
    expect(verifySlackSignature(secret, headers, body, now)).toBe(true);
  });

  it("rejects a bad signature, a stale timestamp, and missing headers", () => {
    const now = 1_700_000_000;
    const body = "{}";
    expect(
      verifySlackSignature(
        secret,
        { "X-Slack-Request-Timestamp": String(now), "X-Slack-Signature": "v0=dead" },
        body,
        now,
      ),
    ).toBe(false);
    const stale = String(now - 600);
    expect(
      verifySlackSignature(
        secret,
        { "X-Slack-Request-Timestamp": stale, "X-Slack-Signature": sign(stale, body) },
        body,
        now,
      ),
    ).toBe(false);
    expect(verifySlackSignature(secret, {}, body, now)).toBe(false);
  });

  it("verifyInbound fails closed when no signing secret is configured", () => {
    delete process.env.SLACK_SIGNING_SECRET;
    expect(verifyInbound({ headers: {}, body: "{}" } as never)).toEqual({ ok: false });
  });
});

describe("deliver", () => {
  it("posts projected blocks via chat.postMessage", async () => {
    process.env.SLACK_BOT_TOKEN = "xoxb-test";
    const calls: Array<{ method: string; body: Record<string, unknown> }> = [];
    const result = await deliver(
      {
        recipient: { channel: "C9" },
        events: [{ kind: "converse", text: "All done." }],
        profile,
      } as never,
      {
        createClient: () => ({
          postJson: async (method, body) => {
            calls.push({ method, body });
            return { ok: true, ts: "171.001" } as never;
          },
        }),
      },
    );
    delete process.env.SLACK_BOT_TOKEN;
    expect(result).toEqual({ delivered: true, ref: "171.001" });
    expect(calls[0].method).toBe("chat.postMessage");
    expect(calls[0].body.channel).toBe("C9");
    expect(Array.isArray(calls[0].body.blocks)).toBe(true);
  });

  it("threads the reply when recipient.thread_ts is set", async () => {
    process.env.SLACK_BOT_TOKEN = "xoxb-test";
    const calls: Array<{ method: string; body: Record<string, unknown> }> = [];
    await deliver(
      {
        recipient: { channel: "C9", thread_ts: "171.2" },
        events: [{ kind: "converse", text: "in thread" }],
        profile,
      } as never,
      {
        createClient: () => ({
          postJson: async (method, body) => {
            calls.push({ method, body });
            return { ok: true, ts: "171.9" } as never;
          },
        }),
      },
    );
    delete process.env.SLACK_BOT_TOKEN;
    expect(calls[0].method).toBe("chat.postMessage");
    expect(calls[0].body.thread_ts).toBe("171.2");
  });

  it("posts ephemerally for a slash-command recipient (user + ephemeral)", async () => {
    process.env.SLACK_BOT_TOKEN = "xoxb-test";
    const calls: Array<{ method: string; body: Record<string, unknown> }> = [];
    await deliver(
      {
        recipient: { channel: "C9", user: "U7", ephemeral: true },
        events: [{ kind: "converse", text: "only you see this" }],
        profile,
      } as never,
      {
        createClient: () => ({
          postJson: async (method, body) => {
            calls.push({ method, body });
            return { ok: true } as never;
          },
        }),
      },
    );
    delete process.env.SLACK_BOT_TOKEN;
    expect(calls[0].method).toBe("chat.postEphemeral");
    expect(calls[0].body.user).toBe("U7");
  });

  it("dry-runs without a token", async () => {
    delete process.env.SLACK_BOT_TOKEN;
    const result = await deliver(
      {
        recipient: { channel: "C9" },
        events: [{ kind: "converse", text: "hello" }],
        profile,
      } as never,
    );
    expect(result.delivered).toBe(false);
    expect(result.ref).toMatch(/^dry-run:/);
  });
});
