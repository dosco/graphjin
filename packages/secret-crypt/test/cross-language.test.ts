import { readFileSync } from "node:fs";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  _resetSecretKeyCacheForTesting,
  maybeDecryptSecret,
  maybeEncryptSecret,
} from "../src/index";

// Pins the TS and Go enc:v1 implementations together. The fixture files
// were encrypted by the TS cipher; apps/openneko/internal/secrets has the
// mirror test reading the SAME files with the Go cipher, and the
// GO_ENCRYPTED constant below was produced by the Go cipher
// (config.EncryptValue) with the fixture key. If either implementation
// drifts, one of the two suites fails.
const FIXTURE_XDG = join(__dirname, "fixtures", "xdg");
const FIXTURE_DIR = join(FIXTURE_XDG, "openneko");

const prevXdg = process.env.XDG_CONFIG_HOME;

beforeAll(() => {
  process.env.XDG_CONFIG_HOME = FIXTURE_XDG;
  _resetSecretKeyCacheForTesting();
});

afterAll(() => {
  if (prevXdg === undefined) delete process.env.XDG_CONFIG_HOME;
  else process.env.XDG_CONFIG_HOME = prevXdg;
  _resetSecretKeyCacheForTesting();
});

describe("cross-language enc:v1 fixture", () => {
  it("decrypts the TS-encrypted secrets.json env value", () => {
    const secrets = JSON.parse(
      readFileSync(join(FIXTURE_DIR, "secrets.json"), "utf8"),
    );
    expect(
      maybeDecryptSecret(secrets["@open-neko/plugin-slack"].SLACK_BOT_TOKEN),
    ).toBe("xoxb-fixture-token");
  });

  it("decrypts the operator credential blob", () => {
    const secrets = JSON.parse(
      readFileSync(join(FIXTURE_DIR, "secrets.json"), "utf8"),
    );
    const blob = maybeDecryptSecret(
      secrets._operators["op-1"]["@open-neko/plugin-google"],
    );
    expect(JSON.parse(blob).tokens.access_token).toBe("ya29.fixture-access");
  });

  it("decrypts the config.json pg.password", () => {
    const cfg = JSON.parse(
      readFileSync(join(FIXTURE_DIR, "config.json"), "utf8"),
    );
    expect(maybeDecryptSecret(cfg.pg.password)).toBe("pg-fixture-pass");
  });

  it("decrypts a Go-encrypted value (reverse direction)", () => {
    expect(maybeDecryptSecret(GO_ENCRYPTED)).toBe("go-encrypted-fixture-value");
  });

  it("round-trips through encrypt/decrypt", () => {
    const ct = maybeEncryptSecret("round-trip");
    expect(ct.startsWith("enc:v1:")).toBe(true);
    expect(maybeDecryptSecret(ct)).toBe("round-trip");
  });

  it("passes legacy plaintext through unchanged", () => {
    expect(maybeDecryptSecret("plain-old-value")).toBe("plain-old-value");
  });
});

// Produced by apps/openneko/internal/config EncryptValue with the fixture
// secret-key. Regenerate via the throwaway snippet in that package if the
// fixture key ever changes.
const GO_ENCRYPTED = "enc:v1:NUHQdIPgY89FKDK8Z50peWd2419p4EBE6aZHYcvnwoXsx3mSlKrxWRGwKgq2cjNfEvISg24N";
