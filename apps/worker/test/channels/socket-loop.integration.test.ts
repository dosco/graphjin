import { createHash } from "node:crypto";
import { createServer, type IncomingMessage } from "node:http";
import type { Duplex } from "node:stream";
import { afterEach, describe, expect, it, vi } from "vitest";

// Integration test: drive runSlackSocketLoop against a REAL local WebSocket
// server (real global WebSocket client, real apps.connections.open via a stubbed
// fetch returning the local ws URL). Only processInboundUpdate — the seam into
// the rest of the worker — is mocked. Exercises connect → ack → dispatch →
// disconnect → backoff → reconnect, which handleFrame's unit test can't.
vi.mock("../../src/channels/delivery.js", () => ({
  processInboundUpdate: vi.fn(async () => true),
}));
vi.mock("../../src/channels/runtime.js", () => ({
  channelEnv: vi.fn(async () => ({ SLACK_APP_TOKEN: "xapp-test" })),
}));

import { runSlackSocketLoop } from "../../src/channels/socket-loop";
import { processInboundUpdate } from "../../src/channels/delivery.js";
import { channelEnv } from "../../src/channels/runtime.js";

const WS_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";
const accept = (key: string) => createHash("sha1").update(key + WS_GUID).digest("base64");

function encodeText(data: string): Buffer {
  const body = Buffer.from(data, "utf8");
  const n = body.length;
  const head =
    n < 126 ? Buffer.from([0x81, n]) : Buffer.from([0x81, 126, (n >> 8) & 0xff, n & 0xff]);
  return Buffer.concat([head, body]);
}

// Decode a single masked client→server text frame (enough for the tiny acks).
function decodeText(buf: Buffer): string | null {
  if (buf.length < 2 || (buf[0] & 0x0f) !== 0x1) return null;
  const masked = (buf[1] & 0x80) !== 0;
  let len = buf[1] & 0x7f;
  let off = 2;
  if (len === 126) {
    len = buf.readUInt16BE(2);
    off = 4;
  }
  const mask = masked ? buf.subarray(off, (off += 4)) : null;
  const body = buf.subarray(off, off + len);
  const out = Buffer.alloc(len);
  for (let i = 0; i < len; i++) out[i] = body[i] ^ (mask ? mask[i % 4] : 0);
  return out.toString("utf8");
}

type WsConn = { received: string[]; send: (s: string) => void; close: () => void };

function startWsServer(onConn: (c: WsConn, index: number) => void) {
  const conns: WsConn[] = [];
  const sockets: Duplex[] = [];
  const server = createServer();
  server.on("upgrade", (req: IncomingMessage, socket: Duplex) => {
    sockets.push(socket);
    socket.write(
      "HTTP/1.1 101 Switching Protocols\r\n" +
        "Upgrade: websocket\r\n" +
        "Connection: Upgrade\r\n" +
        `Sec-WebSocket-Accept: ${accept((req.headers["sec-websocket-key"] as string) ?? "")}\r\n\r\n`,
    );
    const conn: WsConn = {
      received: [],
      send: (s) => socket.write(encodeText(s)),
      close: () => socket.destroy(),
    };
    const index = conns.push(conn) - 1;
    socket.on("data", (chunk: Buffer) => {
      const text = decodeText(chunk);
      if (text !== null) conn.received.push(text);
    });
    socket.on("error", () => {});
    onConn(conn, index);
  });
  return new Promise<{ port: number; conns: WsConn[]; close: () => Promise<void> }>((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      resolve({
        port: typeof addr === "object" && addr ? addr.port : 0,
        conns,
        close: () =>
          new Promise<void>((r) => {
            for (const s of sockets) s.destroy();
            server.close(() => r());
          }),
      });
    });
  });
}

const waitFor = async (pred: () => boolean, ms = 8000): Promise<void> => {
  const start = Date.now();
  while (!pred()) {
    if (Date.now() - start > ms) throw new Error("waitFor timed out");
    await new Promise((r) => setTimeout(r, 25));
  }
};

describe("runSlackSocketLoop (integration: real WebSocket transport)", () => {
  let stopped = true;
  let realFetch: typeof globalThis.fetch;
  let srv: Awaited<ReturnType<typeof startWsServer>> | undefined;
  let loop: Promise<void> | undefined;

  afterEach(async () => {
    stopped = true;
    if (loop) await loop.catch(() => {});
    loop = undefined;
    if (realFetch) globalThis.fetch = realFetch;
    if (srv) await srv.close();
    srv = undefined;
    vi.clearAllMocks();
  });

  it("connects, acks before dispatch, then reconnects after a disconnect", async () => {
    const EVENT = { type: "event_callback", event: { type: "message", text: "hi" } };
    srv = await startWsServer((conn, index) => {
      if (index === 0) {
        // Send after the handshake settles into its own segment — bundling
        // frames with the 101 response trips the client's parser.
        setTimeout(() => {
          conn.send(JSON.stringify({ type: "hello" }));
          conn.send(JSON.stringify({ type: "events_api", envelope_id: "e1", payload: EVENT }));
          // Slack sends a disconnect frame then drops the socket; mirror that so
          // the client's close fires and the loop backs off + reconnects.
          setTimeout(() => {
            conn.send(JSON.stringify({ type: "disconnect" }));
            conn.close();
          }, 100);
        }, 30);
      }
    });
    realFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      json: async () => ({ ok: true, url: `ws://127.0.0.1:${srv!.port}/link` }),
    })) as never;

    stopped = false;
    loop = runSlackSocketLoop({
      orgId: "org-1",
      pluginName: "slack",
      isStopped: () => stopped,
    });

    await waitFor(() => srv!.conns.length >= 1 && srv!.conns[0].received.length >= 1);
    expect(srv.conns[0].received).toContain(JSON.stringify({ envelope_id: "e1" }));
    expect(processInboundUpdate).toHaveBeenCalledWith("org-1", "slack", EVENT);

    // disconnect frame → close → 3s backoff → reconnect
    await waitFor(() => srv!.conns.length >= 2, 8000);
    expect(srv.conns.length).toBeGreaterThanOrEqual(2);
  }, 15000);

  it("bails without connecting when SLACK_APP_TOKEN is absent", async () => {
    vi.mocked(channelEnv).mockResolvedValueOnce({});
    srv = await startWsServer(() => {});
    realFetch = globalThis.fetch;
    const fetchSpy = vi.fn();
    globalThis.fetch = fetchSpy as never;

    stopped = false;
    await runSlackSocketLoop({
      orgId: "o",
      pluginName: "slack",
      isStopped: () => stopped,
    });

    expect(fetchSpy).not.toHaveBeenCalled();
    expect(srv.conns.length).toBe(0);
  });
});
