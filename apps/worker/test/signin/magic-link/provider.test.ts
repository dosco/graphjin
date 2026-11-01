import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { EmailMessage } from "../../../src/signin/magic-link/email.js";
import {
  beginMagicLinkAuth,
  completeMagicLinkAuth,
  type MagicLinkOptions,
} from "../../../src/signin/magic-link/provider.js";
import type { BeginAuthParams, CompleteAuthParams } from "../../../src/signin/types.js";

// The tests drive settings through process.env; the provider takes them explicitly.
const settings = () => ({
  MAGIC_LINK_SIGNING_SECRET: process.env.MAGIC_LINK_SIGNING_SECRET,
  MAGIC_LINK_FROM: process.env.MAGIC_LINK_FROM,
  MAGIC_LINK_TTL_SECONDS: process.env.MAGIC_LINK_TTL_SECONDS,
});
const runBeginAuth = (params: BeginAuthParams, options?: MagicLinkOptions) =>
  beginMagicLinkAuth(params, settings(), options);
const runCompleteAuth = async (params: CompleteAuthParams, options?: MagicLinkOptions) => ({
  identity: await completeMagicLinkAuth(params, settings(), options),
});

const SECRET = "s".repeat(32);
const NOW = 1_755_000_000_000;
const CALLBACK = "https://neko.example/api/auth/callback";

