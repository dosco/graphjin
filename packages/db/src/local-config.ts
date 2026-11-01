/**
 * Local config file at `~/.config/openneko/config.json` (XDG base dir).
 *
 * Stores per-host bootstrap state that can't live in the metadata DB
 * (because the app needs it before the DB connection exists). Currently:
 *
 *   {
 *     "pg": {
 *       "host": "localhost",       // optional override; default localhost
 *       "port": 5432,              // optional override; default 5432
 *       "user": "neko",            // optional override; default 'neko'
 *       "password": "<set by /setup>", // when present, signals admin has changed from default 'secret'
 *       "database": "neko"         // optional override; default 'neko'
 *     }
 *   }
 *
 * On first boot the file doesn't exist; the app uses the hardcoded defaults
 * (which match what the docker compose ships with). The /setup wizard's
 * "Set DB password" step writes a new password to this file and runs
 * ALTER ROLE. Presence of pg.password here is the signal that the admin has
 * changed the password from the default.
 *
 * Sister file in the same dir: `~/.config/openneko/secret-key` (at-rest key).
 */

import {
  chmodSync,
  mkdirSync,
  readFileSync,
  renameSync,
  writeFileSync,
} from "node:fs";
import { randomUUID } from "node:crypto";
import { homedir } from "node:os";
import { join } from "node:path";
import { maybeDecryptSecret, maybeEncryptSecret } from "@neko/secret-crypt";

export type LocalPgConfig = {
  host?: string;
  port?: number;
  user?: string;
  password?: string;
  database?: string;
  sslmode?: string;
  sslrootcert?: string;
};

export type LocalSecretsConfig = {
  /** SEC2/SEC3 residency: "file" (default) or "infisical". */
  backend?: "file" | "infisical";
  infisical?: {
    siteUrl: string;
    projectId: string;
    environment?: string;
  };
};

export type LocalConfig = {
  /** A postgres:// URL for OpenNeko's database. It replaces `pg`. */
  database_url?: string;
  pg?: LocalPgConfig;
  secrets?: LocalSecretsConfig;
};

function configBase(): string {
  const xdg = process.env.XDG_CONFIG_HOME?.trim();
  return xdg && xdg.length > 0
    ? xdg
    : join(process.env.HOME || homedir(), ".config");
}

function configDir(): string {
  return join(configBase(), "openneko");
}

// Pre-rebrand path. Long-lived hosts may still have the file here from
// before the neko → openneko rename; readers fall back to it (writers
// always go to the new path).
function legacyConfigDir(): string {
  return join(configBase(), "neko");
}

export function localConfigPath(): string {
  return join(configDir(), "config.json");
}

export function readLocalConfig(): LocalConfig {
  for (const path of [localConfigPath(), join(legacyConfigDir(), "config.json")]) {
    try {
      const raw = readFileSync(path, "utf8");
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed === "object") {
        const cfg = parsed as LocalConfig;
        // Database passwords are encrypted at rest (enc:v1); legacy plaintext
        // passes through unchanged.
        if (typeof cfg.pg?.password === "string" && cfg.pg.password) {
          cfg.pg = {
            ...cfg.pg,
            password: maybeDecryptSecret(cfg.pg.password),
          };
        }
        if (typeof cfg.database_url === "string" && cfg.database_url) {
          cfg.database_url = maybeDecryptSecret(cfg.database_url);
        }
        return cfg;
      }
    } catch {
      // missing / malformed → try next, fall through to {}
    }
  }
  return {};
}

/**
 * Deep-merges `partial` into the existing config and writes it atomically.
 * The merge is intentionally shallow at the top level + one level deeper —
 * passing { pg: { password } } only touches pg.password, not the whole pg
 * subtree.
 */
export function writeLocalConfig(partial: LocalConfig): void {
  const current = readLocalConfig();
  const next: LocalConfig = {
    ...current,
    ...partial,
    pg: { ...(current.pg ?? {}), ...(partial.pg ?? {}) },
  };
  if (typeof next.pg?.password === "string" && next.pg.password) {
    next.pg = {
      ...next.pg,
      password: maybeEncryptSecret(next.pg.password),
    };
  }
  if (typeof next.database_url === "string" && next.database_url) {
    next.database_url = maybeEncryptSecret(next.database_url);
  }
  mkdirSync(configDir(), { recursive: true });
  const path = localConfigPath();
  const temporary = `${path}.${process.pid}.${randomUUID()}.tmp`;
  writeFileSync(temporary, JSON.stringify(next, null, 2), {
    encoding: "utf8",
    mode: 0o600,
    flag: "w",
  });
  renameSync(temporary, path);
  chmodSync(path, 0o600);
}

/**
 * True when the admin has changed the DB password from the default, or when
 * a database URL is set. With a URL the operator owns the credentials.
 */
export function hasCustomPassword(): boolean {
  if (process.env.OPENNEKO_DATABASE_URL_FILE?.trim()) return true;
  const cfg = readLocalConfig();
  if (cfg.database_url) return true;
  const password = cfg.pg?.password;
  return (
    typeof password === "string" &&
    password.length > 0 &&
    password !== "secret"
  );
}
