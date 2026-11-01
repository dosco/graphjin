/**
 * Postgres pool config builder.
 *
 * The connection details come from `~/.config/openneko/config.json`
 * (written by /setup), then `NEKO_PG_*` environment variables, then
 * hardcoded defaults that match what the docker compose stack ships with:
 *
 *   user      = "neko"
 *   password  = "secret"      ← initial / unchanged
 *   host      = "localhost"
 *   port      = 5432
 *   database  = "neko"
 *
 * On first boot the config file doesn't exist; the app connects with the
 * defaults. The /setup wizard's "Set DB password" step calls
 * `ALTER USER neko WITH PASSWORD '<new>'` and writes the new password to
 * the config file. From then on the file's value takes precedence.
 *
 * Production deploys can either pre-populate `~/.config/openneko/config.json`
 * or provide `NEKO_PG_*` directly. The local file wins so a password rotated
 * through /setup remains authoritative.
 *
 * A database URL replaces all of these: `OPENNEKO_DATABASE_URL_FILE` names a
 * file that holds it, or `database_url` sets it in the config file.
 *
 * `sslmode` follows libpq: `require` encrypts without checking the
 * certificate, `verify-ca` checks it against `sslrootcert`, and
 * `verify-full` also checks the host name.
 */

import { readFileSync } from "node:fs";
import { checkServerIdentity } from "node:tls";
import type { PoolConfig } from "pg";
import { readLocalConfig, type LocalPgConfig } from "./local-config";

const DEFAULT_PG = {
  host: "localhost",
  port: 5432,
  user: "neko",
  password: "secret",
  database: "neko",
} as const;

const SSL_MODES = new Set(["disable", "allow", "prefer", "require", "verify-ca", "verify-full"]);

export type DatabaseTarget = {
  host: string;
  port: number;
  user: string;
  password: string;
  database: string;
  sslmode?: string;
  sslrootcert?: string;
  sslcert?: string;
  sslkey?: string;
};

function nonEmptyEnv(name: string): string | undefined {
  const value = process.env[name]?.trim();
  return value ? value : undefined;
}

function envPort(): number | undefined {
  const value = nonEmptyEnv("NEKO_PG_PORT") ?? nonEmptyEnv("OPENNEKO_DB_PORT");
  if (!value) return undefined;
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 && parsed <= 65_535
    ? parsed
    : undefined;
}

/** The configured database URL, or undefined when the fields apply. */
export function configuredDatabaseUrl(
  cfg: { database_url?: string } = readLocalConfig(),
): string | undefined {
  const file = nonEmptyEnv("OPENNEKO_DATABASE_URL_FILE");
  if (file) {
    const url = readFileSync(file, "utf8").trim();
    if (!url) throw new Error(`OPENNEKO_DATABASE_URL_FILE (${file}) is empty`);
    return url;
  }
  const url = cfg.database_url?.trim();
  return url ? url : undefined;
}

