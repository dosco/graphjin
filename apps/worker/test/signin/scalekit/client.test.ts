import { describe, expect, it } from "vitest";
import {
  createScalekitClient,
  ScalekitApiError,
} from "../../../src/signin/scalekit/client";

function fetchOk(body: unknown, status = 200): typeof fetch {
  return (async () =>
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    })) as unknown as typeof fetch;
}

function fetchErr(status: number, body: unknown): typeof fetch {
  return (async () =>
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    })) as unknown as typeof fetch;
}

function recordingFetch(body: unknown, status = 200): {
  fetch: typeof fetch;
  calls: Array<{ url: string; init: RequestInit }>;
} {
  const calls: Array<{ url: string; init: RequestInit }> = [];
  const fn = (async (url: string, init: RequestInit) => {
    calls.push({ url, init });
    return new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  }) as unknown as typeof fetch;
  return { fetch: fn, calls };
}

describe("environment URL", () => {
  it("rejects non-https URLs", () => {
    expect(() =>
      createScalekitClient({
        environmentUrl: "http://foo.scalekit.com",
        clientId: "c",
        clientSecret: "s",
      }),
    ).toThrow(/https/);
  });

  it("rejects empty URL", () => {
    expect(() =>
      createScalekitClient({
        environmentUrl: "",
        clientId: "c",
        clientSecret: "s",
      }),
    ).toThrow(/empty/);
  });

  it("strips trailing slashes", () => {
    const client = createScalekitClient({
      environmentUrl: "https://foo.scalekit.com//",
      clientId: "c",
      clientSecret: "s",
    });
    const url = client.buildAuthorizationUrl({
      redirectUri: "https://app.example.com/cb",
      state: "abc",
    });
    expect(url).toMatch(/^https:\/\/foo\.scalekit\.com\/oauth\/authorize\?/);
  });
});

describe("buildAuthorizationUrl", () => {
  const client = createScalekitClient({
    environmentUrl: "https://foo.scalekit.com",
    clientId: "client-1",
    clientSecret: "secret-1",
  });

  it("sets standard OIDC params", () => {
    const url = new URL(
      client.buildAuthorizationUrl({
        redirectUri: "https://app.example.com/cb",
        state: "csrf-token",
      }),
    );
    expect(url.pathname).toBe("/oauth/authorize");
    expect(url.searchParams.get("response_type")).toBe("code");
    expect(url.searchParams.get("client_id")).toBe("client-1");
    expect(url.searchParams.get("redirect_uri")).toBe(
      "https://app.example.com/cb",
    );
    expect(url.searchParams.get("state")).toBe("csrf-token");
    expect(url.searchParams.get("scope")).toBe("openid profile email");
  });

  it("includes login_hint when supplied", () => {
    const url = new URL(
      client.buildAuthorizationUrl({
        redirectUri: "https://app.example.com/cb",
        state: "csrf-token",
        loginHint: "amit@example.com",
      }),
    );
    expect(url.searchParams.get("login_hint")).toBe("amit@example.com");
  });

  it("omits login_hint when null", () => {
    const url = new URL(
      client.buildAuthorizationUrl({
        redirectUri: "https://app.example.com/cb",
        state: "csrf-token",
        loginHint: null,
      }),
    );
    expect(url.searchParams.get("login_hint")).toBeNull();
  });

  it("rejects empty state", () => {
    expect(() =>
      client.buildAuthorizationUrl({
        redirectUri: "https://app.example.com/cb",
        state: "",
      }),
    ).toThrow(/state/);
  });

  it("rejects empty redirectUri", () => {
    expect(() =>
      client.buildAuthorizationUrl({
        redirectUri: "",
        state: "x",
      }),
    ).toThrow(/redirectUri/);
  });
});

describe("exchangeCode", () => {
  it("posts authorization_code grant with Basic auth", async () => {
    const { fetch, calls } = recordingFetch({
      access_token: "at-1",
      token_type: "Bearer",
      expires_in: 3600,
    });
    const client = createScalekitClient({
      environmentUrl: "https://foo.scalekit.com",
      clientId: "client-1",
      clientSecret: "secret-1",
      fetchImpl: fetch,
    });
    const tokens = await client.exchangeCode({
      code: "auth-code-1",
      redirectUri: "https://app.example.com/cb",
    });
    expect(tokens.access_token).toBe("at-1");
    expect(calls).toHaveLength(1);
    const call = calls[0]!;
    expect(call.url).toBe("https://foo.scalekit.com/oauth/token");
    expect(call.init.method).toBe("POST");
    const headers = call.init.headers as Record<string, string>;
    expect(headers["Content-Type"]).toBe(
      "application/x-www-form-urlencoded",
    );
    const expected =
      "Basic " +
      Buffer.from("client-1:secret-1").toString("base64");
    expect(headers["Authorization"]).toBe(expected);
    const body = String(call.init.body);
    expect(body).toContain("grant_type=authorization_code");
    expect(body).toContain("code=auth-code-1");
    expect(body).toContain(
      "redirect_uri=https%3A%2F%2Fapp.example.com%2Fcb",
    );
  });

  it("throws when access_token is missing", async () => {
    const client = createScalekitClient({
      environmentUrl: "https://foo.scalekit.com",
      clientId: "c",
      clientSecret: "s",
      fetchImpl: fetchOk({ token_type: "Bearer" }),
    });
    await expect(
      client.exchangeCode({
        code: "x",
        redirectUri: "https://app.example.com/cb",
      }),
    ).rejects.toThrow(/no access_token/);
  });

  it("surfaces provider error code on HTTP 4xx", async () => {
    const client = createScalekitClient({
      environmentUrl: "https://foo.scalekit.com",
      clientId: "c",
      clientSecret: "s",
      fetchImpl: fetchErr(400, {
        error: "invalid_grant",
        error_description: "authorization code expired",
      }),
    });
    await expect(
      client.exchangeCode({
        code: "x",
        redirectUri: "https://app.example.com/cb",
      }),
    ).rejects.toBeInstanceOf(ScalekitApiError);
  });
});

describe("fetchUserinfo", () => {
  it("sends Bearer token, parses sub", async () => {
    const { fetch, calls } = recordingFetch({
      sub: "user-42",
      email: "amit@example.com",
      name: "Amit Patel",
    });
    const client = createScalekitClient({
      environmentUrl: "https://foo.scalekit.com",
      clientId: "c",
      clientSecret: "s",
      fetchImpl: fetch,
    });
    const info = await client.fetchUserinfo("at-1");
    expect(info.sub).toBe("user-42");
    expect(calls[0]!.url).toBe("https://foo.scalekit.com/userinfo");
    expect((calls[0]!.init.headers as Record<string, string>).Authorization).toBe(
      "Bearer at-1",
    );
  });

  it("throws when sub is missing", async () => {
    const client = createScalekitClient({
      environmentUrl: "https://foo.scalekit.com",
      clientId: "c",
      clientSecret: "s",
      fetchImpl: fetchOk({ email: "x@y.com" }),
    });
    await expect(client.fetchUserinfo("at-1")).rejects.toThrow(/no sub/);
  });
});
