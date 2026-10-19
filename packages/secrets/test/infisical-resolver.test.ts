import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  InfisicalSecretsResolver,
  folderToSection,
  sectionToFolder,
} from "../src/infisical-resolver";

function fakeFetcher(handlers: {
  login?: () => { status: number; body: unknown };
  secrets?: () => { status: number; body: unknown };
}) {
  return vi.fn(async (input: URL | RequestInfo) => {
    const url = String(input);
    const h = url.includes("universal-auth/login")
      ? handlers.login
      : handlers.secrets;
    const { status, body } = h
      ? h()
      : { status: 404, body: { error: "no handler" } };
    return {
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
      text: async () => JSON.stringify(body),
    } as Response;
  }) as unknown as typeof fetch;
}

let dir: string;

beforeEach(async () => {
  dir = await mkdtemp(join(tmpdir(), "infisical-"));
});

afterEach(async () => {
  await rm(dir, { recursive: true, force: true });
});

describe("InfisicalSecretsResolver (SEC3)", () => {
  it("folder encoding round-trips section names", () => {
    expect(sectionToFolder("channel.slack")).toBe("channel.slack");
    expect(sectionToFolder("@scope/name")).toBe("@scope__name");
    expect(folderToSection("@scope__name")).toBe("@scope/name");
  });

  it("merges Infisical env bags with LOCAL operator credentials", async () => {
    await writeFile(
      join(dir, "secrets.json"),
      JSON.stringify({
        "@x/old-local": { LOCAL_ONLY: "ignored-when-infisical-works" },
        _operators: {
          "op-1": {
            "@x/p": { tokens: { access_token: "tok" }, connectedAt: "2026-01-01T00:00:00Z" },
          },
        },
      }),
      "utf8",
    );
    const fetcher = fakeFetcher({
      login: () => ({
        status: 200,
        body: { accessToken: "at", expiresIn: 3600 },
      }),
      secrets: () => ({
        status: 200,
        body: {
          secrets: [
            {
              secretKey: "SLACK_BOT_TOKEN",
              secretValue: "xoxb-1",
              secretPath: "/channel.slack/",
            },
            { secretKey: "ROOT_LEVEL", secretValue: "skip", secretPath: "/" },
          ],
        },
      }),
    });
    const resolver = new InfisicalSecretsResolver(
      { siteUrl: "https://inf.local", projectId: "p1", clientId: "c", clientSecret: "s" },
      fetcher,
      dir,
    );
    const full = await resolver.resolveFullSecrets();
    expect(full.env).toEqual({
      "channel.slack": { SLACK_BOT_TOKEN: "xoxb-1" },
    });
    expect(full.operators["op-1"]?.["@x/p"]?.tokens).toEqual({
      access_token: "tok",
    });
  });

  it("caches secrets within the TTL (one fetch for two resolves)", async () => {
    const secrets = vi.fn(() => ({
      status: 200,
      body: { secrets: [] },
    }));
    const fetcher = fakeFetcher({
      login: () => ({ status: 200, body: { accessToken: "at", expiresIn: 3600 } }),
      secrets,
    });
    const resolver = new InfisicalSecretsResolver(
      { siteUrl: "https://inf.local", projectId: "p1", clientId: "c", clientSecret: "s", cacheTtlMs: 60_000 },
      fetcher,
      dir,
    );
    await resolver.resolveFullSecrets();
    await resolver.resolveFullSecrets();
    expect(secrets).toHaveBeenCalledTimes(1);
  });

  it("falls back to the local file when Infisical is unreachable", async () => {
    await writeFile(
      join(dir, "secrets.json"),
      JSON.stringify({ "@x/p": { KEY: "local-value" } }),
      "utf8",
    );
    const warn = vi.fn();
    const resolver = new InfisicalSecretsResolver(
      { siteUrl: "https://inf.local", projectId: "p1", clientId: "c", clientSecret: "s" },
      fakeFetcher({ login: () => ({ status: 503, body: {} }) }),
      dir,
    );
    const full = await resolver.resolveFullSecrets(warn);
    expect(full.env["@x/p"]?.KEY).toBe("local-value");
    expect(warn).toHaveBeenCalledWith(
      expect.stringContaining("falling back to the local secrets file"),
    );
  });
});
