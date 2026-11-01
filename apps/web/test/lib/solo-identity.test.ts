import { afterEach, beforeEach, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({ rows: [] as Record<string, unknown>[][], updates: [] as unknown[], inserts: [] as unknown[], token: undefined as string | undefined, reconcile: vi.fn(async (input: unknown) => void input) }));
vi.mock("next/headers", () => ({ cookies: async () => ({ get: () => state.token ? { value: state.token } : undefined }) }));
vi.mock("@/lib/db", () => ({ getOrgId: async () => "org" }));
vi.mock("@neko/llm/work", () => ({ upsertOperatorProfile: vi.fn() }));
vi.mock("@neko/db", async (original) => {
  const actual = await original<Record<string, unknown>>();
  const runner = {
    select: () => ({ from: () => ({ where: () => ({ limit: async () => state.rows.shift() ?? [] }) }) }),
    execute: async () => {},
    update: () => ({ set: (value: unknown) => ({ where: async () => { state.updates.push(value); } }) }),
    insert: () => ({ values: async (value: unknown) => { state.inserts.push(value); } }),
    delete: () => ({ where: async () => {} }),
  };
  return { ...actual, reconcileSignInGroups: state.reconcile, getOrCreateSoloAdmin: vi.fn(async () => state.rows.shift()?.[0] ?? null), db: () => ({ ...runner, transaction: async (fn: (tx: typeof runner) => unknown) => fn(runner) }) };
});
import { _resetAuthProviderCache, encodeSession, getCurrentUser, upsertUserFromIdentity } from "@/lib/auth";

const owner = { id: "usr_owner", email: "owner@example.com", name: "Owner", role: "admin", sub: null, disabledAt: null };
function provider(live: boolean) {
  _resetAuthProviderCache();
  vi.stubGlobal("fetch", vi.fn(async () => Response.json({ provider: live ? { pluginName: "oidc" } : null })));
}
beforeEach(() => { state.rows = []; state.updates = []; state.inserts = []; state.token = undefined; provider(false); });
afterEach(() => { vi.unstubAllGlobals(); _resetAuthProviderCache(); });

it("uses the persisted solo owner without requiring a cookie or redirect", async () => {
  state.rows = [[owner], [owner]];
  expect(await getCurrentUser()).toEqual({ id: owner.id, email: owner.email, name: owner.name });
  expect((await getCurrentUser())?.id).toBe(owner.id);
});

it("links SSO to the solo account without creating a second identity, and requires a session once live", async () => {
  provider(true);
  expect(await getCurrentUser()).toBeNull();
  state.rows = [[], [owner]];
  const linked = await upsertUserFromIdentity({ sub: "idp-owner", email: "Owner@Example.com", name: "Owner" });
  expect(linked.id).toBe(owner.id);
  expect(state.updates[0]).toMatchObject({ sub: "idp-owner" });
  expect(state.updates[0]).not.toHaveProperty("role");
  expect(state.inserts).toEqual([]);
  expect(state.reconcile).toHaveBeenCalledWith({ orgId: "org", userId: owner.id, provider: "oidc", tenantId: "org", groups: [] });
  state.token = encodeSession({ userId: owner.id, email: owner.email, name: owner.name, expiresAt: Math.floor(Date.now() / 1000) + 60 });
  state.rows = [[{ id: owner.id, email: owner.email, name: owner.name }]];
  expect((await getCurrentUser())?.id).toBe(owner.id);
});
