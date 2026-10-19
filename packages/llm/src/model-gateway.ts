// Model calls outside agent turns go through `openneko serve`, so serve is
// the one process that reaches model providers. Protocol:
// apps/ax-harness/serve/gateway.go.

import { createConnection } from "node:net";
import { createInterface } from "node:readline";

type GatewayFrame = {
  type?: string;
  status?: number;
  headers?: Record<string, string>;
  data?: string;
  code?: number;
  error?: string;
};

/** The fetch model calls use: through serve when it runs, else direct. */
export function modelFetch(): typeof fetch {
  const socket = process.env.OPENNEKO_SERVE_SOCKET;
  return socket ? gatewayFetch(socket) : fetch;
}

/** Ax options that send its model calls through serve when it runs. */
export function modelFetchOptions(): { fetch?: typeof fetch } {
  return process.env.OPENNEKO_SERVE_SOCKET ? { fetch: modelFetch() } : {};
}

/** A fetch whose requests serve makes. */
export function gatewayFetch(socket: string): typeof fetch {
  return async (input, init) => {
    const request = new Request(input, init);
    const headers: Record<string, string> = {};
    request.headers.forEach((value, name) => (headers[name] = value));
    const body = request.body ? await request.text() : "";
    const signal = init?.signal ?? request.signal;
    signal?.throwIfAborted();

    const conn = createConnection(socket);
    const lines = createInterface({ input: conn })[Symbol.asyncIterator]();
    const hangUp = () => conn.destroy();
    signal?.addEventListener("abort", hangUp, { once: true });
    const done = () => {
      signal?.removeEventListener("abort", hangUp);
      conn.destroy();
    };
    const next = async (): Promise<GatewayFrame> => {
      for (;;) {
        const line = await lines.next().catch((error: Error) => {
          throw signal?.aborted ? signal.reason : new Error(`openneko serve unavailable: ${error.message}`);
        });
        if (line.done) throw signal?.aborted ? signal.reason : new Error("openneko serve hung up during a model call");
        if (line.value.trim()) return JSON.parse(line.value) as GatewayFrame;
      }
    };

    conn.write(`${JSON.stringify({ type: "fetch", request: { url: request.url, method: request.method, headers, body } })}\n`);
    let head: GatewayFrame;
    try {
      head = await next();
    } catch (error) {
      done();
      throw error;
    }
    if (head.type !== "response") {
      done();
      throw new TypeError(head.error || "model call failed");
    }

    const stream = new ReadableStream<Uint8Array>({
      async pull(controller) {
        try {
          const frame = await next();
          if (frame.type === "chunk" && frame.data) controller.enqueue(Buffer.from(frame.data, "base64"));
          else if (frame.type === "exit") {
            done();
            if (frame.code) controller.error(new TypeError(frame.error || "model response interrupted"));
            else controller.close();
          }
        } catch (error) {
          done();
          controller.error(error);
        }
      },
      cancel: done,
    });
    const status = head.status ?? 502;
    // A null-body status cannot carry a stream.
    const nullBody = status === 204 || status === 205 || status === 304;
    if (nullBody) void stream.cancel();
    return new Response(nullBody ? null : stream, { status, headers: head.headers });
  };
}
