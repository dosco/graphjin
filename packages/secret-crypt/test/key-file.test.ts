import { execFile } from "node:child_process";
import { createRequire } from "node:module";
import { mkdirSync, mkdtempSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { afterEach, describe, expect, it } from "vitest";

import {
  _resetSecretKeyCacheForTesting,
  maybeDecryptSecret,
  maybeEncryptSecret,
} from "../src/index";

const execFileAsync = promisify(execFile);
const require = createRequire(import.meta.url);
const here = fileURLToPath(new URL(".", import.meta.url));

function freshConfigHome(): string {
  const dir = mkdtempSync(join(tmpdir(), "secret-crypt-"));
  process.env.XDG_CONFIG_HOME = dir;
  _resetSecretKeyCacheForTesting();
  return dir;
}

afterEach(() => {
  delete process.env.XDG_CONFIG_HOME;
  _resetSecretKeyCacheForTesting();
});

describe("secret-key creation", () => {
  it("generates a 0600 key on first use and reuses it", () => {
    const dir = freshConfigHome();
    const round = maybeDecryptSecret(maybeEncryptSecret("hello"));
    expect(round).toBe("hello");
    const keyPath = join(dir, "openneko", "secret-key");
    const info = statSync(keyPath);
    expect(info.mode & 0o777).toBe(0o600);
    expect(readFileSync(keyPath, "utf8").trim().length).toBeGreaterThan(0);
  });

  it("adopts an existing key instead of overwriting it", () => {
    const dir = freshConfigHome();
    const keyPath = join(dir, "openneko", "secret-key");
    mkdirSync(join(dir, "openneko"), { recursive: true, mode: 0o700 });
    writeFileSync(keyPath, "pre-existing-key-material\n", { mode: 0o600 });

    const encrypted = maybeEncryptSecret("hello");
    expect(readFileSync(keyPath, "utf8")).toBe("pre-existing-key-material\n");
    expect(maybeDecryptSecret(encrypted)).toBe("hello");
  });

  it("throws instead of regenerating when the key file is unreadable", () => {
    const dir = freshConfigHome();
    // A directory at the key path yields EISDIR — a non-ENOENT failure that
    // previously triggered a silent, destructive regeneration.
    mkdirSync(join(dir, "openneko", "secret-key"), { recursive: true });
    expect(() => maybeEncryptSecret("hello")).toThrow(/refusing to regenerate/);
  });

  it("throws on an empty key file rather than replacing it", () => {
    const dir = freshConfigHome();
    mkdirSync(join(dir, "openneko"), { recursive: true, mode: 0o700 });
    writeFileSync(join(dir, "openneko", "secret-key"), "", { mode: 0o600 });
    expect(() => maybeEncryptSecret("hello")).toThrow(/exists but is empty/);
    expect(readFileSync(join(dir, "openneko", "secret-key"), "utf8")).toBe("");
  });
});

describe("concurrent first-use across processes", () => {
  it("every process converges on one key", async () => {
    const dir = freshConfigHome();
    // tsx is a dependency of the worker app; resolve its loader from there so
    // child node processes can import the TS source directly, like the
    // packaged one-shots do.
    const tsxLoader = require.resolve("tsx/esm", {
      paths: [join(here, "..", "..", "..", "apps", "worker")],
    });
    const entry = join(here, "..", "src", "index.ts");
    const script = `
      const { maybeEncryptSecret } = await import(${JSON.stringify(entry)});
      process.stdout.write(maybeEncryptSecret("probe"));
    `;

    const children = Array.from({ length: 6 }, () =>
      execFileAsync(
        process.execPath,
        ["--import", tsxLoader, "--input-type=module", "-e", script],
        { env: { ...process.env, XDG_CONFIG_HOME: dir } },
      ),
    );
    const outputs = await Promise.all(children);

    // The parent process reads whichever key won; every child's ciphertext
    // must decrypt under it — divergent cached keys would fail GCM auth.
    _resetSecretKeyCacheForTesting();
    for (const { stdout } of outputs) {
      expect(stdout.startsWith("enc:v1:")).toBe(true);
      expect(maybeDecryptSecret(stdout)).toBe("probe");
    }
  }, 30_000);
});
