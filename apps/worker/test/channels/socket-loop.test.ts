import { beforeEach, describe, expect, it, vi } from "vitest";

// handleFrame is the pure-ish core of the Slack Socket Mode loop. Mock the
// downstream dispatch + channel settings so we test framing/ack/routing in isolation.
vi.mock("../../src/channels/delivery.js", () => ({
  processInboundUpdate: vi.fn(async () => true),
}));
vi.mock("../../src/channels/runtime.js", () => ({
  channelEnv: vi.fn(async () => ({})),
}));

import { handleFrame } from "../../src/channels/socket-loop";
import { processInboundUpdate } from "../../src/channels/delivery.js";

const makeWs = () => ({ send: vi.fn(), close: vi.fn() });
const frame = (o: Record<string, unknown>) => JSON.stringify(o);

beforeEach(() => vi.clearAllMocks());

describe("socket-loop handleFrame", () => {
  it("ignores a hello frame (no ack, no close, no dispatch)", async () => {
    const ws = makeWs();
    await handleFrame(ws as never, frame({ type: "hello" }), "org-1", "p");
    expect(ws.send).not.toHaveBeenCalled();
    expect(ws.close).not.toHaveBeenCalled();
    expect(processInboundUpdate).not.toHaveBeenCalled();
  });

  it("closes on a disconnect frame so the outer loop reconnects", async () => {
    const ws = makeWs();
    await handleFrame(ws as never, frame({ type: "disconnect", reason: "refresh" }), "org-1", "p");
    expect(ws.close).toHaveBeenCalled();
    expect(processInboundUpdate).not.toHaveBeenCalled();
  });

  it("acks an events_api envelope BEFORE dispatching its payload", async () => {
    const ws = makeWs();
    const payload = { type: "event_callback", event: { type: "message" } };
    await handleFrame(
      ws as never,
      frame({ type: "events_api", envelope_id: "e1", payload }),
      "org-1",
      "p",
    );
    expect(ws.send).toHaveBeenCalledWith(JSON.stringify({ envelope_id: "e1" }));
    expect(processInboundUpdate).toHaveBeenCalledWith("org-1", "p", payload);
    // Slack requires the ack within 3s; parsing spins up the VM, so ack first.
    expect(ws.send.mock.invocationCallOrder[0]).toBeLessThan(
      vi.mocked(processInboundUpdate).mock.invocationCallOrder[0],
    );
  });

  it("dispatches slash_commands and interactive payloads too", async () => {
    const ws = makeWs();
    await handleFrame(
      ws as never,
      frame({ type: "slash_commands", envelope_id: "e2", payload: { command: "/openneko" } }),
      "o",
      "p",
    );
    await handleFrame(
      ws as never,
      frame({ type: "interactive", envelope_id: "e3", payload: { type: "block_actions" } }),
      "o",
      "p",
    );
    expect(processInboundUpdate).toHaveBeenCalledTimes(2);
  });

  it("acks but does not dispatch a frame with no payload", async () => {
    const ws = makeWs();
    await handleFrame(ws as never, frame({ type: "events_api", envelope_id: "e4" }), "o", "p");
    expect(ws.send).toHaveBeenCalledWith(JSON.stringify({ envelope_id: "e4" }));
    expect(processInboundUpdate).not.toHaveBeenCalled();
  });

  it("ignores malformed JSON without throwing", async () => {
    const ws = makeWs();
    await expect(handleFrame(ws as never, "not json{", "o", "p")).resolves.toBeUndefined();
    expect(ws.send).not.toHaveBeenCalled();
    expect(processInboundUpdate).not.toHaveBeenCalled();
  });
});
