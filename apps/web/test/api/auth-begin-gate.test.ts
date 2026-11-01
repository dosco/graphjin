import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  getAuthProvider: vi.fn(),
  beginAuth: vi.fn(),
  writeStateCookie: vi.fn(async () => undefined),
  userRows: [] as Array<{ id: string }>,
  selectCalls: 0,
}));

vi.mock("@/lib/auth", () => ({
  getAuthProvider: mocks.getAuthProvider,
  beginAuth: mocks.beginAuth,
  newStateToken: () => "state-token-fixture",
  writeStateCookie: mocks.writeStateCookie,
  buildRedirectUri: () => "http://localhost/api/auth/callback",
}));

vi.mock("@/lib/db", () => ({
  getOrgId: async () => "org-1",
}));

vi.mock("@neko/db", () => ({
  app_user: {
    id: "id",
    org_id: "org_id",
    email: "email",
    disabled_at: "disabled_at",
  },
  db: () => ({
    select: () => {
      mocks.selectCalls += 1;
      return {
        from: () => ({
          where: () => ({
            limit: async () => mocks.userRows,
          }),
        }),
      };
    },
  }),
  and: (...args: unknown[]) => args,
  eq: (...args: unknown[]) => args,
  isNull: (...args: unknown[]) => args,
  sql: () => "sql",
}));

import { GET } from "@/app/api/auth/begin/route";

function request(query: string): Request {
  return new Request(`http://localhost/api/auth/begin${query}`);
}

describe("manual-provisioning gate on /api/auth/begin", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.userRows = [];
    mocks.selectCalls = 0;
    mocks.beginAuth.mockResolvedValue({
      authorizationUrl: "https://idp.example/authorize?x=1",
    });
  });

  it("silently redirects unknown emails to the link-sent notice without touching the plugin", async () => {
    mocks.getAuthProvider.mockResolvedValue({
      pluginName: "magic-link",
      providerLabel: "Email link",
      provisioning: "manual",
      loginHintRequired: true,
    });
    const res = await GET(request("?loginHint=stranger%40evil.com") as never);
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toContain("/signin?notice=link-sent");
    expect(mocks.beginAuth).not.toHaveBeenCalled();
    expect(mocks.writeStateCookie).not.toHaveBeenCalled();
  });

  it("starts the flow for a provisioned email and redirects to the plugin URL", async () => {
    mocks.getAuthProvider.mockResolvedValue({
      pluginName: "magic-link",
      providerLabel: "Email link",
      provisioning: "manual",
      loginHintRequired: true,
    });
    mocks.userRows = [{ id: "usr_1" }];
    const res = await GET(request("?loginHint=Person%40Company.com") as never);
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toBe("https://idp.example/authorize?x=1");
    expect(mocks.writeStateCookie).toHaveBeenCalledWith("state-token-fixture", "/");
    expect(mocks.beginAuth).toHaveBeenCalledWith({
      redirectUri: "http://localhost/api/auth/callback",
      state: "state-token-fixture",
      loginHint: "Person@Company.com",
    });
  });

  it("rejects a hint-less request when the provider requires one", async () => {
    mocks.getAuthProvider.mockResolvedValue({
      pluginName: "magic-link",
      providerLabel: "Email link",
      provisioning: "manual",
      loginHintRequired: true,
    });
    const res = await GET(request("") as never);
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toContain("/signin?error=");
    expect(mocks.beginAuth).not.toHaveBeenCalled();
  });

  it("masks a plugin send failure as the same link-sent notice", async () => {
    mocks.getAuthProvider.mockResolvedValue({
      pluginName: "magic-link",
      providerLabel: "Email link",
      provisioning: "manual",
      loginHintRequired: true,
    });
    mocks.userRows = [{ id: "usr_1" }];
    mocks.beginAuth.mockRejectedValue(new Error("smtp exploded"));
    const res = await GET(request("?loginHint=person%40company.com") as never);
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toContain("/signin?notice=link-sent");
  });

  it("leaves automatic-provisioning providers untouched (no user lookup)", async () => {
    mocks.getAuthProvider.mockResolvedValue({
      pluginName: "scalekit",
      providerLabel: "Scalekit",
      provisioning: "automatic",
      loginHintRequired: false,
    });
    const res = await GET(request("") as never);
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toBe("https://idp.example/authorize?x=1");
    expect(mocks.selectCalls).toBe(0);
  });

  it("treats a provider without the provisioning field as automatic", async () => {
    mocks.getAuthProvider.mockResolvedValue({
      pluginName: "@open-neko/plugin-legacy",
      providerLabel: "Legacy",
    });
    const res = await GET(request("") as never);
    expect(res.status).toBe(302);
    expect(res.headers.get("location")).toBe("https://idp.example/authorize?x=1");
    expect(mocks.selectCalls).toBe(0);
  });
});
