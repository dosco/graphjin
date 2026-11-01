/**
 * Secrets are always encrypted: file at ~/.config/openneko/secret-key →
 * auto-generated 32-byte random key. No env override. Tests use a temp
 * HOME so they don't touch the dev's real key file.
 */

import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  _resetSecretKeyCacheForTesting,
  maybeDecryptSecret,
  maybeEncryptSecret,
} from "../src/secrets";

describe("server-provider-secrets", () => {
  let tempHome: string;
  const ORIGINAL_HOME = process.env.HOME;
  const ORIGINAL_XDG = process.env.XDG_CONFIG_HOME;

  beforeEach(async () => {
    tempHome = await mkdtemp(join(tmpdir(), "neko-secrets-test-"));
    process.env.HOME = tempHome;
    delete process.env.XDG_CONFIG_HOME;
    _resetSecretKeyCacheForTesting();
  });

  afterEach(async () => {
    if (ORIGINAL_HOME) process.env.HOME = ORIGINAL_HOME;
    if (ORIGINAL_XDG === undefined) delete process.env.XDG_CONFIG_HOME;
    else process.env.XDG_CONFIG_HOME = ORIGINAL_XDG;
    _resetSecretKeyCacheForTesting();
    await rm(tempHome, { recursive: true, force: true });
  });

  it("encrypts using an auto-generated key on first call", () => {
    const ciphertext = maybeEncryptSecret("sk-plain-key");
    expect(ciphertext).toMatch(/^enc:v1:/);
    expect(ciphertext).not.toContain("sk-plain-key");
  });

  it("round-trips a typical API key", () => {
    const plaintext = "sk-proj-abcdefghijklmnopqrstuvwxyz0123456789";
    const ciphertext = maybeEncryptSecret(plaintext);
    expect(maybeDecryptSecret(ciphertext)).toBe(plaintext);
  });

  it("round-trips Unicode (emoji + multibyte)", () => {
    const plaintext = "secret-密码-🔐-passphrase";
    const ciphertext = maybeEncryptSecret(plaintext);
    expect(maybeDecryptSecret(ciphertext)).toBe(plaintext);
  });

  it("produces different ciphertexts for the same plaintext (random IV)", () => {
    const plaintext = "deterministic-input";
    const a = maybeEncryptSecret(plaintext);
    const b = maybeEncryptSecret(plaintext);
    expect(a).not.toBe(b);
    expect(maybeDecryptSecret(a)).toBe(plaintext);
    expect(maybeDecryptSecret(b)).toBe(plaintext);
  });

  it("rejects ciphertext encrypted under a different key", async () => {
    const ciphertext = maybeEncryptSecret("foo");
    // Wipe HOME → secrets module rebuilds against a fresh auto-generated key.
    await rm(tempHome, { recursive: true, force: true });
    tempHome = await mkdtemp(join(tmpdir(), "neko-secrets-test-"));
    process.env.HOME = tempHome;
    _resetSecretKeyCacheForTesting();
    expect(() => maybeDecryptSecret(ciphertext)).toThrow();
  });

  it("rejects tampered ciphertext (GCM auth tag detects mutation)", () => {
    const ciphertext = maybeEncryptSecret("foo");
    const body = ciphertext.slice("enc:v1:".length);
    const buf = Buffer.from(body, "base64");
    buf[buf.length - 1] ^= 0xff;
    const tampered = `enc:v1:${buf.toString("base64")}`;
    expect(() => maybeDecryptSecret(tampered)).toThrow();
  });

  it("plaintext (non-prefixed) values pass through decrypt unchanged", () => {
    expect(maybeDecryptSecret("legacy-plaintext-key")).toBe("legacy-plaintext-key");
  });

  it("decrypt of empty / non-string returns empty string", () => {
    expect(maybeDecryptSecret(undefined)).toBe("");
    expect(maybeDecryptSecret(null)).toBe("");
    expect(maybeDecryptSecret(42)).toBe("");
    expect(maybeDecryptSecret("")).toBe("");
  });
});
