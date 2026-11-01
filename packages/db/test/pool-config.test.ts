import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  buildPoolConfig,
  formatDatabaseUrl,
  hasCustomPassword,
  parseDatabaseUrl,
  pool,
  reconnectPool,
  writeLocalConfig,
} from "../src";

describe("metadata DB pool config", () => {
  let tempHome: string;
  const ORIGINAL_HOME = process.env.HOME;
  const ORIGINAL_XDG = process.env.XDG_CONFIG_HOME;
  const PG_ENV_KEYS = [
    "NEKO_PG_HOST",
    "NEKO_PG_PORT",
    "OPENNEKO_DB_PORT",
    "NEKO_PG_USER",
    "NEKO_PG_PASSWORD",
    "NEKO_PG_DATABASE",
    "NEKO_PG_SSLMODE",
    "OPENNEKO_PG_ENV_OVERRIDE",
    "OPENNEKO_DATABASE_URL_FILE",
    "NEKO_PG_SSLROOTCERT",
  ] as const;
  const ORIGINAL_PG_ENV = Object.fromEntries(
    PG_ENV_KEYS.map((key) => [key, process.env[key]]),
  );

  beforeEach(async () => {
    tempHome = await mkdtemp(join(tmpdir(), "neko-poolcfg-test-"));
    process.env.HOME = tempHome;
    delete process.env.XDG_CONFIG_HOME;
    for (const key of PG_ENV_KEYS) delete process.env[key];
    await reconnectPool();
  });

  afterEach(async () => {
    await reconnectPool();
    if (ORIGINAL_HOME) process.env.HOME = ORIGINAL_HOME;
    if (ORIGINAL_XDG === undefined) delete process.env.XDG_CONFIG_HOME;
    else process.env.XDG_CONFIG_HOME = ORIGINAL_XDG;
    for (const key of PG_ENV_KEYS) {
      const original = ORIGINAL_PG_ENV[key];
      if (original === undefined) delete process.env[key];
      else process.env[key] = original;
    }
    await rm(tempHome, { recursive: true, force: true });
  });

  it("uses environment config when no local config exists", () => {
    process.env.NEKO_PG_HOST = "metadata.internal";
    process.env.NEKO_PG_PORT = "55432";
    process.env.NEKO_PG_USER = "runtime-user";
    process.env.NEKO_PG_PASSWORD = "runtime-password";
    process.env.NEKO_PG_DATABASE = "runtime-db";
    process.env.NEKO_PG_SSLMODE = "require";

    expect(buildPoolConfig()).toMatchObject({
      host: "metadata.internal",
      port: 55432,
      user: "runtime-user",
      password: "runtime-password",
      database: "runtime-db",
      ssl: { rejectUnauthorized: false },
    });
  });

  it("prefers local config over environment config", () => {
    process.env.NEKO_PG_HOST = "metadata.internal";
    process.env.NEKO_PG_PORT = "55432";
    writeLocalConfig({ pg: { host: "localhost", port: 5433 } });

    expect(buildPoolConfig()).toMatchObject({
      host: "localhost",
      port: 5433,
    });
  });

  it("can prefer explicit host-development environment values", () => {
    process.env.NEKO_PG_HOST = "127.0.0.1";
    process.env.NEKO_PG_PORT = "56432";
    process.env.OPENNEKO_PG_ENV_OVERRIDE = "1";
    writeLocalConfig({ pg: { host: "neko-db", port: 5432 } });

    expect(buildPoolConfig()).toMatchObject({
      host: "127.0.0.1",
      port: 56432,
    });
  });

  it("reuses the pool while config is unchanged", () => {
    const first = pool();
    expect(pool()).toBe(first);
  });

  it("survives an idle connection error instead of killing the process", () => {
    const live = pool();
    expect(live.listenerCount("error")).toBeGreaterThan(0);
    // Without a listener node reports this as unhandled and exits.
    expect(() => live.emit("error", new Error("Connection terminated unexpectedly"))).not.toThrow();
  });

  it("rebuilds the pool when local Postgres config changes", () => {
    const first = pool();
    writeLocalConfig({ pg: { password: "changed-password" } });
    const second = pool();

    expect(second).not.toBe(first);
    expect(pool()).toBe(second);
  });

  it("uses database_url over the pg fields", () => {
    process.env.NEKO_PG_HOST = "neko-db";
    writeLocalConfig({
      database_url: "postgres://app%40corp:p%2Fw@db.example:6432/openneko?sslmode=require",
      pg: { host: "ignored" },
    });

    expect(buildPoolConfig()).toMatchObject({
      host: "db.example",
      port: 6432,
      user: "app@corp",
      password: "p/w",
      database: "openneko",
      ssl: { rejectUnauthorized: false },
    });
    expect(hasCustomPassword()).toBe(true);
  });

  it("reads the URL from OPENNEKO_DATABASE_URL_FILE first", async () => {
    writeLocalConfig({ database_url: "postgres://neko:config@config-host/neko" });
    const file = join(tempHome, "database-url");
    await writeFile(file, "postgres://neko:file@file-host:5433/neko\n");
    process.env.OPENNEKO_DATABASE_URL_FILE = file;

    expect(buildPoolConfig()).toMatchObject({ host: "file-host", port: 5433, password: "file" });
  });

  it("refuses an empty URL file", async () => {
    const file = join(tempHome, "database-url");
    await writeFile(file, "\n");
    process.env.OPENNEKO_DATABASE_URL_FILE = file;

    expect(() => buildPoolConfig()).toThrow(/is empty/);
  });

  it("checks the certificate for verify-ca and verify-full", async () => {
    const ca = join(tempHome, "ca.pem");
    await writeFile(ca, "CA-PEM");
    process.env.OPENNEKO_DATABASE_URL_FILE = join(tempHome, "database-url");

    await writeFile(process.env.OPENNEKO_DATABASE_URL_FILE, `postgres://neko:pw@db/neko?sslmode=verify-full&sslrootcert=${ca}`);
    const full = buildPoolConfig().ssl as {
      ca: string;
      rejectUnauthorized: boolean;
      checkServerIdentity: (name: string, cert: object) => Error | undefined;
    };
    expect(full).toMatchObject({ ca: "CA-PEM", rejectUnauthorized: true });
    const cert = { subject: { CN: "localhost" }, subjectaltname: "DNS:localhost" };
    expect(full.checkServerIdentity("localhost", cert)).toBeInstanceOf(Error);

    await writeFile(process.env.OPENNEKO_DATABASE_URL_FILE, `postgres://neko:pw@db/neko?sslmode=verify-ca&sslrootcert=${ca}`);
    const caOnly = buildPoolConfig().ssl as { rejectUnauthorized: boolean; checkServerIdentity: () => unknown };
    expect(caOnly.rejectUnauthorized).toBe(true);
    expect(caOnly.checkServerIdentity()).toBeUndefined();
  });

  it("applies verify-full to the pg fields too", () => {
    process.env.NEKO_PG_SSLMODE = "verify-full";
    expect(buildPoolConfig().ssl).toMatchObject({ rejectUnauthorized: true });
  });

  it("refuses an unknown sslmode and a non-postgres URL", () => {
    process.env.NEKO_PG_SSLMODE = "verify";
    expect(() => buildPoolConfig()).toThrow(/Unknown sslmode/);
    expect(() => parseDatabaseUrl("mysql://db/neko")).toThrow(/postgres:\/\//);
  });

  it("formats a target back into an equal URL", () => {
    for (const url of [
      "postgres://neko_subscriptions:p%40ss%3Aw@db.example:6432/neko?sslmode=verify-full&sslrootcert=%2Fetc%2Fca.pem",
      "postgres://neko:pw@[::1]:5432/neko",
      "postgres://neko:pw@localhost:5432/neko?host=%2Fvar%2Frun%2Fpostgresql",
    ]) {
      expect(parseDatabaseUrl(formatDatabaseUrl(parseDatabaseUrl(url)))).toEqual(parseDatabaseUrl(url));
    }
    expect(parseDatabaseUrl("postgres://neko:pw@[::1]:5432/neko").host).toBe("::1");
    expect(parseDatabaseUrl("postgres://neko:pw@localhost:5432/neko?host=%2Fvar%2Frun%2Fpostgresql").host).toBe("/var/run/postgresql");
  });
});
