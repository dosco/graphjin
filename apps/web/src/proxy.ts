/**
 * Next.js 16 Proxy (formerly middleware) — auth gate for all routes.
 *
 * Two-mode model:
 *
 *   1. No sign-in provider is live → the app runs fully open. Every
 *      browser user gets the full app, no sign-in required. This is the
 *      single-operator / laptop deployment.
 *
 *   2. A sign-in provider is live (worker reports a provider on
 *      /admin/auth/status) → every route is gated. A request without a
 *      valid signed session cookie is 302'd to /signin with returnTo
 *      pointing at the original URL.
 *
 * The mode is detected per-request (cached 1s) by asking the worker,
 * so turning sign-in on or off takes effect on the next cache miss
 * without a web restart.
 *
 * Paths exempt from the gate:
 *   - /signin                — the sign-in page itself
 *   - /api/auth/*            — the SSO flow endpoints
 *   - _next/static, _next/image, favicon.ico — static assets
 *
 * Session verification here is the HMAC check ONLY. We do not hit the
 * database — the per-route handler (or `getCurrentUser`) does the
 * follow-up DB lookup if it needs the user row. Per the Next.js docs,
 * proxy should not be the sole authorisation layer; we use it for
 * optimistic redirects.
 *
 * Note: Server Functions appear as POSTs to the page they're invoked
 * from, so the matcher's page-level gate covers them. If a future
 * refactor moves a Server Function to a route the matcher excludes,
 * the gate is silently lost — re-verify auth inside any Server
 * Function that performs sensitive work.
 */

import { NextResponse, type NextRequest } from "next/server";
import { createHmac, timingSafeEqual } from "node:crypto";
import { get as httpGet } from "node:http";
import { get as httpsGet } from "node:https";
import { deriveSigningSecret } from "@neko/secret-crypt";
import { accessPolicyFor } from "./lib/access-policy";
import {
  readAuthGateMarker,
  registerAuthGateCacheReset,
  resetAuthGateCaches,
  writeAuthGateMarker,
} from "./lib/auth-gate-marker";

const SESSION_COOKIE_NAME = "openneko_session";
const PROVIDER_CACHE_TTL_MS = 1_000;
const WORKER_STATUS_TIMEOUT_MS = 1_500;
const WORKER_STATUS_MAX_BYTES = 64 * 1024;

interface ProviderProbe {
  installed: boolean;
  at: number;
}

let providerCache: ProviderProbe | null = null;

type ProviderStatusRequest = (url: string) => Promise<Response>;

/**
 * Proxy runs before every matched route, so it must not use Next's
 * request-scoped fetch wrapper for Docker-internal service names. A failed
 * wrapper request becomes a framework BubbledError that escapes userland
 * try/catch and produces Next's blank 500. Use Node HTTP directly so ordinary
 * network failures remain catchable and the proxy can fail open as designed.
 */
function requestWorkerStatus(url: string): Promise<Response> {
  return new Promise((resolve, reject) => {
    const target = new URL(url);
    const get = target.protocol === "https:" ? httpsGet : httpGet;
    const req = get(target, { headers: { accept: "application/json" } }, (res) => {
      const chunks: Buffer[] = [];
      let size = 0;
      res.on("data", (chunk: Buffer) => {
        size += chunk.length;
        if (size > WORKER_STATUS_MAX_BYTES) {
          req.destroy(new Error("worker auth status response is too large"));
          return;
        }
        chunks.push(chunk);
      });
      res.on("end", () => {
        resolve(
          new Response(Buffer.concat(chunks), {
            status: res.statusCode ?? 502,
          }),
        );
      });
    });
    req.setTimeout(WORKER_STATUS_TIMEOUT_MS, () => {
      req.destroy(new Error("worker auth status request timed out"));
    });
    req.on("error", reject);
  });
}

let providerStatusRequest: ProviderStatusRequest = requestWorkerStatus;