export function parseDatabaseUrl(url: string): DatabaseTarget {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    throw new Error("The database URL is not a valid URL");
  }
  if (parsed.protocol !== "postgres:" && parsed.protocol !== "postgresql:") {
    throw new Error("The database URL must start with postgres:// or postgresql://");
  }
  const query = parsed.searchParams;
  const param = (name: string) => query.get(name)?.trim() || undefined;
  const host = param("host") ?? parsed.hostname.replace(/^\[(.*)\]$/, "$1");
  const port = parsed.port ? Number(parsed.port) : Number(param("port") ?? DEFAULT_PG.port);
  return {
    host: host || DEFAULT_PG.host,
    port,
    user: decodeURIComponent(parsed.username) || DEFAULT_PG.user,
    password: decodeURIComponent(parsed.password),
    database: decodeURIComponent(parsed.pathname.replace(/^\//, "")) || DEFAULT_PG.database,
    sslmode: param("sslmode"),
    sslrootcert: param("sslrootcert"),
    sslcert: param("sslcert"),
    sslkey: param("sslkey"),
  };
}

function targetFromFields(cfg: LocalPgConfig): DatabaseTarget {
  const environmentFirst = nonEmptyEnv("OPENNEKO_PG_ENV_OVERRIDE") === "1";
  const choose = <T>(environmentValue: T | undefined, localValue: T | undefined): T | undefined =>
    environmentFirst ? environmentValue ?? localValue : localValue ?? environmentValue;
  return {
    host: choose(nonEmptyEnv("NEKO_PG_HOST"), cfg.host) ?? DEFAULT_PG.host,
    port: choose(envPort(), cfg.port) ?? DEFAULT_PG.port,
    user: choose(nonEmptyEnv("NEKO_PG_USER"), cfg.user) ?? DEFAULT_PG.user,
    password:
      choose(nonEmptyEnv("NEKO_PG_PASSWORD"), cfg.password) ?? DEFAULT_PG.password,
    database: choose(nonEmptyEnv("NEKO_PG_DATABASE"), cfg.database) ?? DEFAULT_PG.database,
    sslmode: choose(nonEmptyEnv("NEKO_PG_SSLMODE"), cfg.sslmode),
    sslrootcert: choose(nonEmptyEnv("NEKO_PG_SSLROOTCERT"), cfg.sslrootcert),
  };
}

/** OpenNeko's database: the URL when one is configured, else the fields. */
export function resolveDatabaseTarget(): DatabaseTarget {
  const cfg = readLocalConfig();
  const url = configuredDatabaseUrl(cfg);
  return url ? parseDatabaseUrl(url) : targetFromFields(cfg.pg ?? {});
}

/** The URL form of a target, for a process that takes a connection string. */
export function formatDatabaseUrl(target: DatabaseTarget): string {
  const socket = target.host.startsWith("/");
  const host = socket ? "localhost" : target.host.includes(":") ? `[${target.host}]` : target.host;
  const params = new URLSearchParams();
  if (socket) params.set("host", target.host);
  for (const key of ["sslmode", "sslrootcert", "sslcert", "sslkey"] as const) {
    const value = target[key];
    if (value) params.set(key, value);
  }
  const query = params.size > 0 ? `?${params.toString()}` : "";
  const auth = `${encodeURIComponent(target.user)}:${encodeURIComponent(target.password)}`;
  return `postgres://${auth}@${host}:${target.port}/${encodeURIComponent(target.database)}${query}`;
}

export function sslConfig(target: DatabaseTarget): PoolConfig["ssl"] {
  const mode = target.sslmode ?? "disable";
  if (!SSL_MODES.has(mode)) throw new Error(`Unknown sslmode "${mode}"`);
  if (mode === "disable" || mode === "allow" || mode === "prefer") return undefined;
  const files = {
    ...(target.sslrootcert ? { ca: readFileSync(target.sslrootcert, "utf8") } : {}),
    ...(target.sslcert ? { cert: readFileSync(target.sslcert, "utf8") } : {}),
    ...(target.sslkey ? { key: readFileSync(target.sslkey, "utf8") } : {}),
  };
  if (mode === "require") return { ...files, rejectUnauthorized: false };
  if (mode === "verify-ca") {
    return { ...files, rejectUnauthorized: true, checkServerIdentity: () => undefined };
  }
  // node-pg sends no server name for an IP address, and Node then checks the
  // certificate against "localhost". Check it against the configured host.
  const host = target.host;
  return {
    ...files,
    rejectUnauthorized: true,
    checkServerIdentity: (_name: string, cert) => checkServerIdentity(host, cert),
  };
}

export function buildPoolConfig(overrides: { database?: string } = {}): PoolConfig {
  const target = resolveDatabaseTarget();
  const ssl = sslConfig(target);
  return {
    host: target.host,
    port: target.port,
    user: target.user,
    password: target.password,
    database: overrides.database ?? target.database,
    max: 10,
    ...(ssl ? { ssl } : {}),
  };
}
