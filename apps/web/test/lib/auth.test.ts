import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  decodeSession,
  encodeSession,
  newStateToken,
} from "@/lib/auth";

describe("session encode/decode", () => {
  const goodSecret = "a".repeat(64);
  const otherSecret = "b".repeat(64);

  beforeEach(() => {
    process.env.OPENNEKO_SESSION_SECRET = goodSecret;
  });
  afterEach(() => {
    delete process.env.OPENNEKO_SESSION_SECRET;
  });

  it("encodes then decodes a session round-trip", () => {
    const expiresAt = Math.floor(Date.now() / 1000) + 3600;
    const token = encodeSession({
      userId: "usr_abc",
      email: "",
      name: null,
      expiresAt,
    });
    const decoded = decodeSession(token);
    expect(decoded?.userId).toBe("usr_abc");
    expect(decoded?.expiresAt).toBe(expiresAt);
  });

  it("rejects a token signed with a different secret", () => {
    const expiresAt = Math.floor(Date.now() / 1000) + 3600;
    const token = encodeSession({
      userId: "usr_abc",
      email: "",
      name: null,
      expiresAt,
    });
    process.env.OPENNEKO_SESSION_SECRET = otherSecret;
    expect(decodeSession(token)).toBeNull();
  });

  it("rejects a token whose body has been tampered with", () => {
    const expiresAt = Math.floor(Date.now() / 1000) + 3600;
    const token = encodeSession({
      userId: "usr_abc",
      email: "",
      name: null,
      expiresAt,
    });
    const [user, exp, mac] = token.split(".");
    const tampered = `${user}_evil.${exp}.${mac}`;
    expect(decodeSession(tampered)).toBeNull();
  });

  it("rejects an expired token", () => {
    const expiresAt = Math.floor(Date.now() / 1000) - 60;
    const token = encodeSession({
      userId: "usr_abc",
      email: "",
      name: null,
      expiresAt,
    });
    expect(decodeSession(token)).toBeNull();
  });

  it("rejects garbage input cleanly", () => {
    expect(decodeSession("")).toBeNull();
    expect(decodeSession("x.y")).toBeNull();
    expect(decodeSession("a.b.c.d")).toBeNull();
  });

  it("derives a stable per-install secret when the env var is unset", () => {
    // No compose file sets OPENNEKO_SESSION_SECRET; falling back to a
    // secret derived from the deployment secret-key keeps sessions valid
    // across restarts instead of throwing on every SSO deployment.
    delete process.env.OPENNEKO_SESSION_SECRET;
    const session = {
      userId: "u",
      email: "",
      name: null,
      expiresAt: Math.floor(Date.now() / 1000) + 60,
    };
    const encoded = encodeSession(session);
    expect(decodeSession(encoded)?.userId).toBe("u");
  });

  it("throws when the session secret is explicitly set but too short", () => {
    process.env.OPENNEKO_SESSION_SECRET = "tooshort";
    expect(() =>
      encodeSession({
        userId: "u",
        email: "",
        name: null,
        expiresAt: Math.floor(Date.now() / 1000) + 60,
      }),
    ).toThrow(/OPENNEKO_SESSION_SECRET/);
  });
});

describe("newStateToken", () => {
  it("returns a high-entropy base64url string", () => {
    const a = newStateToken();
    const b = newStateToken();
    expect(a).not.toBe(b);
    expect(a).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(a.length).toBeGreaterThanOrEqual(32);
  });
});
