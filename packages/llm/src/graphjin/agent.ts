import type {
  AgentActionEvent,
  AgentRefusal,
  AgentRequest,
  AgentResponse,
  AgentStatus,
  AgentTurn,
} from "@neko/graphjin-contract";
import { packUserConnectionHeaders } from "./pack-user-connections";
import { graphjinTraceHeaders } from "./trace-headers";
/** Steps per GraphJin agent call: the cap OpenNeko writes and the default it asks for. */
export const GRAPHJIN_AGENT_MAX_STEPS = 12;

// The agent wire types come from GraphJin's Go types; see
// @neko/graphjin-contract.
export type GraphjinAgentTurn = AgentTurn;
export type GraphjinAgentRequest = AgentRequest;
export type GraphjinAgentRefusal = AgentRefusal;
export type GraphjinAgentResponse = AgentResponse;
export type GraphjinAgentStatus = AgentStatus;
export type GraphjinAgentActionEvent = AgentActionEvent;

/** Resolve GraphQL, MCP, or server-root URLs to the matching agent route. */
export function graphjinAgentEndpoint(baseUrl: string, status = false): string {
  const url = new URL(baseUrl);
  const suffix = status ? "agent/status" : "agent";
  const apiMarker = "/api/v1/";
  const markerIndex = url.pathname.indexOf(apiMarker);
  if (markerIndex >= 0) {
    const prefix = url.pathname.slice(0, markerIndex + apiMarker.length);
    const remainder = url.pathname.slice(markerIndex + apiMarker.length);
    const namespace = remainder.match(/^ns\/[^/]+\//)?.[0] ?? "";
    url.pathname = `${prefix}${namespace}${suffix}`;
  } else {
    url.pathname = `${url.pathname.replace(/\/$/, "")}/api/v1/${suffix}`;
  }
  url.search = "";
  url.hash = "";
  return url.toString();
}

async function readAgentResponse<T>(res: Response, operation: string): Promise<T> {
  const text = await res.text();
  let parsed: unknown;
  try {
    parsed = text ? JSON.parse(text) : null;
  } catch {
    parsed = null;
  }
  if (!res.ok) {
    const detail =
      parsed && typeof parsed === "object"
        ? JSON.stringify(parsed).slice(0, 1_000)
        : text.slice(0, 1_000);
    throw new Error(`GraphJin ${operation} failed: ${res.status} ${detail}`.trim());
  }
  if (!parsed || typeof parsed !== "object") {
    throw new Error(`GraphJin ${operation} returned an invalid JSON response`);
  }
  return parsed as T;
}

/**
 * Ask GraphJin's agent. With onAction, GraphJin streams the run over
 * Server-Sent Events and onAction receives each step as it happens.
 */
export async function askGraphjinAgent(input: {
  baseUrl: string;
  token: string;
  request: GraphjinAgentRequest;
  signal?: AbortSignal;
  onAction?: (event: GraphjinAgentActionEvent) => void;
}): Promise<GraphjinAgentResponse> {
  const res = await fetch(graphjinAgentEndpoint(input.baseUrl), {
    method: "POST",
    headers: await packUserConnectionHeaders(input.baseUrl, {
      ...graphjinTraceHeaders(),
      accept: input.onAction ? "text/event-stream" : "application/json",
      "content-type": "application/json",
      authorization: `Bearer ${input.token}`,
    }),
    redirect: "error",
    cache: "no-store",
    body: JSON.stringify(input.request),
    signal: input.signal,
  });
  if (input.onAction && res.ok && res.headers.get("content-type")?.startsWith("text/event-stream")) {
    return readAgentEvents(res, input.onAction);
  }
  return readAgentResponse<GraphjinAgentResponse>(res, "agent request");
}

/** Read GraphJin's agent stream: `action` events, then one `result`. */
async function readAgentEvents(
  res: Response,
  onAction: (event: GraphjinAgentActionEvent) => void,
): Promise<GraphjinAgentResponse> {
  if (!res.body) throw new Error("GraphJin agent stream has no body");
  const decoder = new TextDecoder();
  let buffered = "";
  let result: GraphjinAgentResponse | undefined;
  const dispatch = (block: string) => {
    let event = "message";
    const data: string[] = [];
    for (const line of block.split("\n")) {
      if (line.startsWith("event:")) event = line.slice(6).trim();
      else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
    }
    if (data.length === 0) return;
    let payload: unknown;
    try {
      payload = JSON.parse(data.join("\n"));
    } catch {
      return;
    }
    if (event === "action") {
      try {
        onAction(payload as GraphjinAgentActionEvent);
      } catch {
        // Progress is best-effort; the answer still arrives.
      }
    } else if (event === "result" && payload && typeof payload === "object") {
      result = payload as GraphjinAgentResponse;
    }
  };
  for await (const chunk of res.body as unknown as AsyncIterable<Uint8Array>) {
    buffered += decoder.decode(chunk, { stream: true }).replace(/\r\n/g, "\n");
    let end = buffered.indexOf("\n\n");
    while (end >= 0) {
      dispatch(buffered.slice(0, end));
      buffered = buffered.slice(end + 2);
      end = buffered.indexOf("\n\n");
    }
  }
  if (buffered.trim()) dispatch(buffered);
  if (!result) throw new Error("GraphJin agent stream ended without a result");
  return result;
}

export async function getGraphjinAgentStatus(input: {
  baseUrl: string;
  token: string;
  signal?: AbortSignal;
}): Promise<GraphjinAgentStatus> {
  const res = await fetch(graphjinAgentEndpoint(input.baseUrl, true), {
    headers: {
      accept: "application/json",
      authorization: `Bearer ${input.token}`,
    },
    signal: input.signal,
  });
  return readAgentResponse<GraphjinAgentStatus>(res, "agent status request");
}
