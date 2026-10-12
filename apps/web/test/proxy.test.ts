import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { createHmac } from "node:crypto";
import { createServer } from "node:http";
import {
  _resetAdministratorCacheForTest,
  _resetProviderCache,
  _setProviderStatusRequestForTest,
  isAuthPluginInstalled,
  proxy,
  verifySessionCookie,
} from "../src/proxy";

const resolveUserGroups = vi.fn(async () => ({ administrator: false, groupIds: [], slugs: [], ssoGroupIds: [] }));
vi.mock("@neko/db", () => ({ resolveUserGroups: (...args: unknown[]) => resolveUserGroups(...(args as [])) }));
vi.mock("../src/lib/db", () => ({ getOrgId: async () => "org-1" }));

const SECRET = "a".repeat(64);

function freshFetch(
  impl: (url: string) => Response | Promise<Response>,
): (url: string) => Promise<Response> {
  return (url: string) => Promise.resolve(impl(url));
}

function mintCookie(opts: {
  userId?: string;
  expiresAt?: number;
  secret?: string;
} = {}): string {
  const userId = opts.userId ?? "usr_abc";
  const expiresAt =
    opts.expiresAt ?? Math.floor(Date.now() / 1000) + 60 * 60;
  const secret = opts.secret ?? SECRET;
  const body = `${userId}.${expiresAt}`;
  const mac = createHmac("sha256", secret).update(body).digest("base64url");
  return `${body}.${mac}`;
}

function request(url: string, cookie?: string): NextRequest {
  const headers = new Headers();
  if (cookie) headers.set("cookie", `openneko_session=${cookie}`);
  return new NextRequest(url, { headers });
}

beforeEach(() => {
  process.env.OPENNEKO_SESSION_SECRET = SECRET;
  _resetProviderCache();
  _setProviderStatusRequestForTest(null);
});

afterEach(() => {
  delete process.env.OPENNEKO_SESSION_SECRET;
  delete process.env.WORKER_ADMIN_URL;
  _resetProviderCache();
  _setProviderStatusRequestForTest(null);
  vi.restoreAllMocks();
});

describe("verifySessionCookie", () => {
  it("accepts a freshly minted cookie", () => {
    expect(verifySessionCookie(mintCookie())).toBe(true);
  });

  it("rejects an empty / undefined cookie", () => {
    expect(verifySessionCookie(undefined)).toBe(false);
    expect(verifySessionCookie("")).toBe(false);
  });

  it("rejects a cookie signed with a different secret", () => {
    const cookie = mintCookie({ secret: "b".repeat(64) });
    expect(verifySessionCookie(cookie)).toBe(false);
  });

  it("rejects an expired cookie", () => {
    const cookie = mintCookie({
      expiresAt: Math.floor(Date.now() / 1000) - 60,
    });
    expect(verifySessionCookie(cookie)).toBe(false);
  });

  it("rejects a malformed cookie", () => {
    expect(verifySessionCookie("only.two")).toBe(false);
    expect(verifySessionCookie("a.b.c.d")).toBe(false);
  });

  it("rejects when the session secret is missing or too short", () => {
    delete process.env.OPENNEKO_SESSION_SECRET;
    expect(verifySessionCookie(mintCookie())).toBe(false);
    process.env.OPENNEKO_SESSION_SECRET = "tooshort";
    expect(verifySessionCookie(mintCookie())).toBe(false);
  });
});

