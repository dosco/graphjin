/**
 * Tests for the worker's /health + /admin/reconnect HTTP handler. The
 * /admin/reconnect signal is fired by the web app's change-password
 * handler after rotating the Postgres password; the worker must respond
 * 202 and exit cleanly so the supervisor (`tsx watch` / Cloud Run)
 * restarts it with fresh credentials.
 */

import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  reportAuditLoggingFailure,
  resetAuditLoggingHealthForTesting,
} from "@neko/llm/workflows";
import { createAdminHandler } from "../src/admin-server";
import { PackPreflightError } from "../src/packs/preflight.js";

async function startServer(handler: ReturnType<typeof createAdminHandler>) {
  const server = createServer(handler);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const port = (server.address() as AddressInfo).port;
  return {
    port,
    close: () => new Promise<void>((resolve, reject) =>
      server.close((err) => (err ? reject(err) : resolve())),
    ),
  };
}

describe("worker admin HTTP handler", () => {
  let exit: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    exit = vi.fn();
  });

  afterEach(() => {
    vi.useRealTimers();
    resetAuditLoggingHealthForTesting();
    vi.restoreAllMocks();
  });

  it("GET /health returns 200 ok", async () => {
    const srv = await startServer(createAdminHandler({ exit }));
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/health`);
      expect(res.status).toBe(200);
      expect(await res.text()).toBe("ok");
      expect(exit).not.toHaveBeenCalled();
    } finally {
      await srv.close();
    }
  });

  it("GET /health includes scheduler readiness", async () => {
    const getHealth = vi.fn(() => ({
      status: "ok" as const,
      running: false,
      stale: false,
      startedAt: "2026-08-26T07:00:00.000Z",
      lastAttemptAt: "2026-08-26T07:00:13.000Z",
      lastSuccessAt: "2026-08-26T07:00:13.000Z",
      lastErrorAt: null,
      lastError: null,
      consecutiveFailures: 0,
      materialized: 1,
      dispatched: 1,
      recovered: 0,
    }));
    const srv = await startServer(
      createAdminHandler({ scheduler: { getHealth } }),
    );
    try {
      const health = await fetch(`http://127.0.0.1:${srv.port}/health`);
      expect(health.status).toBe(200);
      expect(await health.text()).toBe("ok");

      const detail = await fetch(
        `http://127.0.0.1:${srv.port}/health/scheduler`,
      );
      expect(detail.status).toBe(200);
      expect(await detail.json()).toMatchObject({
        status: "ok",
        lastSuccessAt: "2026-08-26T07:00:13.000Z",
        materialized: 1,
        dispatched: 1,
      });
    } finally {
      await srv.close();
    }
  });

  it("GET /health fails when the scheduler stops making progress", async () => {
    const getHealth = vi.fn(() => ({
      status: "degraded" as const,
      running: false,
      stale: true,
      startedAt: "2026-08-26T07:00:00.000Z",
      lastAttemptAt: "2026-08-26T07:00:13.000Z",
      lastSuccessAt: "2026-08-26T07:00:13.000Z",
      lastErrorAt: null,
      lastError: null,
      consecutiveFailures: 0,
      materialized: 0,
      dispatched: 0,
      recovered: 0,
    }));
    const srv = await startServer(
      createAdminHandler({ scheduler: { getHealth } }),
    );
    try {
      const health = await fetch(`http://127.0.0.1:${srv.port}/health`);
      expect(health.status).toBe(503);
      expect(await health.text()).toBe("degraded");

      const detail = await fetch(
        `http://127.0.0.1:${srv.port}/health/scheduler`,
      );
      expect(detail.status).toBe(503);
      expect(await detail.json()).toMatchObject({
        status: "degraded",
        stale: true,
      });
    } finally {
      await srv.close();
    }
  });

  it("GET /health/security exposes degraded audit logging health", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    reportAuditLoggingFailure(
      {
        orgId: "org_test",
        entityKind: "work_run",
        entityId: "run_test",
        event: "run:failed",
      },
      new Error("audit database unavailable"),
    );
    const srv = await startServer(createAdminHandler({ exit }));
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/health/security`);
      expect(res.status).toBe(200);
      expect(await res.json()).toMatchObject({
        status: "degraded",
        auditLogging: {
          healthy: false,
          failureCount: 1,
          lastError: "audit database unavailable",
        },
      });
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/reconnect returns 202 and triggers process.exit(0)", async () => {
    const srv = await startServer(
      createAdminHandler({ exit, exitDelayMs: 0 }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/reconnect`, {
        method: "POST",
      });
      expect(res.status).toBe(202);
      expect(await res.text()).toBe("restarting");
      // setTimeout(0) microtask — flush.
      await new Promise((r) => setTimeout(r, 5));
      expect(exit).toHaveBeenCalledTimes(1);
      expect(exit).toHaveBeenCalledWith(0);
    } finally {
      await srv.close();
    }
  });

  it("GET /admin/reconnect (wrong method) returns 404 and does not exit", async () => {
    const srv = await startServer(createAdminHandler({ exit }));
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/reconnect`);
      expect(res.status).toBe(404);
      expect(exit).not.toHaveBeenCalled();
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/action-requests/create delegates to worker preflight", async () => {
    const create = vi.fn(async () => ({ id: "action-prepared", status: "approved" }));
    const srv = await startServer(
      createAdminHandler({ actionRequests: { create } }),
    );
    try {
      const input = {
        orgId: "org-test",
        kind: "user_admin",
        scope: "internal",
        status: "pending_approval",
        payload: { email: "new@example.com" },
      };
      const res = await fetch(
        `http://127.0.0.1:${srv.port}/admin/action-requests/create`,
        {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify(input),
        },
      );
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({ id: "action-prepared", status: "approved" });
      expect(create).toHaveBeenCalledWith(input);
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/action-requests/create fails closed before preflight is ready", async () => {
    const srv = await startServer(createAdminHandler());
    try {
      const res = await fetch(
        `http://127.0.0.1:${srv.port}/admin/action-requests/create`,
        { method: "POST", body: "{}" },
      );
      expect(res.status).toBe(503);
      expect(await res.json()).toEqual({
        error: "action request preflight is not ready",
      });
    } finally {
      await srv.close();
    }
  });

  it("unknown path returns 404", async () => {
    const srv = await startServer(createAdminHandler({ exit }));
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/whatever`);
      expect(res.status).toBe(404);
      expect(exit).not.toHaveBeenCalled();
    } finally {
      await srv.close();
    }
  });
});

describe("worker solution-pack admin routes", () => {
  it("routes the supported pack lifecycle without filtering artifacts", async () => {
    const surface = {
      list: vi.fn(async () => [{ id: "magento", installed: false }]),
      inspect: vi.fn(async (packId: string) => ({ packId })),
      plan: vi.fn(async (packId: string) => ({ packId, entries: [{ key: "action.add_internal_order_comment" }] })),
      status: vi.fn(async (packId: string) => ({ packId, status: "installed" })),
      doctor: vi.fn(async (packId: string) => ({ packId, status: "degraded" })),
      install: vi.fn(async (packId: string, input: Record<string, unknown>) => ({
        packId,
        status: "installed",
        input,
      })),
      configure: vi.fn(async (packId: string, input: Record<string, unknown>) => ({
        packId,
        status: "installed",
        input,
      })),
      upgrade: vi.fn(async (packId: string, input: Record<string, unknown>) => ({
        packId,
        status: "installed",
        input,
      })),
      uninstall: vi.fn(async (packId: string, input: Record<string, unknown>) => ({
        packId,
        status: "removed",
        input,
      })),
      checkContextUpdates: vi.fn(async () => ({ tip: "abc", skills: [{ name: "shortfall", status: "update" }], packs: [] })),
      upload: vi.fn(async () => ({ packId: "service-health", version: "0.1.0" })),
      review: vi.fn(async () => ({ reviewHash: "reviewed" })),
      oauthStatus: vi.fn(async () => ({ connected: true, account: { label: "owner@example.test" } })),
      beginOAuth: vi.fn(async () => ({ authorizationUrl: "https://accounts.example.test/authorize" })),
      completeOAuth: vi.fn(async () => ({ connected: true })),
      disconnectOAuth: vi.fn(async () => true),
      magentoStoreManagement: vi.fn(async () => ({ controls: [{ domain: "catalog" }] })),
      updateMagentoStoreManagement: vi.fn(async (input: Record<string, unknown>) => ({ input })),
    };
    const srv = await startServer(createAdminHandler({ packs: surface }));
    try {
      const updates = await fetch(`http://127.0.0.1:${srv.port}/admin/context-remote/updates`);
      expect(updates.status).toBe(200);
      expect(await updates.json()).toEqual({ updates: { tip: "abc", skills: [{ name: "shortfall", status: "update" }], packs: [] } });
      surface.checkContextUpdates.mockRejectedValueOnce(new Error("Connect a remote repository first."));
      const refused = await fetch(`http://127.0.0.1:${srv.port}/admin/context-remote/updates`);
      expect(refused.status).toBe(400);
      expect(await refused.json()).toEqual({ error: "Connect a remote repository first." });

      const list = await fetch(`http://127.0.0.1:${srv.port}/admin/packs`);
      expect(list.status).toBe(200);
      expect(await list.json()).toEqual({ packs: [{ id: "magento", installed: false }] });

      for (const [suffix, method] of [
        ["", "inspect"],
        ["/plan", "plan"],
        ["/status", "status"],
        ["/doctor", "doctor"],
      ] as const) {
        const response = await fetch(
          `http://127.0.0.1:${srv.port}/admin/packs/magento${suffix}`,
        );
        expect(response.status).toBe(200);
        expect(surface[method]).toHaveBeenCalledWith("magento");
      }

      const input = {
        inputs: { "magento.base_url": "http://magento.test" },
        secrets: { "database.analytics_username": "analytics" },
      };
      const install = await fetch(
        `http://127.0.0.1:${srv.port}/admin/packs/magento/install`,
        {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify(input),
        },
      );
      expect(install.status).toBe(200);
      expect(surface.install).toHaveBeenCalledWith("magento", input);

      surface.install.mockRejectedValueOnce(new PackPreflightError(
        "account_profile",
        "graphjin/saved-queries/account_profile.gql",
        ["table not found: customer_api_get_profile"],
      ));
      const failedInstall = await fetch(
        `http://127.0.0.1:${srv.port}/admin/packs/google-workspace/install`,
        { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(input) },
      );
      expect(failedInstall.status).toBe(400);
      expect(await failedInstall.json()).toMatchObject({
        error: expect.stringContaining("graphjin/saved-queries/account_profile.gql"),
        diagnostic: {
          code: "pack_query_preflight_failed",
          phase: "query_preflight",
          artifact: { kind: "saved_query", name: "account_profile", path: "graphjin/saved-queries/account_profile.gql" },
          causes: ["table not found: customer_api_get_profile"],
        },
      });

      for (const action of ["configure", "upgrade", "uninstall"] as const) {
        const response = await fetch(
          `http://127.0.0.1:${srv.port}/admin/packs/magento/${action}`,
          {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify(input),
          },
        );
        expect(response.status).toBe(200);
        expect(surface[action]).toHaveBeenCalledWith("magento", input);
      }

      const upload = await fetch(`http://127.0.0.1:${srv.port}/admin/packs/upload`, {
        method: "POST", headers: { "content-type": "application/zip", "x-openneko-actor": "admin-1" }, body: Buffer.from("fixture ZIP"),
      });
      expect(upload.status).toBe(200);
      expect(surface.upload).toHaveBeenCalledWith(Buffer.from("fixture ZIP"), expect.objectContaining({ actorUserId: "admin-1" }));
      const tooLarge = await fetch(`http://127.0.0.1:${srv.port}/admin/packs/upload`, {
        method: "POST", headers: { "content-type": "application/zip" }, body: Buffer.alloc(16 * 1024 * 1024 + 1),
      });
      expect(tooLarge.status).toBe(413);
      expect(surface.upload).toHaveBeenCalledTimes(1);
      const review = await fetch(`http://127.0.0.1:${srv.port}/admin/packs/service-health/review`, {
        method: "POST", body: JSON.stringify({ operation: "upgrade", version: "0.2.0" }),
      });
      expect(review.status).toBe(200);
      expect(surface.review).toHaveBeenCalledWith("service-health", { operation: "upgrade", version: "0.2.0" }, "upgrade");
      surface.review.mockRejectedValueOnce(new Error("manifest input service.base_url is missing"));
      const failedReview = await fetch(`http://127.0.0.1:${srv.port}/admin/packs/service-health/review`, {
        method: "POST", body: JSON.stringify({ operation: "install" }),
      });
      expect(await failedReview.json()).toMatchObject({
        error: "manifest input service.base_url is missing",
        diagnostic: {
          code: "pack_operation_failed",
          phase: "review",
          causes: ["manifest input service.base_url is missing"],
          nextStep: expect.any(String),
        },
      });
      const management = await fetch(
        `http://127.0.0.1:${srv.port}/admin/packs/magento/store-management`,
      );
      expect(management.status).toBe(200);
      expect(await management.json()).toEqual({ controls: [{ domain: "catalog" }] });

      const managementInput = { action: "update_domain", domain: "catalog", enabled: true };
      const updateManagement = await fetch(
        `http://127.0.0.1:${srv.port}/admin/packs/magento/store-management`,
        {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify(managementInput),
        },
      );
      expect(updateManagement.status).toBe(200);
      expect(surface.updateMagentoStoreManagement).toHaveBeenCalledWith(managementInput);

      const oauthBase = `http://127.0.0.1:${srv.port}/admin/packs/google-workspace/oauth/workspace`;
      expect((await fetch(`${oauthBase}/status`)).status).toBe(200);
      expect(surface.oauthStatus).toHaveBeenCalledWith("google-workspace", "workspace");
      for (const action of ["begin", "complete"] as const) {
        const response = await fetch(`${oauthBase}/${action}`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ state: "state" }),
        });
        expect(response.status).toBe(200);
        expect(surface[action === "begin" ? "beginOAuth" : "completeOAuth"])
          .toHaveBeenCalledWith("google-workspace", "workspace", { state: "state" });
      }
      expect((await fetch(`${oauthBase}/disconnect`, { method: "POST", body: "{}" })).status).toBe(200);
      expect(surface.disconnectOAuth).toHaveBeenCalledWith("google-workspace", "workspace");
    } finally {
      await srv.close();
    }
  });

  it("fails closed when no pack surface is wired", async () => {
    const srv = await startServer(createAdminHandler());
    try {
      const response = await fetch(`http://127.0.0.1:${srv.port}/admin/packs`);
      expect(response.status).toBe(503);
    } finally {
      await srv.close();
    }
  });

  it("does not accept trailing junk as a pack lifecycle route", async () => {
    const inspect = vi.fn(async () => ({ packId: "magento" }));
    const srv = await startServer(createAdminHandler({
      packs: {
        list: async () => [],
        inspect,
        plan: async () => ({}),
        status: async () => ({}),
        doctor: async () => ({}),
        install: async () => ({}),
        configure: async () => ({}),
        upgrade: async () => ({}),
        uninstall: async () => ({}),
        checkContextUpdates: async () => ({}),
        upload: async () => ({}),
        review: async () => ({}),
        magentoStoreManagement: async () => ({}),
        updateMagentoStoreManagement: async () => ({}),
      },
    }));
    try {
      const response = await fetch(
        `http://127.0.0.1:${srv.port}/admin/packs/magento/status-extra`,
      );
      expect(response.status).toBe(404);
      expect(inspect).not.toHaveBeenCalled();
    } finally {
      await srv.close();
    }
  });
});

