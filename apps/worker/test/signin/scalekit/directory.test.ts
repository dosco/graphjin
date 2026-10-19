import { describe, expect, it } from "vitest";
import { listScalekitDirectory, SCALEKIT_DIRECTORY } from "../../../src/signin/scalekit/provider";
import { createScalekitDirectory } from "../../../src/signin/scalekit/directory";

const ENV = "https://acme.scalekit.dev";
const ORG = "org_1";

type Call = { method: string; url: URL; body: string | null; auth: string | null };

function fakeScalekit(routes: Record<string, (url: URL) => unknown>) {
  const calls: Call[] = [];
  const fetchImpl = (async (input: string | URL, init?: RequestInit) => {
    const url = new URL(String(input));
    const method = init?.method ?? "GET";
    const headers = (init?.headers ?? {}) as Record<string, string>;
    calls.push({ method, url, body: typeof init?.body === "string" ? init.body : null, auth: headers.Authorization ?? null });
    const route = routes[`${method} ${url.pathname}`];
    if (!route) return new Response(JSON.stringify({ error: "not_found" }), { status: 404 });
    return new Response(JSON.stringify(route(url)), { status: 200 });
  }) as typeof fetch;
  return { calls, fetchImpl };
}

const token = () => ({ access_token: "m2m-token", token_type: "Bearer", expires_in: 3600 });

