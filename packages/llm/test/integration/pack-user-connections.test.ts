import { randomUUID } from "node:crypto";
import { readFile } from "node:fs/promises";
import { beforeAll, afterAll, beforeEach, describe, expect, it, vi } from "vitest";
import pg from "pg";
import { buildPoolConfig } from "@neko/db";

const state = vi.hoisted(() => ({ pool: null as pg.Pool | null, refreshes: 0 }));
vi.mock("@neko/db", async original => ({ ...await original<typeof import("@neko/db")>(), pool: () => state.pool! }));
vi.mock("@neko/secrets", () => ({ readSecretsStore: async () => ({ "pack.fixture": { OAUTH_ACCOUNT_CLIENT_ID: "client", CLIENT_SECRET: "secret" } }) }));
vi.mock("@neko/packs/oauth-client", async original => ({
  ...await original<typeof import("@neko/packs/oauth-client")>(),
  exchangePackOAuthCode: vi.fn(async ({ code }: { code: string }) => ({ accessToken: `access-${code}`, refreshToken: `refresh-${code}`, scopes: ["read"], expiresAt: new Date(Date.now() + 3_600_000).toISOString() })),
  fetchPackOAuthAccount: vi.fn(async ({ accessToken }: { accessToken: string }) => ({ id: accessToken, label: `${accessToken}@example.test` })),
  refreshPackOAuthToken: vi.fn(async ({ refreshToken }: { refreshToken: string }) => { state.refreshes++; return { accessToken: `renewed-${refreshToken}`, refreshToken, scopes: ["read"], expiresAt: new Date(Date.now() + 3_600_000).toISOString() }; }),
}));
import { maybeEncryptSecret } from "@neko/secret-crypt";
import { packConnectionHeader, exchangePackOAuthCode } from "@neko/packs/oauth-client";
import { beginPackUserConnection, completePackUserConnection, disconnectPackUserConnection, listPackUserConnections, packConnectionBindings, packUserConnectionHeaders } from "../../src/graphjin/pack-user-connections";
import { mintGraphjinToken } from "../../src/graphjin/token";
import { askGraphjinAgent } from "../../src/graphjin/agent";
import { graphjinQuery } from "../../src/graphjin/client";
import { callGraphjinMcpTool } from "../../src/graphjin/mcp-client";

const config = buildPoolConfig();
const admin = new pg.Pool(config);
let reachable = true;
try { await admin.query("select 1"); } catch { reachable = false; }
const schema = `personal_test_${randomUUID().replaceAll("-", "")}`;
const installId = randomUUID(), sourceId = randomUUID();
const url = "http://graphjin.test/api/v1/graphql", mcp = "http://graphjin.test/api/v1/mcp";
const alice = { orgId: "org", userId: "alice" }, bob = { orgId: "org", userId: "bob" };
const connection = { key: "account", providerLabel: "Fixture account", scope: "user", authorizationUrl: "https://provider.test/authorize", tokenUrl: "https://provider.test/token", userInfoUrl: "https://provider.test/user", clientIdInput: "client_id", clientSecret: "client_secret", accessToken: "access", refreshToken: "refresh", scopes: ["read"], accountIdField: "id", accountLabelField: "email", experience: { description: "Pack-authored description" } };
const beginInput = { state: "state", codeChallenge: "challenge", redirectUri: "https://app.test/callback" };
const tokenHeaders = (userId: string | null) => ({ authorization: `Bearer ${mintGraphjinToken({ orgId: "org", userId, role: "member" })}` });
const header = packConnectionHeader("fixture", "account");
async function connect(actor = alice, code = "alice") {
  await beginPackUserConnection(actor, "fixture", "account", beginInput);
  await completePackUserConnection(actor, "fixture", "account", { ...beginInput, code, codeVerifier: "verifier" });
}

