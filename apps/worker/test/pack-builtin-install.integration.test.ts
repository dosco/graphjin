import { randomUUID } from "node:crypto";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import pg from "pg";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { buildPoolConfig, pool } from "@neko/db";

const state = vi.hoisted(() => ({ pool: null as pg.Pool | null, secrets: {} as Record<string, Record<string, string>> }));
vi.mock("@neko/secrets", () => ({ readSecretsStore: async () => state.secrets, writeSecretsStore: async (value: typeof state.secrets) => { state.secrets = value; } }));
vi.mock("@neko/llm/work", () => ({ ensureOrgWorkspace: async () => ({ skillsRoot: "/tmp/unused-pack-skills" }) }));
import { PackService } from "../src/packs/service.js";

const config = buildPoolConfig();
const admin = new pg.Pool(config);
let reachable = true;
try { await admin.query("select 1"); } catch { reachable = false; await admin.end(); }
const schema = `builtin_test_${randomUUID().replaceAll("-", "")}`;
const root = resolve("../..");

describe.skipIf(!reachable)("built-in pack installation before configuration", () => {
  let service: PackService;
  beforeAll(async () => {
    await admin.query(`create schema ${schema}`);
    vi.stubEnv("PGOPTIONS", `-c search_path=${schema},public`);
    state.pool = pool();
    await state.pool.query("create table organization(id text primary key); create table app_user(id text primary key); create table watcher(id uuid primary key)");
    for (const name of ["0059_solution_packs.sql", "0072_pack_user_connections.sql"]) await state.pool.query(await readFile(resolve(root, "db/migrations", name), "utf8"));
    await state.pool.query("insert into organization values('builtin-test')");
    service = new PackService("builtin-test", resolve(root, "packs"));
  });
  afterAll(async () => { await state.pool?.end(); await admin.query(`drop schema ${schema} cascade`); await admin.end(); vi.unstubAllEnvs(); });
  it("uses the same credential-free install for Magento and Google, and safely removes an unconfigured pack", async () => {
    await expect(service.configureOAuth("google-workspace", "workspace", { clientId: "id", clientSecret: "secret" })).rejects.toThrow(/Install this pack/);
    for (const packId of ["magento", "google-workspace"]) {
      const result = await service.install(packId, { deferConfiguration: true });
      expect(result).toMatchObject({ status: "installed", configuration: { required: true }, readiness: { setup: { status: "blocked" } } });
      expect(await service.install(packId, { deferConfiguration: true })).toMatchObject({ status: "installed" });
    }
    expect((await state.pool!.query("select count(*) from pack_operation")).rows[0].count).toBe("2");
    expect((await state.pool!.query("select count(*) from pack_artifact")).rows[0].count).toBe("0");
    expect(state.secrets).toEqual({});
    await expect(service.configureOAuth("google-workspace", "workspace", { clientId: "id", clientSecret: "secret" })).resolves.toMatchObject({ configured: true });
    expect((await service.uninstall("magento")).status).toBe("removed");
    expect((await service.install("magento", { deferConfiguration: true })).configuration?.required).toBe(true);
  });
});