describe("worker admin /admin/action-descriptors", () => {
  it("returns the built-in action descriptors", async () => {
    const srv = await startServer(
      createAdminHandler({
        actions: {
          getActionDescriptors: () => [{ kind: "slack.send_message", description: "Send a Slack message", default_mode: "ask" }],
        },
      }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/action-descriptors`);
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({
        descriptors: [{ kind: "slack.send_message", description: "Send a Slack message", default_mode: "ask" }],
      });
    } finally {
      await srv.close();
    }
  });

  it("returns an empty list when no surface is wired, and the plugin routes are gone", async () => {
    const srv = await startServer(createAdminHandler());
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/action-descriptors`);
      expect(await res.json()).toEqual({ descriptors: [] });
      for (const path of ["/admin/plugins/status", "/admin/plugins/settings", "/admin/plugins/action-descriptors"]) {
        expect((await fetch(`http://127.0.0.1:${srv.port}${path}`)).status).toBe(404);
      }
    } finally {
      await srv.close();
    }
  });
});

describe("worker admin /admin/auth/*", () => {
  it("GET /admin/auth/status returns { provider: null } when no auth surface is wired", async () => {
    const srv = await startServer(createAdminHandler());
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/auth/status`);
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({ provider: null, pending: null });
    } finally {
      await srv.close();
    }
  });

  it("GET /admin/auth/status returns provider info when one is registered", async () => {
    const srv = await startServer(
      createAdminHandler({
        auth: {
          authSignInReady: () => true,
          getAuthProvider: () => ({
            pluginName: "scalekit",
            providerLabel: "Scalekit",
          }),
          beginAuth: async () => ({ authorizationUrl: "https://x" }),
          completeAuth: async () => ({
            sub: "u",
            email: "u@e.com",
            groups: [],
          }),
        },
      }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/auth/status`);
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({
        provider: {
          pluginName: "scalekit",
          providerLabel: "Scalekit",
          provisioning: "automatic",
          loginHintRequired: false,
        },
        pending: null,
      });
    } finally {
      await srv.close();
    }
  });

  it.each(["manual", "automatic"] as const)("GET /admin/auth/status keeps %s setup pending until the admin is ready", async (provisioning) => {
    let hasAdmin = false;
    const handler = createAdminHandler({
      auth: {
        authSignInReady: () => true,
        getAuthEnvGaps: () => [],
        getAuthConfiguredEnvKeys: () => ["MAGIC_LINK_FROM", "RESEND_API_KEY"],
        hasProvisionedAdmin: async () => hasAdmin,
        soloAdminNeedsEmail: async () => provisioning === "automatic" && !hasAdmin,
        getAuthProvider: () => ({
          pluginName: "magic-link",
          providerLabel: "Email link",
          provisioning,
          loginHintRequired: true,
        }),
        beginAuth: async () => ({ authorizationUrl: "https://x" }),
        completeAuth: async () => ({
          sub: "u",
          email: "u@e.com",
          groups: [],
        }),
      },
    });
    const srv = await startServer(handler);
    try {
      const pendingRes = await fetch(
        `http://127.0.0.1:${srv.port}/admin/auth/status`,
      );
      expect(pendingRes.status).toBe(200);
      const pendingBody = (await pendingRes.json()) as {
        provider: unknown;
        pending: { needsAdminUser: boolean; missingEnv: string[] } | null;
      };
      expect(pendingBody.provider).toBeNull();
      expect(pendingBody.pending?.needsAdminUser).toBe(true);
      expect(pendingBody.pending?.missingEnv).toEqual([]);

      hasAdmin = true;
      const liveRes = await fetch(
        `http://127.0.0.1:${srv.port}/admin/auth/status`,
      );
      const liveBody = (await liveRes.json()) as {
        provider: { pluginName: string } | null;
        pending: unknown;
      };
      expect(liveBody.provider?.pluginName).toBe(
        "magic-link",
      );
      expect(liveBody.pending).toBeNull();
    } finally {
      await srv.close();
    }
  });

  it("GET /admin/auth/status reports missing env keys for a pending provider", async () => {
    const srv = await startServer(
      createAdminHandler({
        auth: {
          authSignInReady: () => false,
          getAuthEnvGaps: () => ["MAGIC_LINK_FROM"],
          getAuthConfiguredEnvKeys: () => ["MAGIC_LINK_SIGNING_SECRET"],
          hasProvisionedAdmin: async () => true,
          getAuthProvider: () => ({
            pluginName: "magic-link",
            providerLabel: "Email link",
            provisioning: "manual",
            loginHintRequired: true,
          }),
          beginAuth: async () => ({ authorizationUrl: "https://x" }),
          completeAuth: async () => ({
            sub: "u",
            email: "u@e.com",
            groups: [],
          }),
        },
      }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/auth/status`);
      const body = (await res.json()) as {
        provider: unknown;
        pending: { missingEnv: string[]; configuredEnv: string[] } | null;
      };
      expect(body.provider).toBeNull();
      expect(body.pending?.missingEnv).toEqual(["MAGIC_LINK_FROM"]);
      expect(body.pending?.configuredEnv).toEqual(["MAGIC_LINK_SIGNING_SECRET"]);
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/auth/secrets writes and deletes only declared keys", async () => {
    const writes: Array<[string, string | null]> = [];
    const srv = await startServer(
      createAdminHandler({
        auth: {
          authSignInReady: () => true,
          getAuthDeclaredEnvKeys: () => ["MAGIC_LINK_FROM", "RESEND_API_KEY"],
          setAuthSecret: async (key, value) => {
            writes.push([key, value]);
          },
          getAuthProvider: () => ({
            pluginName: "magic-link",
            providerLabel: "Email link",
            provisioning: "manual",
            loginHintRequired: true,
          }),
          beginAuth: async () => ({ authorizationUrl: "https://x" }),
          completeAuth: async () => ({
            sub: "u",
            email: "u@e.com",
            groups: [],
          }),
        },
      }),
    );
    try {
      const ok = await fetch(`http://127.0.0.1:${srv.port}/admin/auth/secrets`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          values: { MAGIC_LINK_FROM: "a <b@c.co>", RESEND_API_KEY: null },
        }),
      });
      expect(ok.status).toBe(200);
      expect(writes).toEqual([
        ["MAGIC_LINK_FROM", "a <b@c.co>"],
        ["RESEND_API_KEY", null],
      ]);

      const undeclared = await fetch(
        `http://127.0.0.1:${srv.port}/admin/auth/secrets`,
        {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ values: { SNEAKY_KEY: "x" } }),
        },
      );
      expect(undeclared.status).toBe(400);
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/auth/provider chooses or clears the sign-in provider", async () => {
    const chosen: Array<string | null> = [];
    const srv = await startServer(
      createAdminHandler({
        auth: {
          authSignInReady: () => false,
          getAuthProvider: () => null,
          selectProvider: async (provider) => {
            chosen.push(provider);
          },
          beginAuth: async () => ({ authorizationUrl: "https://x" }),
          completeAuth: async () => ({ sub: "u", email: "u@e.com", groups: [] }),
        },
      }),
    );
    const post = (provider: unknown) =>
      fetch(`http://127.0.0.1:${srv.port}/admin/auth/provider`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ provider }),
      });
    try {
      expect((await post("magic-link")).status).toBe(200);
      expect((await post(null)).status).toBe(200);
      expect((await post("@open-neko/plugin-scalekit")).status).toBe(400);
      expect(chosen).toEqual(["magic-link", null]);
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/auth/begin proxies to the auth surface", async () => {
    const calls: Array<{
      redirectUri: string;
      state: string;
      loginHint?: string | null;
    }> = [];
    const srv = await startServer(
      createAdminHandler({
        auth: {
          getAuthProvider: () => null,
          beginAuth: async (p) => {
            calls.push(p);
            return { authorizationUrl: `https://idp/oauth?state=${p.state}` };
          },
          completeAuth: async () => ({
            sub: "u",
            email: "u@e.com",
            groups: [],
          }),
        },
      }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/auth/begin`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          redirectUri: "https://app.example.com/cb",
          state: "csrf-1",
          loginHint: "amit@example.com",
        }),
      });
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({
        authorizationUrl: "https://idp/oauth?state=csrf-1",
      });
      expect(calls).toEqual([
        {
          redirectUri: "https://app.example.com/cb",
          state: "csrf-1",
          loginHint: "amit@example.com",
        },
      ]);
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/auth/begin returns 400 when required fields are missing", async () => {
    const srv = await startServer(
      createAdminHandler({
        auth: {
          getAuthProvider: () => null,
          beginAuth: async () => ({ authorizationUrl: "x" }),
          completeAuth: async () => ({
            sub: "u",
            email: "u@e.com",
            groups: [],
          }),
        },
      }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/auth/begin`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ redirectUri: "https://x" }),
      });
      expect(res.status).toBe(400);
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/auth/complete returns the identity", async () => {
    const srv = await startServer(
      createAdminHandler({
        auth: {
          getAuthProvider: () => null,
          beginAuth: async () => ({ authorizationUrl: "x" }),
          completeAuth: async ({ code }) => ({
            sub: `sub-${code}`,
            email: "amit@example.com",
            name: "Amit",
            orgId: "org-1",
            groups: ["everyone"],
          }),
        },
      }),
    );
    try {
      const res = await fetch(
        `http://127.0.0.1:${srv.port}/admin/auth/complete`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            code: "auth-code",
            redirectUri: "https://app.example.com/cb",
            state: "csrf-1",
          }),
        },
      );
      expect(res.status).toBe(200);
      const body = (await res.json()) as { identity: { sub: string } };
      expect(body.identity.sub).toBe("sub-auth-code");
    } finally {
      await srv.close();
    }
  });

  it("auth endpoints return 503 when no auth surface is wired", async () => {
    const srv = await startServer(createAdminHandler());
    try {
      const begin = await fetch(`http://127.0.0.1:${srv.port}/admin/auth/begin`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          redirectUri: "https://x",
          state: "x",
        }),
      });
      expect(begin.status).toBe(503);
    } finally {
      await srv.close();
    }
  });

  it("propagates plugin errors as 500", async () => {
    const srv = await startServer(
      createAdminHandler({
        auth: {
          getAuthProvider: () => null,
          beginAuth: async () => {
            throw new Error("SCALEKIT_CLIENT_SECRET not set");
          },
          completeAuth: async () => ({
            sub: "u",
            email: "u@e.com",
            groups: [],
          }),
        },
      }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/auth/begin`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          redirectUri: "https://x",
          state: "x",
        }),
      });
      expect(res.status).toBe(500);
      expect(((await res.json()) as { error: string }).error).toMatch(
        /SCALEKIT_CLIENT_SECRET/,
      );
    } finally {
      await srv.close();
    }
  });

  // ─── /admin/connect/* (per-operator OAuth) ─────────────────────────

  const sampleCredential = () => ({
    tokens: { access_token: "at-1", refresh_token: "rt-1" },
    scopes: ["gmail.send"],
    providerLabel: "Google Workspace",
    connectedAt: "2026-05-21T10:00:00Z",
  });

  function fakeConnect(overrides: Partial<{
    getConnectProviders: () => ReturnType<NonNullable<Parameters<typeof createAdminHandler>[0]["connect"]>["getConnectProviders"]>;
    getOperatorConnectStatus: (operatorId: string) => ReturnType<NonNullable<Parameters<typeof createAdminHandler>[0]["connect"]>["getOperatorConnectStatus"]>;
    beginConnect: NonNullable<Parameters<typeof createAdminHandler>[0]["connect"]>["beginConnect"];
    completeConnect: NonNullable<Parameters<typeof createAdminHandler>[0]["connect"]>["completeConnect"];
    refreshConnect: NonNullable<Parameters<typeof createAdminHandler>[0]["connect"]>["refreshConnect"];
    disconnect: NonNullable<Parameters<typeof createAdminHandler>[0]["connect"]>["disconnect"];
  }> = {}) {
    return {
      getConnectProviders: overrides.getConnectProviders ?? (() => []),
      getOperatorConnectStatus: overrides.getOperatorConnectStatus ?? (() => []),
      beginConnect:
        overrides.beginConnect ??
        (async () => ({ authorizationUrl: "https://x" })),
      completeConnect:
        overrides.completeConnect ?? (async () => sampleCredential()),
      refreshConnect:
        overrides.refreshConnect ?? (async () => sampleCredential()),
      disconnect: overrides.disconnect ?? (async () => true),
    };
  }

  it("GET /admin/connect/providers returns the registry's list", async () => {
    const srv = await startServer(
      createAdminHandler({
        connect: fakeConnect({
          getConnectProviders: () => [
            {
              pluginId: "open-neko-connector-google-workspace",
              pluginName: "@open-neko/connector-google-workspace",
              providerLabel: "Google Workspace",
              scopes: ["gmail.readonly"],
            },
          ],
        }),
      }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/connect/providers`);
      expect(res.status).toBe(200);
      const body = (await res.json()) as { providers: Array<{ providerLabel: string }> };
      expect(body.providers[0]?.providerLabel).toBe("Google Workspace");
    } finally {
      await srv.close();
    }
  });

  it("GET /admin/connect/providers returns [] when connect surface absent", async () => {
    const srv = await startServer(createAdminHandler());
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/connect/providers`);
      const body = (await res.json()) as { providers: unknown[] };
      expect(body.providers).toEqual([]);
    } finally {
      await srv.close();
    }
  });

  it("GET /admin/connect/status/:operatorId returns per-operator status", async () => {
    const srv = await startServer(
      createAdminHandler({
        connect: fakeConnect({
          getOperatorConnectStatus: (operatorId) => [
            {
              pluginName: `${operatorId}-plugin`,
              connectedAt: "2026-05-21T10:00:00Z",
            },
          ],
        }),
      }),
    );
    try {
      const res = await fetch(
        `http://127.0.0.1:${srv.port}/admin/connect/status/op-1`,
      );
      expect(res.status).toBe(200);
      const body = (await res.json()) as {
        connected: Array<{ pluginName: string }>;
      };
      expect(body.connected[0]?.pluginName).toBe("op-1-plugin");
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/connect/begin proxies to the plugin", async () => {
    let captured: { plugin?: string; params?: unknown } = {};
    const srv = await startServer(
      createAdminHandler({
        connect: fakeConnect({
          beginConnect: async (plugin, params) => {
            captured = { plugin, params };
            return { authorizationUrl: "https://provider/auth?x" };
          },
        }),
      }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/connect/begin`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          pluginName: "@x/y",
          params: {
            operatorId: "op-1",
            redirectUri: "https://app/cb",
            state: "csrf",
            scopes: ["s"],
          },
        }),
      });
      expect(res.status).toBe(200);
      const body = (await res.json()) as { authorizationUrl: string };
      expect(body.authorizationUrl).toBe("https://provider/auth?x");
      expect(captured.plugin).toBe("@x/y");
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/connect/begin returns 400 on missing pluginName", async () => {
    const srv = await startServer(
      createAdminHandler({ connect: fakeConnect() }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/connect/begin`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ params: {} }),
      });
      expect(res.status).toBe(400);
      expect(((await res.json()) as { error: string }).error).toMatch(/pluginName/);
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/connect/begin returns 503 when connect surface disabled", async () => {
    const srv = await startServer(createAdminHandler());
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/connect/begin`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ pluginName: "@x/y", params: {} }),
      });
      expect(res.status).toBe(503);
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/connect/complete returns the credential", async () => {
    const srv = await startServer(
      createAdminHandler({ connect: fakeConnect() }),
    );
    try {
      const res = await fetch(
        `http://127.0.0.1:${srv.port}/admin/connect/complete`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            pluginName: "@x/y",
            params: {
              operatorId: "op-1",
              code: "auth-code",
              redirectUri: "https://app/cb",
              state: "csrf",
              scopes: [],
            },
          }),
        },
      );
      expect(res.status).toBe(200);
      const body = (await res.json()) as {
        credential: { tokens: Record<string, string> };
      };
      expect(body.credential.tokens.access_token).toBe("at-1");
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/connect/refresh requires operatorId + pluginName", async () => {
    const srv = await startServer(
      createAdminHandler({ connect: fakeConnect() }),
    );
    try {
      const res = await fetch(
        `http://127.0.0.1:${srv.port}/admin/connect/refresh`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ pluginName: "@x/y" }),
        },
      );
      expect(res.status).toBe(400);
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/connect/disconnect returns { removed }", async () => {
    const srv = await startServer(
      createAdminHandler({
        connect: fakeConnect({ disconnect: async () => true }),
      }),
    );
    try {
      const res = await fetch(
        `http://127.0.0.1:${srv.port}/admin/connect/disconnect`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ pluginName: "@x/y", operatorId: "op-1" }),
        },
      );
      expect(res.status).toBe(200);
      const body = (await res.json()) as { removed: boolean };
      expect(body.removed).toBe(true);
    } finally {
      await srv.close();
    }
  });

  it("GET /admin/install-policy returns the configured policy", async () => {
    const srv = await startServer(
      createAdminHandler({
        installPolicy: {
          getInstallPolicy: async () => ({ allowGitUrlInstalls: true }),
        },
      }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/install-policy`);
      expect(res.status).toBe(200);
      const body = (await res.json()) as {
        policy: { allowGitUrlInstalls: boolean };
        source: string;
      };
      expect(body.source).toBe("org");
      expect(body.policy).toEqual({ allowGitUrlInstalls: true });
    } finally {
      await srv.close();
    }
  });

  it("GET /admin/install-policy returns default policy when surface absent", async () => {
    const srv = await startServer(createAdminHandler());
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/install-policy`);
      expect(res.status).toBe(200);
      const body = (await res.json()) as {
        policy: { allowGitUrlInstalls: boolean };
        source: string;
      };
      expect(body.source).toBe("default");
      expect(body.policy).toEqual({ allowGitUrlInstalls: false });
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/connect/begin propagates provider errors as 500", async () => {
    const srv = await startServer(
      createAdminHandler({
        connect: fakeConnect({
          beginConnect: async () => {
            throw new Error("auth_url_build_failed");
          },
        }),
      }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/connect/begin`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ pluginName: "@x/y", params: {} }),
      });
      expect(res.status).toBe(500);
      expect(((await res.json()) as { error: string }).error).toMatch(/auth_url_build_failed/);
    } finally {
      await srv.close();
    }
  });
});

describe("worker channel routes", () => {
  function fakeChannel(
    overrides: Partial<NonNullable<Parameters<typeof createAdminHandler>[0]["channels"]>> = {},
  ): NonNullable<Parameters<typeof createAdminHandler>[0]["channels"]> {
    return {
      getChannelProviders: overrides.getChannelProviders ?? (async () => []),
      deliver: overrides.deliver ?? (async () => ({ delivered: true, ref: "1" })),
      ingestInbound: overrides.ingestInbound ?? (async () => ({ ok: true, dispatched: 0 })),
      ...(overrides.settings ? { settings: overrides.settings } : {}),
      ...(overrides.setSettings ? { setSettings: overrides.setSettings } : {}),
    };
  }

  it("GET /admin/channels/providers returns the turned-on channels", async () => {
    const srv = await startServer(
      createAdminHandler({
        channels: fakeChannel({
          getChannelProviders: async () => [
            {
              id: "telegram",
              providerLabel: "Telegram",
              directions: ["outbound", "inbound"],
              ingress: "webhook",
            },
          ],
        }),
      }),
    );
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/channels/providers`);
      expect(res.status).toBe(200);
      const body = (await res.json()) as { providers: Array<{ providerLabel: string }> };
      expect(body.providers[0]?.providerLabel).toBe("Telegram");
    } finally {
      await srv.close();
    }
  });

  it("GET /admin/channels/providers returns [] when the channel surface is absent", async () => {
    const srv = await startServer(createAdminHandler());
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/channels/providers`);
      expect(((await res.json()) as { providers: unknown[] }).providers).toEqual([]);
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/channels/:p/deliver proxies recipient + events to the surface", async () => {
    let captured: { plugin?: string; recipient?: unknown; events?: unknown[] } = {};
    const srv = await startServer(
      createAdminHandler({
        channels: fakeChannel({
          deliver: async (plugin, recipient, events) => {
            captured = { plugin, recipient, events };
            return { delivered: true, ref: "278" };
          },
        }),
      }),
    );
    try {
      const res = await fetch(
        `http://127.0.0.1:${srv.port}/admin/channels/telegram/deliver`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ recipient: { kind: "telegram", chatId: 5 }, events: [{ kind: "inform" }] }),
        },
      );
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({ delivered: true, ref: "278" });
      expect(captured.plugin).toBe("telegram");
      expect((captured.recipient as { chatId: number }).chatId).toBe(5);
      expect(captured.events).toHaveLength(1);
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/channels/:p/deliver returns 400 on missing events", async () => {
    const srv = await startServer(createAdminHandler({ channels: fakeChannel() }));
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/channels/x/deliver`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ recipient: { kind: "x" } }),
      });
      expect(res.status).toBe(400);
    } finally {
      await srv.close();
    }
  });

  it("POST /admin/channels/:p/deliver returns 503 when surface absent", async () => {
    const srv = await startServer(createAdminHandler());
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/admin/channels/x/deliver`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ recipient: { kind: "x" }, events: [] }),
      });
      expect(res.status).toBe(503);
    } finally {
      await srv.close();
    }
  });

  it("POST /channels/:p/inbound runs the surface's verify->parse->dispatch", async () => {
    let captured: { plugin?: string; headers?: Record<string, string>; body?: string } = {};
    const srv = await startServer(
      createAdminHandler({
        channels: fakeChannel({
          ingestInbound: async (plugin, headers, body) => {
            captured = { plugin, headers, body };
            return { ok: true, dispatched: 1 };
          },
        }),
      }),
    );
    try {
      const res = await fetch(
        `http://127.0.0.1:${srv.port}/channels/telegram/inbound`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Telegram-Bot-Api-Secret-Token": "s3cret",
          },
          body: JSON.stringify({ callback_query: { data: "approve:ar-1" } }),
        },
      );
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({ ok: true, dispatched: 1 });
      expect(captured.plugin).toBe("telegram");
      expect(captured.headers?.["x-telegram-bot-api-secret-token"]).toBe("s3cret");
      expect(captured.body).toContain("approve:ar-1");
    } finally {
      await srv.close();
    }
  });

  it("GET and POST /admin/channels/settings read and write channel settings", async () => {
    let written: { channel?: string; values?: Record<string, string | null> } = {};
    const srv = await startServer(
      createAdminHandler({
        channels: fakeChannel({
          settings: async () => [{ id: "slack", enabled: true, missing: ["SLACK_BOT_TOKEN"] }],
          setSettings: async (channel, values) => {
            written = { channel, values };
          },
        }),
      }),
    );
    try {
      const read = await fetch(`http://127.0.0.1:${srv.port}/admin/channels/settings`);
      expect(read.status).toBe(200);
      expect(((await read.json()) as { channels: Array<{ id: string }> }).channels[0]?.id).toBe("slack");

      const save = await fetch(`http://127.0.0.1:${srv.port}/admin/channels/settings`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ channel: "slack", values: { SLACK_BOT_TOKEN: "xoxb-1", SLACK_APP_TOKEN: null } }),
      });
      expect(save.status).toBe(200);
      expect(written).toEqual({ channel: "slack", values: { SLACK_BOT_TOKEN: "xoxb-1", SLACK_APP_TOKEN: null } });

      const bad = await fetch(`http://127.0.0.1:${srv.port}/admin/channels/settings`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ channel: "@open-neko/plugin-slack", values: { SLACK_BOT_TOKEN: "x" } }),
      });
      expect(bad.status).toBe(400);
    } finally {
      await srv.close();
    }
  });

  it("POST /channels/:p/inbound returns 503 when surface absent", async () => {
    const srv = await startServer(createAdminHandler());
    try {
      const res = await fetch(`http://127.0.0.1:${srv.port}/channels/x/inbound`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      });
      expect(res.status).toBe(503);
    } finally {
      await srv.close();
    }
  });

  it("GET /admin/sso/setup/status returns the setup state", async () => {
    const srv = await startServer(
      createAdminHandler({
        ssoSetup: {
          getStatus: async () => ({
            status: "awaiting_portal",
            portalLink: "https://scalekit.test/portal",
            portalLinkExpiresAt: null,
            provider: "OKTA",
            connection: { status: "in_progress", provider: "OKTA", enabled: false },
            lastError: null,
            setupCompletedAt: null,
            environmentId: "env_1",
            organizationId: "org_1",
            environmentTier: "dev",
            signInConfigured: false,
          }),
          setScalekitIds: async () => {},
          setSignInCredentials: async () => {},
          listEnvironments: async () => [
            { id: "env_1", name: "Dev", tier: "DEV", domain: "acme.scalekit.dev" },
          ],
          listOrganizations: async () => [{ id: "org_1", name: "Acme" }],
          getEnvironmentCredentials: async () => ({
            environmentUrl: "https://acme.scalekit.dev",
            clientId: "skc_1",
          }),
          generatePortalLink: async () => ({
            portalLink: "https://scalekit.test/portal",
            expiresAt: null,
          }),
          checkConnection: async () => ({
            status: "connected",
            connection: { status: "connected", provider: "OKTA", enabled: true },
            lastError: null,
            setupCompletedAt: "2026-08-13T12:00:00Z",
          }),
        },
      }),
    );
    try {
      const status = await fetch(
        `http://127.0.0.1:${srv.port}/admin/sso/setup/status?orgId=org-1`,
      );
      expect(status.status).toBe(200);
      const body = (await status.json()) as { status: string; environmentTier: string };
      expect(body.status).toBe("awaiting_portal");
      expect(body.environmentTier).toBe("dev");

      const envs = await fetch(
        `http://127.0.0.1:${srv.port}/admin/sso/setup/environments?orgId=org-1`,
      );
      expect(envs.status).toBe(200);
      const envBody = (await envs.json()) as {
        environments: Array<{ tier: string }>;
      };
      expect(envBody.environments).toHaveLength(1);

      const check = await fetch(`http://127.0.0.1:${srv.port}/admin/sso/setup/check`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ orgId: "org-1" }),
      });
      expect(check.status).toBe(200);
      const checkBody = (await check.json()) as { status: string };
      expect(checkBody.status).toBe("connected");
    } finally {
      await srv.close();
    }
  });

  it("sso setup endpoints return 503 when surface absent", async () => {
    const srv = await startServer(createAdminHandler());
    try {
      const res = await fetch(
        `http://127.0.0.1:${srv.port}/admin/sso/setup/status?orgId=org-1`,
      );
      expect(res.status).toBe(503);
    } finally {
      await srv.close();
    }
  });
});