describe.skipIf(!reachable)("personal pack credentials against PostgreSQL", () => {
  beforeAll(async () => {
    await admin.query(`create schema ${schema}`);
    state.pool = new pg.Pool({ ...config, options: `-c search_path=${schema},public` });
    await state.pool.query(`create table organization(id text primary key); create table app_user(id text primary key,org_id text,disabled_at timestamptz); create table pack_install(id uuid primary key,org_id text,pack_id text,status text,config jsonb); create table data_source(id uuid primary key,org_id text,graphql_url text,mcp_url text,enabled boolean,auth_mode text);`);
    const migration = await readFile(new URL("../../../../db/migrations/0072_pack_user_connections.sql", import.meta.url), "utf8");
    await state.pool.query(migration);
    await state.pool.query(migration); // Upgrade retries are safe.
    await state.pool.query("insert into organization values('org'),('other'); insert into app_user values('alice','org',null),('bob','org',null),('outsider','other',null)");
    await state.pool.query("insert into data_source values($1,'org',$2,$3,true,'jwt')", [sourceId, url, mcp]);
    await state.pool.query("insert into pack_install values($1,'org','fixture','installed',$2)", [installId, { _runtime: { source: { id: sourceId, graphqlUrl: url, authMode: "jwt" } }, _userOAuth: [connection] }]);
  });
  beforeEach(async () => {
    vi.clearAllMocks(); state.refreshes = 0;
    await state.pool!.query("delete from pack_user_connection; update app_user set disabled_at=null; update pack_install set status='installed'; update data_source set enabled=true");
  });
  afterAll(async () => { await state.pool?.end(); await admin.query(`drop schema ${schema} cascade`); await admin.end(); });

  it("isolates simultaneous users and never serializes credentials in status", async () => {
    await Promise.all([connect(alice, "alice"), connect(bob, "bob")]);
    const [a, b] = await Promise.all([packUserConnectionHeaders(url, tokenHeaders("alice")), packUserConnectionHeaders(url, tokenHeaders("bob"))]);
    expect(a[header]).toBe("access-alice"); expect(b[header]).toBe("access-bob");
    const status = await listPackUserConnections(alice);
    expect(status[0]).toMatchObject({ accountLabel: "access-alice@example.test", experience: { description: "Pack-authored description" }, connected: true });
    expect(status[0]).not.toHaveProperty("credentials");
    const row = (await state.pool!.query("select credentials from pack_user_connection where user_id='alice'")).rows[0];
    expect(row.credentials).toMatch(/^enc:v1:/); expect(row.credentials).not.toContain("access-alice");
  });
  it("rejects cross-org, disabled users and replayed callbacks", async () => {
    await expect(connect({ orgId: "other", userId: "alice" })).rejects.toThrow("active account");
    await connect();
    await expect(completePackUserConnection(alice, "fixture", "account", { ...beginInput, code: "again", codeVerifier: "verifier" })).rejects.toThrow("expired");
    expect(exchangePackOAuthCode).toHaveBeenCalledTimes(1);
    await state.pool!.query("update app_user set disabled_at=now() where id='alice'");
    await expect(packUserConnectionHeaders(url, tokenHeaders("alice"))).rejects.toThrow("active account");
  });
  it("disconnect invalidates pending callbacks and approved account revisions", async () => {
    await connect();
    const bindings = await packConnectionBindings(alice, installId);
    await beginPackUserConnection(alice, "fixture", "account", beginInput);
    await disconnectPackUserConnection(alice, "fixture", "account");
    await expect(completePackUserConnection(alice, "fixture", "account", { ...beginInput, code: "again", codeVerifier: "verifier" })).rejects.toThrow("expired");
    await connect(alice, "new");
    await expect(packUserConnectionHeaders(url, tokenHeaders("alice"), bindings)).rejects.toThrow("changed");
    expect((await packUserConnectionHeaders(url, tokenHeaders("alice")))[header]).toBe("access-new");
  });
  it("serializes refreshes and leaves no credential after a concurrent disconnect", async () => {
    await connect();
    await state.pool!.query("update pack_user_connection set credentials=$1", [maybeEncryptSecret(JSON.stringify({ accessToken: "old", refreshToken: "r", scopes: ["read"], expiresAt: "2000-01-01" }))]);
    const calls = await Promise.all(Array.from({ length: 8 }, () => packUserConnectionHeaders(url, tokenHeaders("alice"))));
    expect(calls.every(h => h[header] === "renewed-r")).toBe(true); expect(state.refreshes).toBe(1);
    await Promise.all([packUserConnectionHeaders(url, tokenHeaders("alice")), disconnectPackUserConnection(alice, "fixture", "account")]);
    expect((await packUserConnectionHeaders(url, tokenHeaders("alice")))[header]).toBeUndefined();
  });
  it("does not forward to unknown endpoints, accept supplied headers or fall back to another account", async () => {
    await connect();
    expect((await packUserConnectionHeaders("https://evil.test", tokenHeaders("alice")))[header]).toBeUndefined();
    expect((await packUserConnectionHeaders(url, { [header]: "forged" }))[header]).toBeUndefined();
    expect((await packUserConnectionHeaders(url, tokenHeaders(null)))[header]).toBeUndefined();
    expect((await packUserConnectionHeaders(url, tokenHeaders("bob")))[header]).toBeUndefined();
    const bindings = await packConnectionBindings(alice, installId);
    await expect(packUserConnectionHeaders("https://evil.test", tokenHeaders("alice"), bindings)).rejects.toThrow("no longer available");
    await expect(packUserConnectionHeaders(url, tokenHeaders(null), bindings)).rejects.toThrow("no longer available");
  });
  it("rejects expired state, missing installs and plaintext database writes", async () => {
    await beginPackUserConnection(alice, "fixture", "account", beginInput);
    await state.pool!.query("update pack_user_connection set pending_expires_at=now()-interval '1 second'");
    await expect(completePackUserConnection(alice, "fixture", "account", { ...beginInput, code: "expired", codeVerifier: "verifier" })).rejects.toThrow("expired");
    expect(exchangePackOAuthCode).not.toHaveBeenCalled();
    await expect(state.pool!.query("update pack_user_connection set credentials='plain-token'")).rejects.toThrow("check constraint");
    await state.pool!.query("update pack_install set status='removed'");
    await expect(connect()).rejects.toThrow("not installed");
    expect((await packUserConnectionHeaders(url, tokenHeaders("alice")))[header]).toBeUndefined();
  });
  it("bridges GraphQL, MCP and agent requests and refuses redirects", async () => {
    await connect();
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ data: { ok: true } }), { status: 200 }));
    await graphjinQuery({ baseUrl: url, headers: tokenHeaders("alice"), query: "{ me { id } }" });
    expect(spy.mock.calls[0]?.[1]).toMatchObject({ headers: { [header]: "access-alice" }, redirect: "error", cache: "no-store" });
    spy.mockResolvedValue(new Response(JSON.stringify({ jsonrpc: "2.0", id: 2, result: { content: [] } }), { status: 200, headers: { "content-type": "application/json" } }));
    await callGraphjinMcpTool({ baseUrl: mcp, headers: tokenHeaders("alice") }, { name: "query", arguments: {} });
    expect(spy.mock.calls[1]?.[1]).toMatchObject({ headers: { [header]: "access-alice" }, redirect: "error" });
    spy.mockResolvedValue(new Response(JSON.stringify({ status: "completed", answer: "done" }), { status: 200 }));
    await askGraphjinAgent({ baseUrl: url, token: mintGraphjinToken({ orgId: "org", userId: "alice", role: "member" }), request: { instruction: "Read my account" } });
    expect(spy.mock.calls[2]?.[1]).toMatchObject({ headers: { [header]: "access-alice" }, redirect: "error" });
    spy.mockRestore();
  });
});
if (!reachable) await admin.end();
