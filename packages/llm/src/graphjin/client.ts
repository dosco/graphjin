import { packUserConnectionHeaders, type ConnectionBinding } from "./pack-user-connections";
import { graphjinTraceHeaders } from "./trace-headers";
import { randomUUID } from "node:crypto";
import { Agent } from "undici";

export type GraphjinQueryOptions = {
  connectionBindings?: ConnectionBinding[];
  /** Trusted configuration-only requests must never attach personal tokens. */
  configurationOnly?: boolean;
  /** Full GraphJin GraphQL endpoint URL, e.g. `http://host:8080/api/v1/graphql`. */
  baseUrl: string;
  query: string;
  variables?: Record<string, unknown>;
  operationName?: string;
  /** Sent as `X-Role` header for GraphJin role-based access. */
  role?: string;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  /**
   * A gj_config preview or apply can rebuild GraphJin's runtime, which
   * rediscovers every source. On a large schema that outlasts fetch's
   * 5-minute wait for response headers.
   */
  longRunning?: boolean;
};

export type GraphjinQueryResult<T = unknown> = {
  data: T | null;
  errors?: GraphjinQueryError[];
};

/** A GraphQL error. extensions.code holds a GraphJin error code when it has one. */
export type GraphjinQueryError = {
  message: string;
  path?: (string | number)[];
  extensions?: Record<string, unknown>;
};

const longRunningTimeoutMs = 45 * 60_000;
let longRunningAgent: Agent | undefined;

function longRunningDispatcher(): Agent {
  longRunningAgent ??= new Agent({ headersTimeout: longRunningTimeoutMs, bodyTimeout: longRunningTimeoutMs });
  return longRunningAgent;
}

export async function graphjinQuery<T = unknown>(
  opts: GraphjinQueryOptions,
): Promise<GraphjinQueryResult<T>> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...graphjinTraceHeaders(),
    ...(opts.role ? { "X-Role": opts.role } : {}),
    ...(opts.headers ?? {}),
  };
  const res = await fetch(opts.baseUrl, {
    method: "POST",
    headers: opts.configurationOnly ? headers : await packUserConnectionHeaders(opts.baseUrl, headers, opts.connectionBindings),
    redirect: "error",
    cache: "no-store",
    body: JSON.stringify({
      query: opts.query,
      variables: opts.variables ?? {},
      operationName: opts.operationName,
    }),
    signal: opts.signal,
    ...(opts.longRunning ? { dispatcher: longRunningDispatcher() } : {}),
  } as RequestInit);
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(`graphjin query failed: ${res.status} ${text.slice(0, 500)}`);
  }
  const body = (await res.json()) as {
    data?: T | null;
    errors?: unknown[];
  };
  const errors = Array.isArray(body.errors)
    ? body.errors.map((item) => {
        if (typeof item === "string") return { message: item };
        if (item && typeof item === "object") {
          const error = item as {
            message?: unknown;
            path?: unknown;
            extensions?: unknown;
          };
          return {
            message:
              typeof error.message === "string"
                ? error.message
                : JSON.stringify(item),
            ...(Array.isArray(error.path)
              ? { path: error.path as (string | number)[] }
              : {}),
            ...(error.extensions && typeof error.extensions === "object" && !Array.isArray(error.extensions)
              ? { extensions: error.extensions as Record<string, unknown> }
              : {}),
          };
        }
        return { message: String(item) };
      })
    : undefined;
  return {
    data: body.data ?? null,
    ...(errors?.length ? { errors } : {}),
  };
}

export type GraphjinSubscriptionMessage<T = unknown> = {
  data: T | null;
  errors?: Array<{ message: string; path?: (string | number)[] }>;
};

export type GraphjinSubscribeOptions<T = unknown> = {
  /** Full GraphJin GraphQL endpoint URL, e.g. `http://host:8080/api/v1/graphql`. */
  baseUrl: string;
  query: string;
  variables?: Record<string, unknown>;
  operationName?: string;
  /** Sent inside the connection_init payload as `role`. */
  role?: string;
  onNext: (msg: GraphjinSubscriptionMessage<T>) => void | Promise<void>;
  onError?: (err: Error) => void;
  onComplete?: () => void;
  signal?: AbortSignal;
};