/** Test seam for deterministic provider responses. Passing null restores IO. */
export function _setProviderStatusRequestForTest(
  requester: ProviderStatusRequest | null,
): void {
  providerStatusRequest = requester ?? requestWorkerStatus;
}

function workerAdminBase(): string {
  return (process.env.WORKER_ADMIN_URL ?? "http://127.0.0.1:4100").replace(
    /\/+$/,
    "",
  );
}

/**
 * Ask the worker whether a sign-in provider is live. Cached for one
 * second to keep proxy overhead negligible under load while still
 * picking up a sign-in settings change within a second.
 *
 * When the worker is unreachable, the persisted auth-gate marker decides:
 * an install where SSO was ever live fails CLOSED (valid session cookies
 * still verify locally below; everything else goes to /signin), because
 * "worker restarting" must not drop the gate. An install that never
 * configured SSO keeps failing open — a dev environment with no worker
 * running shouldn't be locked out.
 */
export async function isSignInGateLive(): Promise<boolean> {
  const now = Date.now();
  if (providerCache && now - providerCache.at < PROVIDER_CACHE_TTL_MS) {
    return providerCache.installed;
  }
  try {
    const res = await providerStatusRequest(
      `${workerAdminBase()}/admin/auth/status`,
    );
    if (!res.ok) {
      providerCache = { installed: installedWhileWorkerUnavailable(), at: now };
      return providerCache.installed;
    }
    const body = (await res.json()) as { provider: { pluginName: string } | null };
    const installed = body.provider != null;
    providerCache = { installed, at: now };
    writeAuthGateMarker({ provider: body.provider ?? null });
    return installed;
  } catch {
    providerCache = { installed: installedWhileWorkerUnavailable(), at: now };
    return providerCache.installed;
  }
}

function installedWhileWorkerUnavailable(): boolean {
  return readAuthGateMarker()?.provider != null;
}

registerAuthGateCacheReset(() => {
  providerCache = null;
});

/** Test seam — clears this cache, lib/auth's, and the marker copy. */
export function _resetProviderCache(): void {
  resetAuthGateCaches();
}

/**
 * HMAC-verify the signed session cookie. Mirrors `decodeSession` in
 * @/lib/auth but doesn't throw on missing/short secret — the proxy
 * treats that as "no valid session" so a misconfigured deployment
 * redirects to /signin rather than 500ing every page load.
 */
export function verifySessionCookie(value: string | undefined): boolean {
  return sessionUserId(value) !== null;
}

/** The signed-in user id, or null when the cookie is missing or invalid. */
export function sessionUserId(value: string | undefined): string | null {
  if (!value) return null;
  // Same resolution as lib/auth's sessionSecret(): explicit env wins;
  // otherwise a stable secret derived from the deployment secret-key.
  // (This copy still never throws — the proxy treats any failure as "no
  // valid session".)
  let secret = process.env.OPENNEKO_SESSION_SECRET;
  if (!secret) {
    try {
      secret = deriveSigningSecret("session-cookie:v1").toString("base64");
    } catch {
      return null;
    }
  }
  if (secret.length < 32) return null;
  const parts = value.split(".");
  if (parts.length !== 3) return null;
  const [userId, expiresAtRaw, mac] = parts;
  if (!userId || !expiresAtRaw || !mac) return null;
  const body = `${userId}.${expiresAtRaw}`;
  const expected = createHmac("sha256", secret).update(body).digest("base64url");
  const a = Buffer.from(mac);
  const b = Buffer.from(expected);
  if (a.length !== b.length) return null;
  if (!timingSafeEqual(a, b)) return null;
  const expiresAt = Number.parseInt(expiresAtRaw, 10);
  if (!Number.isFinite(expiresAt)) return null;
  if (expiresAt < Math.floor(Date.now() / 1000)) return null;
  return userId;
}

const ADMIN_CACHE_TTL_MS = 5_000;
const administrators = new Map<string, { administrator: boolean; at: number }>();

