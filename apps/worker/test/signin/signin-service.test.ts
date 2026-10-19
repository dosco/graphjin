import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  readFullSecretsFile,
  writeFullSecretsFile,
  type ConnectorCredential,
} from "@neko/secrets";
import { CoreSecrets } from "../../src/secrets/core-secrets";
import { SignInService } from "../../src/signin/signin-service";
import type { EmailMessage } from "../../src/signin/magic-link/email";

let dir: string;
let secrets: CoreSecrets;

beforeEach(async () => {
  dir = await mkdtemp(join(tmpdir(), "signin-service-"));
  secrets = new CoreSecrets({ configDir: dir, pollMs: 0 });
  await secrets.start();
});

afterEach(async () => {
  secrets.stop();
  await rm(dir, { recursive: true, force: true });
});

function credential(tokens: Record<string, unknown>): ConnectorCredential {
  return { tokens, connectedAt: "2026-01-01T00:00:00.000Z" };
}

describe("provider choice and the sign-in gate", () => {
  it("stays in single-operator mode until a provider is chosen and its values are set", async () => {
    const signIn = new SignInService(secrets);
    expect(signIn.getAuthProvider()).toBeNull();
    expect(signIn.getAuthEnvGaps()).toBeNull();
    expect(signIn.authSignInReady()).toBe(false);

    await signIn.selectProvider("magic-link");
    expect(signIn.getAuthProvider()).toEqual({
      pluginId: "magic-link",
      pluginName: "magic-link",
      providerLabel: "Email link",
      provisioning: "manual",
      loginHintRequired: true,
    });
    // The HMAC key is generated on selection; only the From address is missing.
    expect(signIn.getAuthEnvGaps()).toEqual(["MAGIC_LINK_FROM"]);
    expect(signIn.providerValues("magic-link").MAGIC_LINK_SIGNING_SECRET).toMatch(/^[A-Za-z0-9_-]{64}$/);

    await signIn.setAuthSecret("MAGIC_LINK_FROM", "OpenNeko <signin@acme.com>");
    await signIn.setAuthSecret("RESEND_API_KEY", "re_1");
    expect(signIn.authSignInReady()).toBe(true);
    expect(signIn.getAuthConfiguredEnvKeys()).toEqual(["MAGIC_LINK_SIGNING_SECRET", "MAGIC_LINK_FROM", "RESEND_API_KEY"]);
    await expect(signIn.setAuthSecret("SCALEKIT_CLIENT_ID", "x")).rejects.toThrow(/not a Email link setting/);

    await signIn.selectProvider(null);
    expect(signIn.getAuthProvider()).toBeNull();
    expect(signIn.providerValues("magic-link").MAGIC_LINK_FROM).toBe("OpenNeko <signin@acme.com>");
  });

  it("runs the magic-link flow end to end with the stored values", async () => {
    const sent: EmailMessage[] = [];
    const signIn = new SignInService(secrets, { magicLink: { send: async (m) => void sent.push(m) } });
    await signIn.selectProvider("magic-link");
    await signIn.setProviderValues("magic-link", { MAGIC_LINK_FROM: "signin@acme.com" });
    const begin = await signIn.beginAuth({
      redirectUri: "https://neko.example/api/auth/callback",
      state: "st-1",
      loginHint: "ada@acme.com",
    });
    expect(begin.authorizationUrl).toBe("https://neko.example/signin?notice=link-sent");
    const link = new URL(/https:\/\/\S+/.exec(sent[0]!.text)![0]);
    const identity = await signIn.completeAuth({
      code: link.searchParams.get("code")!,
      redirectUri: "https://neko.example/api/auth/callback",
      state: "st-1",
    });
    expect(identity).toMatchObject({ sub: "magic-link:ada@acme.com", email: "ada@acme.com", groups: [] });
  });

  it("offers the directory only while Scalekit is the provider", async () => {
    const signIn = new SignInService(secrets);
    expect(signIn.getDirectoryProvider()).toBeNull();
    await signIn.selectProvider("scalekit");
    expect(signIn.getDirectoryProvider()?.capabilities.write).toEqual({ createUser: true, deactivateUser: false });
    await expect(signIn.applyDirectoryChange({ op: "deactivate_user", externalId: "u1" })).rejects.toThrow(/does not accept/);
  });
});

