import { describe, expect, it } from "vitest";
import {
  clampTtl,
  DEFAULT_TTL_SECONDS,
  MAX_TTL_SECONDS,
  MIN_TTL_SECONDS,
  mintToken,
  verifyToken,
} from "../../../src/signin/magic-link/token.js";

const SECRET = "s".repeat(32);
const NOW = 1_755_000_000_000;

function mint(overrides: Partial<Parameters<typeof mintToken>[0]> = {}) {
  return mintToken({
    email: "person@company.com",
    state: "state-abc",
    secret: SECRET,
    nowMs: NOW,
    ...overrides,
  });
}

describe("mintToken / verifyToken", () => {
  it("round-trips claims", () => {
    const token = mint();
    const claims = verifyToken({
      token,
      state: "state-abc",
      secret: SECRET,
      nowMs: NOW + 60_000,
    });
    expect(claims.email).toBe("person@company.com");
    expect(claims.state).toBe("state-abc");
    expect(claims.expiresAt).toBe(Math.floor(NOW / 1000) + DEFAULT_TTL_SECONDS);
  });

  it("refuses to mint with a short secret", () => {
    expect(() => mint({ secret: "short" })).toThrow(/32 characters/);
  });

  it("rejects a tampered payload", () => {
    const token = mint();
    const [payload, sig] = token.split(".");
    const claims = JSON.parse(Buffer.from(payload, "base64url").toString());
    claims.email = "attacker@evil.com";
    const forged = `${Buffer.from(JSON.stringify(claims)).toString("base64url")}.${sig}`;
    expect(() =>
      verifyToken({ token: forged, state: "state-abc", secret: SECRET, nowMs: NOW }),
    ).toThrow(/not valid/);
  });

  it("rejects a token signed with a different secret", () => {
    const token = mint({ secret: "x".repeat(32) });
    expect(() =>
      verifyToken({ token, state: "state-abc", secret: SECRET, nowMs: NOW }),
    ).toThrow(/not valid/);
  });

  it("rejects a state mismatch (replay in another browser)", () => {
    const token = mint();
    expect(() =>
      verifyToken({ token, state: "other-state", secret: SECRET, nowMs: NOW }),
    ).toThrow(/browser that requested it/);
  });

  it("rejects an expired token", () => {
    const token = mint();
    expect(() =>
      verifyToken({
        token,
        state: "state-abc",
        secret: SECRET,
        nowMs: NOW + (DEFAULT_TTL_SECONDS + 1) * 1000,
      }),
    ).toThrow(/expired/);
  });

  it("rejects garbage tokens cleanly", () => {
    for (const junk of ["", "a", "a.b", "!!.??", `${"a".repeat(10)}.${"b".repeat(10)}`]) {
      expect(() =>
        verifyToken({ token: junk, state: "s", secret: SECRET, nowMs: NOW }),
      ).toThrow(/not valid/);
    }
  });
});

describe("clampTtl", () => {
  it("defaults, floors, and caps", () => {
    expect(clampTtl(undefined)).toBe(DEFAULT_TTL_SECONDS);
    expect(clampTtl(NaN)).toBe(DEFAULT_TTL_SECONDS);
    expect(clampTtl(5)).toBe(MIN_TTL_SECONDS);
    expect(clampTtl(86_400)).toBe(MAX_TTL_SECONDS);
    expect(clampTtl(120)).toBe(120);
  });
});