/** Test seam: drop the cached Administrators answers. */
export function _resetAdministratorCacheForTest(): void {
  administrators.clear();
}

/**
 * Administrators membership for the signed-in user. Cached for five
 * seconds: the route handler behind this check reads the membership again,
 * so a change applies there at once and here on the next few requests.
 */
async function isAdministrator(userId: string): Promise<boolean> {
  const now = Date.now();
  const cached = administrators.get(userId);
  if (cached && now - cached.at < ADMIN_CACHE_TTL_MS) return cached.administrator;
  try {
    const [{ resolveUserGroups }, { getOrgId }] = await Promise.all([
      import("@neko/db"),
      import("./lib/db"),
    ]);
    const groups = await resolveUserGroups(await getOrgId(), userId);
    administrators.set(userId, { administrator: groups.administrator, at: now });
    return groups.administrator;
  } catch {
    // The route handler checks again, so a database blip must not lock an
    // administrator out of the pages that report it.
    return true;
  }
}

function refuse(request: NextRequest, pathname: string): NextResponse {
  if (pathname.startsWith("/api/")) {
    return NextResponse.json({ error: "admin only" }, { status: 403 });
  }
  const url = request.nextUrl.clone();
  url.pathname = "/";
  url.search = "";
  return NextResponse.redirect(url, { status: 302 });
}

/** The app opens on chat. The Admin button in the profile menu opens the console. */
function chooseSurface(request: NextRequest, gated: NextResponse): NextResponse {
  if (gated.status !== 200 || gated.headers.get("x-middleware-next") !== "1") return gated;
  if (request.nextUrl.pathname !== "/") return gated;
  const url = request.nextUrl.clone();
  url.pathname = "/chat";
  url.search = "";
  return NextResponse.redirect(url, { status: 302 });
}

export async function proxy(request: NextRequest): Promise<NextResponse> {
  return chooseSurface(request, await gate(request));
}

async function gate(request: NextRequest): Promise<NextResponse> {
  const pathname = request.nextUrl.pathname;
  const policy = accessPolicyFor(pathname);
  if (!policy) {
    // No entry covers this path. Refuse it rather than guess; add the area
    // to ACCESS_POLICIES (the access-policy test names it).
    return pathname.startsWith("/api/")
      ? NextResponse.json({ error: "not found" }, { status: 404 })
      : new NextResponse(null, { status: 404 });
  }
  if (policy.rule === "public" || policy.rule === "token") {
    return NextResponse.next();
  }
  if (!(await isSignInGateLive())) {
    // Single-operator install: one person runs everything.
    return NextResponse.next();
  }
  const userId = sessionUserId(request.cookies.get(SESSION_COOKIE_NAME)?.value);
  if (!userId) {
    const returnTo = pathname + request.nextUrl.search;
    const url = request.nextUrl.clone();
    url.pathname = "/signin";
    url.search = `?returnTo=${encodeURIComponent(returnTo)}`;
    return NextResponse.redirect(url, { status: 302 });
  }
  if (policy.rule === "admin" && !(await isAdministrator(userId))) {
    return refuse(request, pathname);
  }
  return NextResponse.next();
}

export const config = {
  // Match every route except static assets: ACCESS_POLICIES decides what
  // each path needs, and the sign-in, SSO setup and OAuth callback paths
  // are `public` or `token` there.
  // Everything under _next/ belongs to the framework, including the dev
  // hot-reload socket. The gate 404s what it does not recognise, which
  // stops the page hydrating and kills every button on it.
  // A path that ends in a file extension is a file in public/ (cat.png,
  // icon.png, robots.txt). No page or API route ends that way.
  // Negative lookahead is a constant here so Next can statically
  // analyse it at build time (per the proxy.md API reference).
  matcher: ["/((?!_next/|favicon\\.ico|.*\\.[a-zA-Z0-9]+$).*)"],
};