describe("Scalekit workspace connection", () => {
  it("stores the workspace credential in the deployment slot and refreshes it before a tool call", async () => {
    const refreshed: string[] = [];
    const seenTokens: string[] = [];
    const signIn = new SignInService(secrets, {
      scalekit: {
        createMcpOAuth: () => ({
          refreshTokens: async () => {
            refreshed.push("x");
            return { access_token: "at-new", refresh_token: "rt-new", expires_in: 3600 };
          },
        }) as never,
        createMcpClient: async ({ accessToken }) => ({
          callTool: async () => {
            seenTokens.push(accessToken);
            return { content: [{ type: "text", text: "ok" }] };
          },
          close: async () => {},
        }),
      },
    });
    expect(signIn.getDeploymentConnectStatus()).toEqual([]);
    await secrets.setOperatorCredential("deployment", "scalekit", credential({
      access_token: "at-old",
      refresh_token: "rt-old",
      expires_at: "0",
      token_endpoint: "https://as.scalekit.com/token",
      client_id: "dcr-1",
    }));
    expect(signIn.getDeploymentConnectStatus()).toEqual([
      { pluginName: "scalekit", connectedAt: "2026-01-01T00:00:00.000Z", scopes: undefined },
    ]);

    expect(await signIn.callWorkspaceTool("list_environments", {})).toBe("ok");
    expect(refreshed).toHaveLength(1);
    expect(seenTokens).toEqual(["at-new"]);
    expect(secrets.operatorCredential("deployment", "scalekit")?.tokens.access_token).toBe("at-new");

    expect(await signIn.disconnect("scalekit", "anyone")).toBe(true);
    expect(signIn.getDeploymentConnectStatus()).toEqual([]);
  });
});

describe("migrateLegacy", () => {
  it("moves plugin-era values, the workspace credential and the implied provider choice", async () => {
    await writeFullSecretsFile({
      env: {
        "@open-neko/plugin-scalekit": {
          SCALEKIT_ENVIRONMENT_URL: "https://acme.scalekit.dev",
          SCALEKIT_CLIENT_ID: "skc_1",
          SCALEKIT_CLIENT_SECRET: "sec",
          SCALEKIT_ORGANIZATION_ID: "org_1",
        },
        "pack.magento": { KEY: "keep" },
      },
      operators: { deployment: { "@open-neko/plugin-scalekit": credential({ access_token: "at" }) } },
    }, dir);
    await secrets.refresh();
    const signIn = new SignInService(secrets);

    expect(await signIn.migrateLegacy()).toEqual(["@open-neko/plugin-scalekit"]);
    expect(signIn.getAuthProvider()?.pluginName).toBe("scalekit");
    expect(signIn.authSignInReady()).toBe(true);
    expect(signIn.providerValues("scalekit").SCALEKIT_ORGANIZATION_ID).toBe("org_1");
    const file = await readFullSecretsFile(dir);
    expect(file.env["@open-neko/plugin-scalekit"]).toBeUndefined();
    expect(file.env["pack.magento"]).toEqual({ KEY: "keep" });
    expect(file.operators.deployment).toEqual({ scalekit: credential({ access_token: "at" }) });

    expect(await signIn.migrateLegacy()).toEqual([]);
  });

  it("chooses the magic-link provider when only its values were left behind", async () => {
    await writeFullSecretsFile({
      env: { "@open-neko/plugin-magic-link": { MAGIC_LINK_FROM: "signin@acme.com", MAGIC_LINK_SIGNING_SECRET: "k".repeat(64) } },
      operators: {},
    }, dir);
    await secrets.refresh();
    const signIn = new SignInService(secrets);
    await signIn.migrateLegacy();
    expect(signIn.getAuthProvider()?.pluginName).toBe("magic-link");
    expect(signIn.providerValues("magic-link").MAGIC_LINK_SIGNING_SECRET).toBe("k".repeat(64));
  });
});