describe("Scalekit directory", () => {
  it("pages groups, then users, across enabled directories", async () => {
    const { calls, fetchImpl } = fakeScalekit({
      "POST /oauth/token": token,
      [`GET /api/v1/organizations/${ORG}/directories`]: () => ({
        directories: [
          { id: "dir_b", enabled: true },
          { id: "dir_off", enabled: false },
          { id: "dir_a", enabled: true },
        ],
      }),
      [`GET /api/v1/organizations/${ORG}/directories/dir_a/groups`]: (url) =>
        url.searchParams.get("page_token")
          ? { groups: [{ id: "dirgroup_2", display_name: "Sales" }], next_page_token: "" }
          : { groups: [{ id: "dirgroup_1", display_name: "Finance" }], next_page_token: "g2" },
      [`GET /api/v1/organizations/${ORG}/directories/dir_a/users`]: () => ({
        users: [
          {
            id: "diruser_1",
            email: "Ana@Acme.com",
            given_name: "Ana",
            family_name: "Diaz",
            groups: [{ id: "dirgroup_1", display_name: "Finance" }],
            user_detail: { active: true },
          },
          { id: "diruser_2", emails: ["bo@acme.com"], preferred_username: "bo", groups: [], user_detail: { active: false } },
          { id: "diruser_3", groups: [] },
        ],
        next_page_token: "",
      }),
      [`GET /api/v1/organizations/${ORG}/directories/dir_b/groups`]: () => ({ groups: [], next_page_token: "" }),
      [`GET /api/v1/organizations/${ORG}/directories/dir_b/users`]: () => ({
        users: [{ id: "diruser_9", email: "cy@acme.com", groups: [{ id: "dirgroup_2", display_name: "Sales" }] }],
      }),
    });
    const directory = createScalekitDirectory({ environmentUrl: ENV, clientId: "c", clientSecret: "s", organizationId: ORG, fetchImpl });

    const pages = [];
    let cursor: string | null = null;
    do {
      const page = await directory.list({ cursor });
      pages.push(page);
      cursor = page.nextCursor ?? null;
    } while (cursor && pages.length < 10);

    expect(pages).toHaveLength(5);
    expect(pages.every((p) => p.tenantId === ORG)).toBe(true);
    expect(pages.flatMap((p) => p.groups)).toEqual([
      { externalId: "Finance", name: "Finance" },
      { externalId: "Sales", name: "Sales" },
    ]);
    expect(pages.flatMap((p) => p.users)).toEqual([
      { externalId: "diruser_1", sub: null, email: "ana@acme.com", name: "Ana Diaz", active: true },
      { externalId: "diruser_2", sub: null, email: "bo@acme.com", name: "bo", active: false },
      { externalId: "diruser_9", sub: null, email: "cy@acme.com", name: null, active: true },
    ]);
    expect(pages.flatMap((p) => p.memberships)).toEqual([
      { userExternalId: "diruser_1", groupExternalId: "Finance" },
      { userExternalId: "diruser_9", groupExternalId: "Sales" },
    ]);

    expect(calls.filter((c) => c.url.pathname === "/oauth/token")).toHaveLength(1);
    expect(new URLSearchParams(calls[0]!.body ?? "").get("grant_type")).toBe("client_credentials");
    expect(calls.filter((c) => c.url.pathname !== "/oauth/token").every((c) => c.auth === "Bearer m2m-token")).toBe(true);
    const userCall = calls.find((c) => c.url.pathname.endsWith("dir_a/users"))!;
    expect(userCall.url.searchParams.get("include_detail")).toBe("true");
    expect(userCall.url.searchParams.get("page_size")).toBe("100");
  });

  it("returns an empty snapshot when the organization has no directory", async () => {
    const { fetchImpl } = fakeScalekit({
      "POST /oauth/token": token,
      [`GET /api/v1/organizations/${ORG}/directories`]: () => ({ directories: [] }),
    });
    const directory = createScalekitDirectory({ environmentUrl: ENV, clientId: "c", clientSecret: "s", organizationId: ORG, fetchImpl });
    expect(await directory.list({ cursor: null })).toEqual({ tenantId: ORG, users: [], groups: [], memberships: [], nextCursor: null });
  });

  it("rejects a cursor it did not issue", async () => {
    const { fetchImpl } = fakeScalekit({ "POST /oauth/token": token });
    const directory = createScalekitDirectory({ environmentUrl: ENV, clientId: "c", clientSecret: "s", organizationId: ORG, fetchImpl });
    await expect(directory.list({ cursor: "not-a-cursor" })).rejects.toThrow("directory cursor is not valid");
  });

  it("creates an organization user and refuses to deactivate one", async () => {
    const { calls, fetchImpl } = fakeScalekit({
      "POST /oauth/token": token,
      [`POST /api/v1/organizations/${ORG}/users`]: () => ({
        user: { id: "usr_1", email: "dee@acme.com", user_profile: { name: "Dee" } },
      }),
    });
    const directory = createScalekitDirectory({ environmentUrl: ENV, clientId: "c", clientSecret: "s", organizationId: ORG, fetchImpl });
    expect(await directory.apply({ change: { op: "create_user", email: "dee@acme.com", name: "Dee" } })).toEqual({
      user: { externalId: "usr_1", sub: null, email: "dee@acme.com", name: "Dee", active: true },
    });
    const create = calls.find((c) => c.method === "POST" && c.url.pathname.endsWith("/users"))!;
    expect(JSON.parse(create.body ?? "{}")).toEqual({ email: "dee@acme.com", user_profile: { name: "Dee" } });
    expect(create.url.searchParams.get("send_invitation_email")).toBe("true");
    await expect(directory.apply({ change: { op: "deactivate_user", externalId: "diruser_1" } })).rejects.toThrow("identity provider owns user status");
  });
});

describe("Scalekit directory capabilities", () => {
  it("reads users, groups and memberships, and creates but never deactivates users", () => {
    expect(SCALEKIT_DIRECTORY).toEqual({
      providerLabel: "Scalekit directory",
      read: { users: true, groups: true, memberships: true },
      write: { createUser: true, deactivateUser: false },
    });
  });

  it("needs the organization id", async () => {
    const env = { SCALEKIT_ENVIRONMENT_URL: ENV, SCALEKIT_CLIENT_ID: "c", SCALEKIT_CLIENT_SECRET: "s" };
    await expect(listScalekitDirectory({ cursor: null }, env)).rejects.toThrow("SCALEKIT_ORGANIZATION_ID not set");

    const seen: string[] = [];
    const result = await listScalekitDirectory({ cursor: null }, { ...env, SCALEKIT_ORGANIZATION_ID: ORG }, {
      createDirectory: (resolved) => {
        seen.push(resolved.organizationId);
        return { list: async () => ({ tenantId: resolved.organizationId, users: [], groups: [], memberships: [], nextCursor: null }), apply: async () => ({ user: null }) };
      },
    });
    expect(seen).toEqual([ORG]);
    expect(result.tenantId).toBe(ORG);
  });
});
