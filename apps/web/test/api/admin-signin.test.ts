import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  gate: {
    provider: null as unknown,
    pending: null as unknown,
  },
  setSignInSecrets: vi.fn(async (values: unknown) => {
    void values;
    return undefined;
  }),
  beginAuth: vi.fn(async (params: unknown) => {
    void params;
    return { authorizationUrl: "https://x" };
  }),
  selectSignInProvider: vi.fn(async (provider: unknown) => {
    void provider;
  }),
}));

vi.mock("@/lib/admin-auth", () => ({
  requireAdminActor: async () => ({ userId: "admin-1", role: "admin" }),
  isDenied: () => false,
}));

vi.mock("@/lib/auth", () => ({
  getAuthGateStatus: async () => mocks.gate,
  setSignInSecrets: mocks.setSignInSecrets,
  beginAuth: mocks.beginAuth,
  selectSignInProvider: mocks.selectSignInProvider,
  buildRedirectUri: () => "http://localhost/api/auth/callback",
}));

import { POST as postProvider } from "@/app/api/admin/signin/email-provider/route";
import { POST as postTest } from "@/app/api/admin/signin/test/route";
import { POST as postSignInProvider } from "@/app/api/admin/signin/provider/route";

function request(url: string, body: unknown): Request {
  return new Request(url, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

const providerUrl = "http://localhost/api/admin/signin/email-provider";
const testUrl = "http://localhost/api/admin/signin/test";

function magicLinkPending(configuredEnv: string[] = []) {
  return {
    pluginName: "magic-link",
    providerLabel: "Email link",
    provisioning: "manual",
    loginHintRequired: true,
    missingEnv: ["MAGIC_LINK_FROM"],
    needsAdminUser: true,
    configuredEnv,
  };
}

describe("POST /api/admin/signin/email-provider", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.gate = { provider: null, pending: magicLinkPending() };
  });

  it("writes the chosen provider key + From and clears the other providers", async () => {
    const res = await postProvider(
      request(providerUrl, {
        provider: "postmark",
        apiKey: "pm_key",
        from: "OpenNeko <signin@c.co>",
      }) as never,
    );
    expect(res.status).toBe(200);
    expect(mocks.setSignInSecrets).toHaveBeenCalledWith({
      MAGIC_LINK_FROM: "OpenNeko <signin@c.co>",
      POSTMARK_SERVER_TOKEN: "pm_key",
      RESEND_API_KEY: null,
      SENDGRID_API_KEY: null,
    });
  });

  it("keeps a stored key when none is pasted", async () => {
    mocks.gate = {
      provider: null,
      pending: magicLinkPending(["RESEND_API_KEY"]),
    };
    const res = await postProvider(
      request(providerUrl, {
        provider: "resend",
        from: "a <b@c.co>",
      }) as never,
    );
    expect(res.status).toBe(200);
    const values = mocks.setSignInSecrets.mock.calls[0][0] as Record<
      string,
      unknown
    >;
    expect(values.RESEND_API_KEY).toBeUndefined();
    expect(values.POSTMARK_SERVER_TOKEN).toBeNull();
    expect(values.SENDGRID_API_KEY).toBeNull();
  });

  it("rejects when no key is pasted and none is stored", async () => {
    const res = await postProvider(
      request(providerUrl, { provider: "resend", from: "a <b@c.co>" }) as never,
    );
    expect(res.status).toBe(400);
    expect(mocks.setSignInSecrets).not.toHaveBeenCalled();
  });

  it("rejects unknown providers and a missing From", async () => {
    expect(
      (
        await postProvider(
          request(providerUrl, {
            provider: "smtp",
            apiKey: "k",
            from: "a <b@c.co>",
          }) as never,
        )
      ).status,
    ).toBe(400);
    expect(
      (
        await postProvider(
          request(providerUrl, { provider: "resend", apiKey: "k", from: "" }) as never,
        )
      ).status,
    ).toBe(400);
  });

  it("refuses when email-link sign-in is not the chosen provider", async () => {
    mocks.gate = {
      provider: {
        pluginName: "scalekit",
        providerLabel: "Scalekit",
      },
      pending: null,
    };
    const res = await postProvider(
      request(providerUrl, {
        provider: "resend",
        apiKey: "k",
        from: "a <b@c.co>",
      }) as never,
    );
    expect(res.status).toBe(409);
  });
});

describe("POST /api/admin/signin/test", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("drives beginAuth with a synthetic delivery-test state", async () => {
    const res = await postTest(
      request(testUrl, { email: "Admin@Company.com" }) as never,
    );
    expect(res.status).toBe(200);
    expect(mocks.beginAuth).toHaveBeenCalledTimes(1);
    const params = mocks.beginAuth.mock.calls[0][0] as {
      state: string;
      loginHint: string;
    };
    expect(params.state).toMatch(/^delivery-test-/);
    expect(params.loginHint).toBe("admin@company.com");
  });

  it("rejects invalid emails without starting sign-in", async () => {
    const res = await postTest(request(testUrl, { email: "nope" }) as never);
    expect(res.status).toBe(400);
    expect(mocks.beginAuth).not.toHaveBeenCalled();
  });

  it("surfaces provider failures verbatim to the admin", async () => {
    mocks.beginAuth.mockRejectedValueOnce(
      new Error("Resend rejected the sign-in email (422): sender not verified"),
    );
    const res = await postTest(
      request(testUrl, { email: "a@b.co" }) as never,
    );
    expect(res.status).toBe(502);
    const body = (await res.json()) as { error: string };
    expect(body.error).toContain("sender not verified");
  });
});

describe("POST /api/admin/signin/provider", () => {
  beforeEach(() => vi.clearAllMocks());

  it("chooses or clears the sign-in provider and rejects anything else", async () => {
    const url = "http://localhost/api/admin/signin/provider";
    expect((await postSignInProvider(request(url, { provider: "magic-link" }) as never)).status).toBe(200);
    expect((await postSignInProvider(request(url, { provider: null }) as never)).status).toBe(200);
    expect((await postSignInProvider(request(url, { provider: "@open-neko/plugin-magic-link" }) as never)).status).toBe(400);
    expect(mocks.selectSignInProvider.mock.calls.map((call) => call[0])).toEqual(["magic-link", null]);
  });
});