describe("magic-link auth flow", () => {
  let sent: EmailMessage[];
  const send = vi.fn(async (message: EmailMessage) => {
    sent.push(message);
  });

  beforeEach(() => {
    sent = [];
    send.mockClear();
    process.env.MAGIC_LINK_SIGNING_SECRET = SECRET;
    process.env.MAGIC_LINK_FROM = "OpenNeko <signin@company.com>";
    delete process.env.MAGIC_LINK_TTL_SECONDS;
  });
  afterEach(() => {
    delete process.env.MAGIC_LINK_SIGNING_SECRET;
    delete process.env.MAGIC_LINK_FROM;
  });

  it("emails a link that completes into the same identity", async () => {
    const begin = await runBeginAuth(
      {
        redirectUri: CALLBACK,
        state: "state-1",
        loginHint: "Person@Company.com",
      },
      { send, nowMs: NOW },
    );
    expect(begin.authorizationUrl).toBe(
      "https://neko.example/signin?notice=link-sent",
    );
    expect(sent).toHaveLength(1);
    expect(sent[0].to).toBe("person@company.com");

    const linkMatch = /https:\/\/\S+/.exec(sent[0].text);
    expect(linkMatch).not.toBeNull();
    const link = new URL(linkMatch![0]);
    expect(`${link.origin}${link.pathname}`).toBe(CALLBACK);
    expect(link.searchParams.get("state")).toBe("state-1");

    const complete = await runCompleteAuth(
      {
        code: link.searchParams.get("code")!,
        redirectUri: CALLBACK,
        state: "state-1",
      },
      { nowMs: NOW + 60_000 },
    );
    expect(complete.identity).toEqual({
      sub: "magic-link:person@company.com",
      email: "person@company.com",
      name: null,
      orgId: null,
      groups: [],
    });
  });

  it("refuses to begin without a usable email hint", async () => {
    for (const loginHint of [null, "", "not-an-email"]) {
      await expect(
        runBeginAuth(
          { redirectUri: CALLBACK, state: "s", loginHint },
          { send, nowMs: NOW },
        ),
      ).rejects.toThrow(/email address as loginHint/);
    }
    expect(send).not.toHaveBeenCalled();
  });

  it("rejects completion with a mismatched state", async () => {
    await runBeginAuth(
      { redirectUri: CALLBACK, state: "state-1", loginHint: "a@b.co" },
      { send, nowMs: NOW },
    );
    const link = new URL(/https:\/\/\S+/.exec(sent[0].text)![0]);
    await expect(
      runCompleteAuth(
        {
          code: link.searchParams.get("code")!,
          redirectUri: CALLBACK,
          state: "different-state",
        },
        { nowMs: NOW },
      ),
    ).rejects.toThrow(/browser that requested it/);
  });

  it("rejects completion after expiry", async () => {
    process.env.MAGIC_LINK_TTL_SECONDS = "60";
    await runBeginAuth(
      { redirectUri: CALLBACK, state: "state-1", loginHint: "a@b.co" },
      { send, nowMs: NOW },
    );
    const link = new URL(/https:\/\/\S+/.exec(sent[0].text)![0]);
    await expect(
      runCompleteAuth(
        {
          code: link.searchParams.get("code")!,
          redirectUri: CALLBACK,
          state: "state-1",
        },
        { nowMs: NOW + 61_000 },
      ),
    ).rejects.toThrow(/expired/);
  });

  it("never returns groups — role assignment stays with the core", async () => {
    await runBeginAuth(
      { redirectUri: CALLBACK, state: "state-1", loginHint: "a@b.co" },
      { send, nowMs: NOW },
    );
    const link = new URL(/https:\/\/\S+/.exec(sent[0].text)![0]);
    const complete = await runCompleteAuth(
      {
        code: link.searchParams.get("code")!,
        redirectUri: CALLBACK,
        state: "state-1",
      },
      { nowMs: NOW },
    );
    expect(complete.identity.groups).toEqual([]);
  });

  it("fails begin when the signing secret is missing or short", async () => {
    process.env.MAGIC_LINK_SIGNING_SECRET = "short";
    await expect(
      runBeginAuth(
        { redirectUri: CALLBACK, state: "s", loginHint: "a@b.co" },
        { send, nowMs: NOW },
      ),
    ).rejects.toThrow(/MAGIC_LINK_SIGNING_SECRET/);
  });

  it("fails begin when MAGIC_LINK_FROM is unset", async () => {
    delete process.env.MAGIC_LINK_FROM;
    await expect(
      runBeginAuth(
        { redirectUri: CALLBACK, state: "s", loginHint: "a@b.co" },
        { send, nowMs: NOW },
      ),
    ).rejects.toThrow(/MAGIC_LINK_FROM/);
  });

  it("rejects a relative redirectUri", async () => {
    await expect(
      runBeginAuth(
        { redirectUri: "/api/auth/callback", state: "s", loginHint: "a@b.co" },
        { send, nowMs: NOW },
      ),
    ).rejects.toThrow(/absolute URL/);
  });

  it("requires redirectUri and state", async () => {
    await expect(
      runBeginAuth(
        { redirectUri: "", state: "s", loginHint: "a@b.co" },
        { send, nowMs: NOW },
      ),
    ).rejects.toThrow(/redirectUri/);
    await expect(
      runBeginAuth(
        { redirectUri: CALLBACK, state: "", loginHint: "a@b.co" },
        { send, nowMs: NOW },
      ),
    ).rejects.toThrow(/state/);
    await expect(
      runCompleteAuth(
        { code: "", redirectUri: CALLBACK, state: "s" },
        { nowMs: NOW },
      ),
    ).rejects.toThrow(/code/);
    await expect(
      runCompleteAuth(
        { code: "x.y", redirectUri: CALLBACK, state: "" },
        { nowMs: NOW },
      ),
    ).rejects.toThrow(/state/);
  });

  it("honors MAGIC_LINK_TTL_SECONDS within its 60–600 clamp", async () => {
    process.env.MAGIC_LINK_TTL_SECONDS = "120";
    await runBeginAuth(
      { redirectUri: CALLBACK, state: "s1", loginHint: "a@b.co" },
      { send, nowMs: NOW },
    );
    const link = new URL(/https:\/\/\S+/.exec(sent[0].text)![0]);
    const code = link.searchParams.get("code")!;
    // Valid inside the window…
    await runCompleteAuth(
      { code, redirectUri: CALLBACK, state: "s1" },
      { nowMs: NOW + 119_000 },
    );
    // …expired after it.
    await expect(
      runCompleteAuth(
        { code, redirectUri: CALLBACK, state: "s1" },
        { nowMs: NOW + 121_000 },
      ),
    ).rejects.toThrow(/expired/);
    // Below the floor the clamp raises to 60s: still valid at 59s.
    process.env.MAGIC_LINK_TTL_SECONDS = "5";
    sent = [];
    await runBeginAuth(
      { redirectUri: CALLBACK, state: "s2", loginHint: "a@b.co" },
      { send, nowMs: NOW },
    );
    const clamped = new URL(/https:\/\/\S+/.exec(sent[0].text)![0]);
    await runCompleteAuth(
      {
        code: clamped.searchParams.get("code")!,
        redirectUri: CALLBACK,
        state: "s2",
      },
      { nowMs: NOW + 59_000 },
    );
  });

  it("mentions the expiry window in the email body", async () => {
    process.env.MAGIC_LINK_TTL_SECONDS = "300";
    await runBeginAuth(
      { redirectUri: CALLBACK, state: "s", loginHint: "a@b.co" },
      { send, nowMs: NOW },
    );
    expect(sent[0].text).toContain("5 minutes");
    expect(sent[0].subject).toContain("sign-in link");
  });

  it("preserves the callback's existing query params in the link", async () => {
    await runBeginAuth(
      {
        redirectUri: `${CALLBACK}?tenant=acme`,
        state: "s",
        loginHint: "a@b.co",
      },
      { send, nowMs: NOW },
    );
    const link = new URL(/https:\/\/\S+/.exec(sent[0].text)![0]);
    expect(link.searchParams.get("tenant")).toBe("acme");
    expect(link.searchParams.get("state")).toBe("s");
    expect(link.searchParams.get("code")).toBeTruthy();
  });

  it("a token minted under a rotated secret no longer verifies", async () => {
    await runBeginAuth(
      { redirectUri: CALLBACK, state: "s", loginHint: "a@b.co" },
      { send, nowMs: NOW },
    );
    const link = new URL(/https:\/\/\S+/.exec(sent[0].text)![0]);
    process.env.MAGIC_LINK_SIGNING_SECRET = "r".repeat(48);
    await expect(
      runCompleteAuth(
        {
          code: link.searchParams.get("code")!,
          redirectUri: CALLBACK,
          state: "s",
        },
        { nowMs: NOW },
      ),
    ).rejects.toThrow(/not valid/);
  });
});
