import {
  createCipheriv,
  createDecipheriv,
  createHash,
  createHmac,
  randomBytes,
} from "node:crypto";
import { linkSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

/**
 * App secret used to encrypt LLM API keys at rest in `llm_provider_config`.
 *
 * Resolution: read `~/.config/openneko/secret-key` file (mode 0600) → if
 * missing, generate 32 random bytes and persist. Cached after first read.
 *
 * Local dev: nothing to configure — the file is auto-created on first run.
 * Prod: pre-populate `~/.config/openneko/secret-key` via init container /
 * secrets-manager mount before the app starts.
 *
 * No env override — same policy as the rest of app config. XDG_CONFIG_HOME
 * is respected for non-default config locations.
 *
 * Test fixtures redirect writes via process.env.HOME (or XDG_CONFIG_HOME),
 * matching the pattern used by `local-config.ts` and `host-provision.ts`.
 */

const PREFIX = "enc:v1:";

let _cachedKey: Buffer | null = null;

function appSecretKeyPath(): string {
  const xdg = process.env.XDG_CONFIG_HOME?.trim();
  const base = xdg && xdg.length > 0
    ? xdg
    : join(process.env.HOME || homedir(), ".config");
  return join(base, "openneko", "secret-key");
}

function generateAndPersist(): string {
  const path = appSecretKeyPath();
  mkdirSync(join(path, ".."), { recursive: true });
  const value = randomBytes(32).toString("base64");
  // Exclusive publish: write the complete key to a private temp file, then
  // link(2) it into place. link fails with EEXIST when a concurrent process
  // won the race — their key is then THE deployment key and ours is
  // discarded. A plain truncate-and-write here let two processes cache
  // different keys, and exposed an empty/partial file to concurrent readers.
  const tmp = `${path}.tmp-${process.pid}-${randomBytes(4).toString("hex")}`;
  writeFileSync(tmp, value, { encoding: "utf8", mode: 0o600 });
  try {
    linkSync(tmp, path);
  } catch (e) {
    const code = (e as NodeJS.ErrnoException).code;
    if (code === "EPERM" || code === "ENOSYS" || code === "EXDEV") {
      // Filesystem without hardlink support: fall back to exclusive create.
      try {
        writeFileSync(path, value, { encoding: "utf8", mode: 0o600, flag: "wx" });
      } catch (e2) {
        if ((e2 as NodeJS.ErrnoException).code !== "EEXIST") {
          rmSync(tmp, { force: true });
          throw e2;
        }
      }
    } else if (code !== "EEXIST") {
      rmSync(tmp, { force: true });
      throw e;
    }
  }
  rmSync(tmp, { force: true });
  // Re-read whatever actually landed — ours, or the race winner's.
  const stored = readFileSync(path, "utf8").trim();
  if (!stored) {
    throw new Error(
      `secret-key at ${path} exists but is empty; remove the file to let OpenNeko generate a new key`,
    );
  }
  return stored;
}

function readSecret(): string {
  const path = appSecretKeyPath();
  let raw: string | null = null;
  try {
    raw = readFileSync(path, "utf8");
  } catch (e) {
    // ONLY a missing file may trigger generation. Every stored secret —
    // LLM API keys, plugin secrets, the rotated DB password — and every
    // derived signing secret is keyed by this file, so regenerating on a
    // transient read failure (EACCES during an init chown, EISDIR from a
    // bad mount, ...) would silently orphan all of them.
    if ((e as NodeJS.ErrnoException).code !== "ENOENT") {
      throw new Error(
        `secret-key unreadable at ${path} (${(e as NodeJS.ErrnoException).code}); refusing to regenerate — fix access to the existing key file`,
        { cause: e },
      );
    }
  }
  if (raw !== null) {
    const stored = raw.trim();
    if (stored) return stored;
    throw new Error(
      `secret-key at ${path} exists but is empty; remove the file to let OpenNeko generate a new key`,
    );
  }
  return generateAndPersist();
}

function getKey(): Buffer {
  if (_cachedKey) return _cachedKey;
  const secret = readSecret();
  _cachedKey = createHash("sha256").update(secret).digest();
  return _cachedKey;
}

/**
 * Test-only: drop the cached key so a temp HOME picks up its own file.
 * Not exported via package.json — only used by integration tests.
 */
export function _resetSecretKeyCacheForTesting(): void {
  _cachedKey = null;
}

export function maybeEncryptSecret(value: string): string {
  const key = getKey();
  const iv = randomBytes(12);
  const cipher = createCipheriv("aes-256-gcm", key, iv);
  const encrypted = Buffer.concat([cipher.update(value, "utf8"), cipher.final()]);
  const tag = cipher.getAuthTag();
  return `${PREFIX}${Buffer.concat([iv, tag, encrypted]).toString("base64")}`;
}

/**
 * Derive a stable per-label signing secret from the deployment key
 * (HMAC-SHA256 of the label) — no extra key storage, rotates with the
 * secret-key file. GJ4 uses `graphjin:<orgId>` labels for per-org
 * GraphJin JWT signing secrets.
 */
export function deriveSigningSecret(label: string): Buffer {
  return createHmac("sha256", getKey()).update(label).digest();
}

export function maybeDecryptSecret(value: unknown): string {
  if (typeof value !== "string" || !value) return "";
  if (!value.startsWith(PREFIX)) return value;

  const key = getKey();
  const raw = Buffer.from(value.slice(PREFIX.length), "base64");
  const iv = raw.subarray(0, 12);
  const tag = raw.subarray(12, 28);
  const encrypted = raw.subarray(28);
  const decipher = createDecipheriv("aes-256-gcm", key, iv);
  decipher.setAuthTag(tag);
  return Buffer.concat([decipher.update(encrypted), decipher.final()]).toString("utf8");
}
