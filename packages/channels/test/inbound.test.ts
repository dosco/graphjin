import { describe, expect, it } from "vitest";
import { intentFromButtonId, parseSlackInbound } from "../src/index.js";

describe("parseSlackInbound", () => {
  it("maps an Approve tap to a decision", () => {
    const raw = { type: "block_actions", actions: [{ action_id: "approve", value: "ar-123" }] };
    expect(parseSlackInbound(raw)).toEqual([{ kind: "decision", decisionRef: "ar-123", choice: "approve" }]);
  });

  it("maps a Reject tap to a decision", () => {
    const raw = { type: "block_actions", actions: [{ action_id: "reject", value: "ar-9" }] };
    expect(parseSlackInbound(raw)).toEqual([{ kind: "decision", decisionRef: "ar-9", choice: "reject" }]);
  });

  it("maps an option button to the option label as a threaded reply", () => {
    const raw = {
      type: "block_actions",
      message: { ts: "171.2" },
      actions: [{ action_id: "select:opt-a", value: "ar-7:opt-a", text: { type: "plain_text", text: "Option A" } }],
    };
    expect(parseSlackInbound(raw)).toEqual([{ kind: "utterance", text: "Option A", threadRef: "171.2" }]);
  });

  it("maps a human message to an utterance carrying the thread", () => {
    const raw = { type: "event_callback", event: { type: "message", channel_type: "im", text: "what's our churn?", thread_ts: "171.99" } };
    expect(parseSlackInbound(raw)).toEqual([{ kind: "utterance", text: "what's our churn?", threadRef: "171.99" }]);
  });

  it("ignores the bot's own echoes", () => {
    const raw = { type: "event_callback", event: { type: "message", text: "hi", bot_id: "B1" } };
    expect(parseSlackInbound(raw)).toEqual([]);
  });

  it("returns nothing for unrelated payloads", () => {
    expect(parseSlackInbound({ type: "url_verification" })).toEqual([]);
    expect(parseSlackInbound(null)).toEqual([]);
  });
});

describe("intentFromButtonId", () => {
  it("decodes the select convention", () => {
    expect(intentFromButtonId("select:ar-1:opt-a")).toEqual({ kind: "select", ref: "ar-1", optionId: "opt-a" });
  });

  it("rejects ids without a verb", () => {
    expect(intentFromButtonId("garbage")).toBeNull();
  });
});
