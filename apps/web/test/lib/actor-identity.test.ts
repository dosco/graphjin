import { beforeEach, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({ user: null as { id: string } | null, provider: false, rows: [] as unknown[] }));
vi.mock("@/lib/auth", () => ({ SESSION_COOKIE_NAME: "openneko_session", getCurrentUser: async () => state.user, getAuthProvider: async () => state.provider ? {} : null }));
vi.mock("@neko/db", () => ({ app_user: {}, eq: vi.fn(), getOrgId: async () => "org", resolveUserGroups: async () => ({ administrator: (state.rows[0] as { role?: string } | undefined)?.role === "admin", groupIds: [], slugs: [], ssoGroupIds: [] }), db: () => ({ select: () => ({ from: () => ({ where: () => ({ limit: async () => state.rows }) }) }) }) }));
import { getCurrentActor } from "@/lib/actor";
import { GET as session } from "@/app/api/auth/session/route";
beforeEach(() => { state.user = null; state.provider = false; state.rows = []; });
it("uses the persisted owner immediately and grants no anonymous admin fallback", async () => {
  expect(await getCurrentActor()).toEqual({ userId: null, role: "member" });
  state.user = { id: "usr_owner" }; state.rows = [{ role: "admin" }];
  expect(await getCurrentActor()).toEqual({ userId: "usr_owner", role: "admin" });
});

it("reports solo identity separately from sign-in state for the navigation", async () => {
  state.user = { id: "usr_owner" }; state.rows = [{ role: "admin" }];
  const request = { cookies: { get: () => undefined } } as never;
  expect(await (await session(request)).json()).toMatchObject({ user: { id: "usr_owner" }, authEnabled: false, signedIn: false });
  state.provider = true;
  expect(await (await session(request)).json()).toMatchObject({ user: { id: "usr_owner" }, authEnabled: true });
  const withCookie = { cookies: { get: () => ({ value: "usr_owner.1.sig" }) } } as never;
  expect(await (await session(withCookie)).json()).toMatchObject({ signedIn: true });
});