export type GraphjinSubscriptionHandle = {
  /** Resolves when the server acknowledges the connection_init. */
  ready: Promise<void>;
  /** Send `complete` and close the socket. */
  stop: () => void;
};

type WsMessage =
  | { type: "connection_init"; payload?: Record<string, unknown> }
  | { type: "connection_ack" }
  | { type: "ping" }
  | { type: "pong" }
  | {
      type: "subscribe";
      id: string;
      payload: { query: string; variables?: unknown; operationName?: string };
    }
  | { type: "next"; id: string; payload: GraphjinSubscriptionMessage }
  | { type: "error"; id: string; payload?: unknown }
  | { type: "complete"; id: string };

function errorMessageFromPayload(payload: unknown): string {
  const list = Array.isArray(payload)
    ? payload
    : Array.isArray((payload as { errors?: unknown } | null)?.errors)
      ? (payload as { errors: unknown[] }).errors
      : null;
  const joined = list
    ?.map((e) =>
      e && typeof e === "object" && "message" in e
        ? String((e as { message: unknown }).message)
        : String(e),
    )
    .filter(Boolean)
    .join("; ");
  if (joined) return joined;
  if (typeof payload === "string" && payload) return payload;
  return "subscription error";
}

/**
 * Subscribe to a GraphJin query via WebSocket using the
 * `graphql-transport-ws` protocol. Maintains a single connection per
 * call; the worker can fan out N subscriptions across N sockets, or
 * upgrade to a shared multiplexer later.
 *
 * GraphJin upgrades to WebSocket on its GraphQL endpoint, so baseUrl is
 * the full `/api/v1/graphql` URL — we only swap the http(s) scheme for
 * ws(s); we do NOT append a path (doing so 404s the upgrade).
 */
export function graphjinSubscribe<T = unknown>(
  opts: GraphjinSubscribeOptions<T>,
): GraphjinSubscriptionHandle {
  const wsUrl = opts.baseUrl.replace(/^http/, "ws");
  const subId = randomUUID();
  const ws = new WebSocket(wsUrl, "graphql-transport-ws");
  let stopped = false;
  let resolveReady: () => void;
  let rejectReady: (e: Error) => void;
  const ready = new Promise<void>((res, rej) => {
    resolveReady = res;
    rejectReady = rej;
  });

  const send = (msg: WsMessage) => {
    if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg));
  };

  ws.addEventListener("open", () => {
    send({
      type: "connection_init",
      payload: opts.role ? { role: opts.role } : {},
    });
  });

  ws.addEventListener("message", (event) => {
    let parsed: WsMessage;
    try {
      parsed = JSON.parse(typeof event.data === "string" ? event.data : "");
    } catch {
      return;
    }
    switch (parsed.type) {
      case "connection_ack":
        send({
          type: "subscribe",
          id: subId,
          payload: {
            query: opts.query,
            variables: opts.variables ?? {},
            operationName: opts.operationName,
          },
        });
        resolveReady();
        return;
      case "ping":
        send({ type: "pong" });
        return;
      case "next": {
        if (parsed.id !== subId) return;
        // One bad handler must not take down the process (the worker
        // crash-looped on exactly this in 2026-06); route to onError.
        const fail = (err: unknown) =>
          opts.onError?.(err instanceof Error ? err : new Error(String(err)));
        try {
          void Promise.resolve(
            opts.onNext(parsed.payload as GraphjinSubscriptionMessage<T>),
          ).catch(fail);
        } catch (err) {
          fail(err);
        }
        return;
      }
      case "error":
        if (parsed.id !== subId) return;
        opts.onError?.(new Error(errorMessageFromPayload(parsed.payload)));
        return;
      case "complete":
        if (parsed.id !== subId) return;
        opts.onComplete?.();
        return;
    }
  });

  ws.addEventListener("error", () => {
    if (!stopped) {
      const err = new Error("graphjin subscription socket error");
      rejectReady(err);
      opts.onError?.(err);
    }
  });

  ws.addEventListener("close", () => {
    if (!stopped) opts.onComplete?.();
  });

  opts.signal?.addEventListener("abort", () => stop());

  function stop() {
    if (stopped) return;
    stopped = true;
    try {
      send({ type: "complete", id: subId });
    } catch {
      // best-effort
    }
    try {
      ws.close();
    } catch {
      // already closed
    }
  }

  return { ready, stop };
}
