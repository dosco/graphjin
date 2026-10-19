import type {
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

export async function askGraphjinAgent(input: {
  baseUrl: string;
  token: string;
  request: GraphjinAgentRequest;
  signal?: AbortSignal;
}): Promise<GraphjinAgentResponse> {
  const res = await fetch(graphjinAgentEndpoint(input.baseUrl), {
    method: "POST",
    headers: await packUserConnectionHeaders(input.baseUrl, {
      ...graphjinTraceHeaders(),
      accept: "application/json",
      "content-type": "application/json",
      authorization: `Bearer ${input.token}`,
    }),
    redirect: "error",
    cache: "no-store",
    body: JSON.stringify(input.request),
    signal: input.signal,
  });
  return readAgentResponse<GraphjinAgentResponse>(res, "agent request");
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
