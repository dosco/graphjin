import "server-only";

import { and, data_source, db, desc, eq } from "@neko/db";
import {
  GRAPHJIN_AGENT_MAX_STEPS,
  askGraphjinAgent,
  graphjinQuery,
  mintGraphjinToken,
  type GraphjinAgentResponse,
  type GraphjinAgentTurn,
} from "@neko/llm/graphjin";

/**
 * Server-side access to the org's GraphJin for the admin console under
 * /admin/graphjin. Every call mints a short-lived admin token for the signed-in
 * admin; the token never leaves the server.
 */

export type GraphjinAdminSource = { name: string; graphqlUrl: string };

export type GraphjinAdminResult<T> =
  | { ok: true; data: T; errors: string[] }
  | { ok: false; error: string; status?: number };

export type GraphjinAdminCaller = { orgId: string; userId: string | null };

const TOKEN_TTL_SECONDS = 120;
const REQUEST_TIMEOUT_MS = 30_000;
const AGENT_TIMEOUT_MS = 180_000;
export const GRAPHJIN_NO_SOURCE_ERROR = "No GraphJin data source is enabled. Add one under Settings → Data.";

/** The default enabled data source, the same one Ask and group grants use. */
export async function graphjinAdminSource(orgId: string): Promise<GraphjinAdminSource | null> {
  const [source] = await db()
    .select({ name: data_source.name, graphqlUrl: data_source.graphql_url })
    .from(data_source)
    .where(and(eq(data_source.org_id, orgId), eq(data_source.enabled, true)))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);
  return source?.graphqlUrl ? { name: source.name, graphqlUrl: source.graphqlUrl } : null;
}

/** Resolve a GraphQL or server-root URL to another /api/v1 route, keeping any namespace. */
export function graphjinApiUrl(graphqlUrl: string, path: string): string {
  const url = new URL(graphqlUrl);
  const marker = "/api/v1/";
  const index = url.pathname.indexOf(marker);
  if (index >= 0) {
    const remainder = url.pathname.slice(index + marker.length);
    const namespace = remainder.match(/^ns\/[^/]+\//)?.[0] ?? "";
    url.pathname = `${url.pathname.slice(0, index + marker.length)}${namespace}${path}`;
  } else {
    url.pathname = `${url.pathname.replace(/\/+$/, "")}${marker}${path}`;
  }
  url.search = "";
  url.hash = "";
  return url.toString();
}

function adminToken(caller: GraphjinAdminCaller): string {
  return mintGraphjinToken({
    orgId: caller.orgId,
    userId: caller.userId,
    role: "admin",
    ttlSeconds: TOKEN_TTL_SECONDS,
  });
}

function errorMessage(error: unknown): string {
  return (error instanceof Error ? error.message : String(error)).slice(0, 1_000);
}

export async function graphjinAdminQuery<T>(
  caller: GraphjinAdminCaller,
  query: string,
  variables?: Record<string, unknown>,
): Promise<GraphjinAdminResult<T>> {
  const source = await graphjinAdminSource(caller.orgId);
  if (!source) return { ok: false, error: GRAPHJIN_NO_SOURCE_ERROR };
  try {
    const result = await graphjinQuery<T>({
      baseUrl: graphjinApiUrl(source.graphqlUrl, "graphql"),
      query,
      variables,
      headers: { authorization: `Bearer ${adminToken(caller)}` },
      // Admin console reads must not carry anyone's personal pack connections.
      configurationOnly: true,
      signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
    });
    const errors = (result.errors ?? []).map((error) => error.message);
    if (result.data == null) {
      return { ok: false, error: errors.join("; ") || "GraphJin returned no data" };
    }
    return { ok: true, data: result.data, errors };
  } catch (error) {
    return { ok: false, error: errorMessage(error) };
  }
}

/** GET a GraphJin REST route such as `openapi.json` or `eval/reports`. */
export async function graphjinAdminGet<T>(
  caller: GraphjinAdminCaller,
  path: string,
): Promise<GraphjinAdminResult<T>> {
  const source = await graphjinAdminSource(caller.orgId);
  if (!source) return { ok: false, error: GRAPHJIN_NO_SOURCE_ERROR };
  try {
    const res = await fetch(graphjinApiUrl(source.graphqlUrl, path), {
      headers: { accept: "application/json", authorization: `Bearer ${adminToken(caller)}` },
      redirect: "error",
      cache: "no-store",
      signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
    });
    const text = await res.text();
    let body: unknown = null;
    try {
      body = text ? JSON.parse(text) : null;
    } catch {
      return { ok: false, status: res.status, error: `GraphJin returned a non-JSON response (${res.status})` };
    }
    if (!res.ok) {
      const payload = body as { message?: unknown; errors?: Array<{ message?: unknown }> } | null;
      const message = payload?.message ?? payload?.errors?.[0]?.message;
      return {
        ok: false,
        status: res.status,
        error: typeof message === "string" && message ? message : `GraphJin returned ${res.status}`,
      };
    }
    return { ok: true, data: body as T, errors: [] };
  } catch (error) {
    return { ok: false, error: errorMessage(error) };
  }
}

export async function askGraphjinAdminAgent(
  caller: GraphjinAdminCaller,
  instruction: string,
  history: GraphjinAgentTurn[] = [],
): Promise<GraphjinAdminResult<GraphjinAgentResponse>> {
  const source = await graphjinAdminSource(caller.orgId);
  if (!source) return { ok: false, error: GRAPHJIN_NO_SOURCE_ERROR };
  try {
    const response = await askGraphjinAgent({
      baseUrl: source.graphqlUrl,
      token: adminToken(caller),
      signal: AbortSignal.timeout(AGENT_TIMEOUT_MS),
      request: {
        instruction,
        max_steps: GRAPHJIN_AGENT_MAX_STEPS,
        return_trace: false,
        ...(history.length > 0 ? { history } : {}),
      },
    });
    return { ok: true, data: response, errors: [] };
  } catch (error) {
    return { ok: false, error: errorMessage(error) };
  }
}

/** Parse a GraphJin `*_json` column, which arrives as a string or already decoded. */
export function parseGraphjinJson<T = unknown>(value: unknown, fallback: T): T {
  if (value == null || value === "") return fallback;
  if (typeof value !== "string") return value as T;
  try {
    return JSON.parse(value) as T;
  } catch {
    return fallback;
  }
}