describe("isAuthPluginInstalled", () => {
  it("uses Node HTTP rather than Next's request fetch wrapper", async () => {
    const server = createServer((_req, res) => {
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ provider: null }));
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("missing address");
    process.env.WORKER_ADMIN_URL = `http://127.0.0.1:${address.port}`;
    const fetchMock = vi.fn(async () => {
      throw new Error("Next fetch wrapper must not run in proxy");
    });
    vi.stubGlobal("fetch", fetchMock as unknown as typeof fetch);

    try {
      expect(await isAuthPluginInstalled()).toBe(false);
      expect(fetchMock).not.toHaveBeenCalled();
    } finally {
      await new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      );
    }
  });

  it("returns true when the worker reports a provider", async () => {
    _setProviderStatusRequestForTest(
      freshFetch(() =>
        Response.json({ provider: { pluginName: "@x/y", providerLabel: "X" } }),
      ),
    );
    expect(await isAuthPluginInstalled()).toBe(true);
  });

  it("returns false when the worker reports no provider", async () => {
    _setProviderStatusRequestForTest(
      freshFetch(() => Response.json({ provider: null })),
    );
    expect(await isAuthPluginInstalled()).toBe(false);
  });

  it("fails open (false) when the worker is unreachable", async () => {
    _setProviderStatusRequestForTest(
      () => Promise.reject(new Error("ECONNREFUSED")),
    );
    expect(await isAuthPluginInstalled()).toBe(false);
  });

  it("caches the result for the TTL window", async () => {
    let calls = 0;
    _setProviderStatusRequestForTest(
      freshFetch(() => {
        calls++;
        return Response.json({ provider: { pluginName: "@x/y", providerLabel: "X" } });
      }),
    );
    expect(await isAuthPluginInstalled()).toBe(true);
    expect(await isAuthPluginInstalled()).toBe(true);
    expect(await isAuthPluginInstalled()).toBe(true);
    expect(calls).toBe(1);
  });
});

describe("proxy", () => {
  it("lets every request through when no SSO plugin is installed", async () => {
    _setProviderStatusRequestForTest(
      freshFetch(() => Response.json({ provider: null })),
    );
    const res = await proxy(request("https://app.example.com/runs"));
    expect(res.status).toBe(200);
    // NextResponse.next() carries the x-middleware-next sentinel header.
    expect(res.headers.get("x-middleware-next")).toBe("1");
  });

  it("lets a signed-in user through when the SSO plugin is installed", async () => {
    _setProviderStatusRequestForTest(
      freshFetch(() =>
        Response.json({ provider: { pluginName: "@x/y", providerLabel: "X" } }),
      ),
    );
    const res = await proxy(
      request("https://app.example.com/runs", mintCookie()),
    );
    expect(res.headers.get("x-middleware-next")).toBe("1");
  });

  it("redirects to /signin with returnTo when no session cookie is present", async () => {
    _setProviderStatusRequestForTest(
      freshFetch(() =>
        Response.json({ provider: { pluginName: "@x/y", providerLabel: "X" } }),
      ),
    );
    const res = await proxy(
      request("https://app.example.com/runs?foo=bar"),
    );
    expect(res.status).toBe(302);
    const loc = res.headers.get("location") ?? "";
    expect(loc).toContain("/signin");
    expect(loc).toContain(
      `returnTo=${encodeURIComponent("/runs?foo=bar")}`,
    );
  });

  it("redirects when the session cookie has been tampered with", async () => {
    _setProviderStatusRequestForTest(
      freshFetch(() =>
        Response.json({ provider: { pluginName: "@x/y", providerLabel: "X" } }),
      ),
    );
    const tampered = mintCookie({ secret: "b".repeat(64) });
    const res = await proxy(
      request("https://app.example.com/runs", tampered),
    );
    expect(res.status).toBe(302);
    expect(res.headers.get("location") ?? "").toContain("/signin");
  });

  it("refuses a path no access policy covers", async () => {
    _setProviderStatusRequestForTest(freshFetch(() => Response.json({ provider: null })));
    expect((await proxy(request("https://app.example.com/api/not-an-area"))).status).toBe(404);
    expect((await proxy(request("https://app.example.com/not-an-area"))).status).toBe(404);
  });

  it("lets the sign-in flow through without a session", async () => {
    _setProviderStatusRequestForTest(
      freshFetch(() => Response.json({ provider: { pluginName: "@x/y", providerLabel: "X" } })),
    );
    for (const path of ["/signin", "/api/auth/begin", "/api/version"]) {
      const res = await proxy(request(`https://app.example.com${path}`));
      expect(res.headers.get("x-middleware-next")).toBe("1");
    }
  });

  it("keeps a member out of admin routes and lets an administrator in", async () => {
    _setProviderStatusRequestForTest(
      freshFetch(() => Response.json({ provider: { pluginName: "@x/y", providerLabel: "X" } })),
    );
    _resetAdministratorCacheForTest();
    resolveUserGroups.mockResolvedValue({ administrator: false, groupIds: [], slugs: [], ssoGroupIds: [] });
    const denied = await proxy(request("https://app.example.com/api/admin/groups", mintCookie()));
    expect(denied.status).toBe(403);
    const page = await proxy(request("https://app.example.com/admin/users", mintCookie()));
    expect(page.status).toBe(302);
    expect(page.headers.get("location") ?? "").toContain("/");

    _resetAdministratorCacheForTest();
    resolveUserGroups.mockResolvedValue({ administrator: true, groupIds: [], slugs: [], ssoGroupIds: [] });
    const allowed = await proxy(request("https://app.example.com/api/admin/groups", mintCookie()));
    expect(allowed.headers.get("x-middleware-next")).toBe("1");
  });

  it("lets a member reach a signed-in route without asking for groups", async () => {
    _setProviderStatusRequestForTest(
      freshFetch(() => Response.json({ provider: { pluginName: "@x/y", providerLabel: "X" } })),
    );
    _resetAdministratorCacheForTest();
    resolveUserGroups.mockClear();
    const res = await proxy(request("https://app.example.com/api/work/skills", mintCookie()));
    expect(res.headers.get("x-middleware-next")).toBe("1");
    expect(resolveUserGroups).not.toHaveBeenCalled();
  });

  it("redirects when the session cookie has expired", async () => {
    _setProviderStatusRequestForTest(
      freshFetch(() =>
        Response.json({ provider: { pluginName: "@x/y", providerLabel: "X" } }),
      ),
    );
    const expired = mintCookie({
      expiresAt: Math.floor(Date.now() / 1000) - 60,
    });
    const res = await proxy(
      request("https://app.example.com/runs", expired),
    );
    expect(res.status).toBe(302);
  });
});

