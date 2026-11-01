import { beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
const mocks = vi.hoisted(() => ({ holds: vi.fn(async () => true), actor: vi.fn(), begin: vi.fn(), complete: vi.fn(), disconnect: vi.fn(), write: vi.fn(), read: vi.fn(), admin: vi.fn(), worker: vi.fn() }));
vi.mock("server-only", () => ({}));
vi.mock("@/lib/personal-pack-accounts", () => ({ personalPackActor: mocks.actor }));
vi.mock("@neko/llm/graphjin/pack-user-connections", () => ({ beginPackUserConnection: mocks.begin, completePackUserConnection: mocks.complete, disconnectPackUserConnection: mocks.disconnect }));
vi.mock("@/lib/pack-oauth", () => ({ writePackOAuthState: mocks.write, readAndClearPackOAuthState: mocks.read, packOAuthCallbackUri: () => "https://app.test/api/pack-accounts/fixture/account/callback" }));
vi.mock("@/lib/integrations", () => ({ newStateToken: () => "state", newPkceVerifier: () => "verifier", pkceChallenge: () => "challenge" }));
vi.mock("@/lib/admin-auth", () => ({ requireAdminActor: mocks.admin, isDenied: (value: unknown) => value instanceof Response }));
vi.mock("@/lib/solution-packs", () => ({ validPackId: () => true, requestPackWorker: mocks.worker }));
vi.mock("@neko/db", () => ({ getOrgId: async () => "org" }));
vi.mock("@/lib/entitlements", () => ({ holdsItem: mocks.holds }));
import { POST, DELETE } from "../src/app/api/my/pack-accounts/[packId]/[connectionKey]/route";
import { GET } from "../src/app/api/pack-accounts/[packId]/[connectionKey]/callback/route";
const params = { params: Promise.resolve({ packId: "fixture", connectionKey: "account" }) };
const actor = { userId: "alice", orgId: "org" };
beforeEach(() => { vi.clearAllMocks(); mocks.actor.mockResolvedValue(actor); mocks.begin.mockResolvedValue({ authorizationUrl: "https://provider.test" }); mocks.complete.mockResolvedValue(undefined); mocks.read.mockResolvedValue({ packId: "fixture", connectionKey: "account", personal: true, userId: "alice", orgId: "org", state: "state", codeVerifier: "verifier", returnPath: "/integrations" }); });
describe("personal account routes", () => {
  it("derives the owner from the session, ignoring a forged browser owner", async () => {
    const response = await POST(new Request("https://app.test/api/my/pack-accounts/fixture/account", { method: "POST", headers: { origin: "https://app.test" }, body: JSON.stringify({ userId: "bob", orgId: "other" }) }), params);
    expect(response.status).toBe(200);
    expect(mocks.begin).toHaveBeenCalledWith(actor, "fixture", "account", expect.objectContaining({ state: "state", codeChallenge: "challenge" }));
    expect(mocks.write).toHaveBeenCalledWith(expect.objectContaining({ ...actor, personal: true }));
    mocks.holds.mockResolvedValueOnce(false);
    const hidden = await POST(new Request("https://app.test/api/my/pack-accounts/fixture/account", { method: "POST", headers: { origin: "https://app.test" } }), params);
    expect(hidden.status).toBe(404);
    expect(mocks.holds).toHaveBeenLastCalledWith("pack", "fixture");
  });
  it("rejects cross-origin disconnect and missing session", async () => {
    expect((await DELETE(new Request("https://app.test/api/my/pack-accounts/fixture/account", { method: "DELETE", headers: { origin: "https://evil.test" } }), params)).status).toBe(403);
    expect(mocks.disconnect).not.toHaveBeenCalled();
    mocks.actor.mockRejectedValue(new Error("Sign in"));
    expect((await POST(new Request("https://app.test", { method: "POST" }), params)).status).toBe(400);
    expect(mocks.begin).not.toHaveBeenCalled();
  });
  it("rejects callback session switching without exchanging the code", async () => {
    mocks.actor.mockResolvedValue({ ...actor, userId: "bob" });
    const response = await GET(new NextRequest("https://app.test/api/pack-accounts/fixture/account/callback?state=state&code=code"), params);
    expect(response.headers.get("location")).toContain("error=");
    expect(mocks.complete).not.toHaveBeenCalled();
  });
  it("completes only matching personal state and never uses the admin flow", async () => {
    const response = await GET(new NextRequest("https://app.test/api/pack-accounts/fixture/account/callback?state=state&code=code"), params);
    expect(response.headers.get("location")).toBe("https://app.test/integrations?connected=account");
    expect(mocks.complete).toHaveBeenCalledWith(actor, "fixture", "account", expect.objectContaining({ state: "state", code: "code", codeVerifier: "verifier" }));
    expect(mocks.worker).not.toHaveBeenCalled(); expect(mocks.admin).not.toHaveBeenCalled();
  });
});
