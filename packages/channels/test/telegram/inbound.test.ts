import { describe, it, expect } from "vitest";
import { parseTelegramInbound, recipientFromTelegramUpdate, senderFromTelegramUpdate } from "../../src/telegram/inbound";

describe("parseTelegramInbound", () => {
  it("maps a callback_query button tap to an approve decision", () => {
    const update = {
      update_id: 1,
      callback_query: { id: "cq1", data: "approve:dr-123", message: { chat: { id: 42 } } },
    };
    expect(parseTelegramInbound(update)).toEqual([
      { kind: "decision", decisionRef: "dr-123", choice: "approve" },
    ]);
  });

  it("maps a reject tap", () => {
    expect(parseTelegramInbound({ callback_query: { data: "reject:dr-9" } })).toEqual([
      { kind: "decision", decisionRef: "dr-9", choice: "reject" },
    ]);
  });

  it("maps a select tap", () => {
    expect(parseTelegramInbound({ callback_query: { data: "select:dr-9:gold" } })).toEqual([
      { kind: "select", ref: "dr-9", optionId: "gold" },
    ]);
  });

  it("maps a text message to an utterance carrying the chat id as threadRef", () => {
    expect(parseTelegramInbound({ message: { text: "how are sales?", chat: { id: 4242 } } })).toEqual([
      { kind: "utterance", text: "how are sales?", threadRef: "4242" },
    ]);
  });

  it("maps a slash command (with args) to an invoke", () => {
    expect(parseTelegramInbound({ message: { text: "/brief today please", chat: { id: 1 } } })).toEqual([
      { kind: "invoke", command: "brief", args: { text: "today please" } },
    ]);
  });

  it("maps a bare slash command to invoke without args", () => {
    expect(parseTelegramInbound({ message: { text: "/brief", chat: { id: 1 } } })).toEqual([
      { kind: "invoke", command: "brief" },
    ]);
  });

  it("handles a getUpdates envelope ({ result: Update[] })", () => {
    const env = {
      ok: true,
      result: [
        { callback_query: { data: "approve:x" } },
        { message: { text: "hi", chat: { id: 7 } } },
      ],
    };
    expect(parseTelegramInbound(env)).toEqual([
      { kind: "decision", decisionRef: "x", choice: "approve" },
      { kind: "utterance", text: "hi", threadRef: "7" },
    ]);
  });

  it("ignores updates it can't map", () => {
    expect(parseTelegramInbound({ update_id: 5 })).toEqual([]);
    expect(parseTelegramInbound(null)).toEqual([]);
    expect(parseTelegramInbound({ callback_query: { data: "weird-no-colon" } })).toEqual([]);
  });
});

describe("recipientFromTelegramUpdate", () => {
  it("extracts the sender chat from a message", () => {
    expect(
      recipientFromTelegramUpdate({ message: { text: "hi", chat: { id: 8102762294 } } }),
    ).toEqual({ kind: "telegram", chatId: 8102762294 });
  });

  it("extracts the chat from a callback_query's message", () => {
    expect(
      recipientFromTelegramUpdate({
        callback_query: { data: "approve:x", message: { chat: { id: 42 } } },
      }),
    ).toEqual({ kind: "telegram", chatId: 42 });
  });

  it("returns undefined when there's no chat to bind to", () => {
    expect(recipientFromTelegramUpdate({ update_id: 5 })).toBeUndefined();
    expect(recipientFromTelegramUpdate(null)).toBeUndefined();
  });
});

describe("senderFromTelegramUpdate (CH1)", () => {
  it("captures from.id + name on a message", () => {
    expect(
      senderFromTelegramUpdate({
        message: {
          text: "hi",
          chat: { id: 42 },
          from: { id: 777, first_name: "Ada", last_name: "L" },
        },
      }),
    ).toEqual({ id: "777", displayName: "Ada L" });
  });

  it("captures the callback_query sender", () => {
    expect(
      senderFromTelegramUpdate({
        callback_query: { data: "approve:x", from: { id: 9, username: "ada" } },
      }),
    ).toEqual({ id: "9", displayName: "ada" });
  });

  it("returns undefined for channel posts (no from)", () => {
    expect(
      senderFromTelegramUpdate({ channel_post: { text: "x", chat: { id: 1 } } }),
    ).toBeUndefined();
  });
});