describe("chat-first landing", () => {
  const openMode = () =>
    _setProviderStatusRequestForTest(freshFetch(() => Response.json({ provider: null })));
  const withSurface = (url: string, surface: string) =>
    new NextRequest(url, { headers: { cookie: `openneko_surface=${surface}` } });

  it("opens chat at the root", async () => {
    openMode();
    const res = await proxy(request("http://localhost/"));
    expect(res.status).toBe(302);
    expect(new URL(res.headers.get("location")!).pathname).toBe("/chat");
  });

  it("enters the console from the Admin button and stays there", async () => {
    openMode();
    const enter = await proxy(request("http://localhost/?console=1"));
    expect(enter.status).toBe(302);
    expect(new URL(enter.headers.get("location")!).search).toBe("");
    expect(enter.cookies.get("openneko_surface")?.value).toBe("console");
    const home = await proxy(withSurface("http://localhost/", "console"));
    expect(home.headers.get("x-middleware-next")).toBe("1");
  });

  it("returns to chat when the reader opens a chat", async () => {
    openMode();
    const res = await proxy(withSurface("http://localhost/chat", "console"));
    expect(res.headers.get("set-cookie")).toMatch(/openneko_surface=;/);
  });

  it("sends a signed-out reader to sign-in before choosing a surface", async () => {
    _setProviderStatusRequestForTest(freshFetch(() => Response.json({ provider: "oidc" })));
    const res = await proxy(request("http://localhost/"));
    expect(new URL(res.headers.get("location")!).pathname).toBe("/signin");
  });
});
