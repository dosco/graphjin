/**
 * Synthesise a NextRequest and call a route handler in-process.
 *
 * Next.js 16's NextRequest extends the standard Request, so the simplest
 * approach is to construct a Request and wrap it. We don't need full
 * route-context behaviour (cookies, params, etc.) for handler tests —
 * just the URL, method, body, and query.
 */

import { NextRequest } from "next/server";

export type CallRouteOptions = {
  method?: "GET" | "POST" | "PUT" | "DELETE" | "PATCH";
  url?: string;
  body?: unknown;
  query?: Record<string, string>;
  headers?: Record<string, string>;
};

export async function callRoute(
  handler: (req: NextRequest) => Promise<Response> | Response,
  opts: CallRouteOptions = {},
): Promise<{ status: number; body: unknown; headers: Headers }> {
  const url = new URL(opts.url ?? "http://localhost:3000/test");
  for (const [k, v] of Object.entries(opts.query ?? {})) {
    url.searchParams.set(k, v);
  }
  const init: ConstructorParameters<typeof NextRequest>[1] = {
    method: opts.method ?? "GET",
    headers: {
      "content-type": "application/json",
      ...(opts.headers ?? {}),
    },
  };
  if (opts.body !== undefined) {
    init.body = typeof opts.body === "string" ? opts.body : JSON.stringify(opts.body);
  }

  const req = new NextRequest(url, init);
  const res = await handler(req);
  const text = await res.text();
  let parsed: unknown;
  try {
    parsed = text ? JSON.parse(text) : null;
  } catch {
    parsed = text;
  }
  return { status: res.status, body: parsed, headers: res.headers };
}
