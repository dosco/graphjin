/**
 * The auth gate must fail CLOSED while the worker is unreachable on any
 * install where SSO has been seen live — a worker boot or restart window
 * previously dropped the gate entirely and admitted unauthenticated
 * requests as the solo-operator admin.
 */

import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  _resetProviderCache,
  _setProviderStatusRequestForTest,
  isSignInGateLive,
} from "../src/proxy";
import {
  readAuthGateMarker,
  resetAuthGateCaches,
  writeAuthGateMarker,
} from "../src/lib/auth-gate-marker";

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
}

const failing = () => Promise.reject(new Error("connect ECONNREFUSED"));

beforeEach(() => {
  process.env.XDG_CONFIG_HOME = mkdtempSync(join(tmpdir(), "auth-gate-"));
  resetAuthGateCaches();
});

afterEach(() => {
  delete process.env.XDG_CONFIG_HOME;
  _setProviderStatusRequestForTest(null);
  resetAuthGateCaches();
});

describe("auth gate while the worker is unreachable", () => {
  it("stays open when SSO was never seen live", async () => {
    _setProviderStatusRequestForTest(failing);
    expect(await isSignInGateLive()).toBe(false);
  });

  it("fails closed once a live answer reported a provider", async () => {
    _setProviderStatusRequestForTest(() =>
      Promise.resolve(jsonResponse({ provider: { pluginName: "scalekit" } })),
    );
    expect(await isSignInGateLive()).toBe(true);
    expect(readAuthGateMarker()?.provider?.pluginName).toBe("scalekit");

    // Worker goes away (restart window). The gate must hold.
    _setProviderStatusRequestForTest(failing);
    resetAuthGateCaches();
    expect(await isSignInGateLive()).toBe(true);
  });

  it("re-opens after a live answer reports SSO removed", async () => {
    writeAuthGateMarker({ provider: { pluginName: "scalekit" } });
    _setProviderStatusRequestForTest(() =>
      Promise.resolve(jsonResponse({ provider: null })),
    );
    expect(await isSignInGateLive()).toBe(false);

    _setProviderStatusRequestForTest(failing);
    resetAuthGateCaches();
    expect(await isSignInGateLive()).toBe(false);
  });

  it("marker survives a fresh process (persisted, not just cached)", async () => {
    writeAuthGateMarker({ provider: { pluginName: "scalekit" } });
    // Simulate a fresh process: drop every in-memory copy, keep the file.
    resetAuthGateCaches();
    _setProviderStatusRequestForTest(failing);
    expect(await isSignInGateLive()).toBe(true);
  });

  it("_resetProviderCache clears the proxy cache too (single invalidation)", async () => {
    _setProviderStatusRequestForTest(() =>
      Promise.resolve(jsonResponse({ provider: null })),
    );
    expect(await isSignInGateLive()).toBe(false);

    // SSO becomes live; without invalidation the 1s cache would lag.
    _setProviderStatusRequestForTest(() =>
      Promise.resolve(jsonResponse({ provider: { pluginName: "scalekit" } })),
    );
    _resetProviderCache();
    expect(await isSignInGateLive()).toBe(true);
  });
});
