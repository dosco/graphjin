/**
 * getOrgId() bootstrap race, against a real Postgres.
 *
 * On an empty organization table, web, worker, the graphjin-secret-init
 * one-shot, and the demo seed can all resolve the org concurrently, and
 * nothing in the schema forbids a second row. Creation must therefore be
 * serialized (advisory lock) and reads made deterministic (stable ORDER BY).
 */

import pg from "pg";
import { afterAll, describe, expect, it } from "vitest";
import { dbReachable } from "./_helpers";
import { pool } from "../../src";
import { getOrgId, _resetOrgIdCacheForTesting } from "../../src/org";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[org-bootstrap] skipping: Postgres unreachable.");
}

const TEMP_DB = `vitest_org_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`;
const savedDatabase = process.env.NEKO_PG_DATABASE;

afterAll(async () => {
  if (!reachable) return;
  if (savedDatabase === undefined) delete process.env.NEKO_PG_DATABASE;
  else process.env.NEKO_PG_DATABASE = savedDatabase;
  _resetOrgIdCacheForTesting();
  // Touch the pool so the signature change retires the temp-db pool before
  // the force-drop terminates its backends (avoids a stray 'error' event).
  await pool().query("select 1");
  await new Promise((resolve) => setTimeout(resolve, 100));
  await pool().query(`drop database if exists ${TEMP_DB} with (force)`);
});

describeIfDb("getOrgId bootstrap", () => {
  it("many concurrent first calls create exactly one organization", async () => {
    await pool().query(`create database ${TEMP_DB}`);
    const admin = new pg.Client({
      host: process.env.NEKO_PG_HOST ?? "localhost",
      port: Number(process.env.NEKO_PG_PORT ?? 5432),
      user: process.env.NEKO_PG_USER ?? "neko",
      password: process.env.NEKO_PG_PASSWORD ?? "secret",
      database: TEMP_DB,
    });
    await admin.connect();
    await admin.query(`
      create table organization (
        id text primary key,
        scalekit_org_id text,
        name text not null,
        domain text,
        status text not null default 'active',
        setup_complete_at timestamptz,
        solo_admin_user_id text,
        created_at timestamptz not null default now(),
        updated_at timestamptz not null default now()
      )`);

    // Point the shared pool at the temp database; db() rebuilds on config
    // signature change. Reset the cache so every call takes the cold path.
    process.env.NEKO_PG_DATABASE = TEMP_DB;
    _resetOrgIdCacheForTesting();

    const ids = await Promise.all(
      Array.from({ length: 12 }, async () => {
        // Each caller behaves like a separate cold process.
        _resetOrgIdCacheForTesting();
        return getOrgId();
      }),
    );

    const rows = await admin.query("select id, name from organization");
    await admin.end();

    expect(rows.rowCount).toBe(1);
    expect(new Set(ids).size).toBe(1);
    expect(ids[0]).toBe(rows.rows[0].id);
    expect(rows.rows[0].name).toBe("My Workspace");
  }, 30_000);

  it("returns the oldest org when two rows exist (deterministic tie-break)", async () => {
    const admin = new pg.Client({
      host: process.env.NEKO_PG_HOST ?? "localhost",
      port: Number(process.env.NEKO_PG_PORT ?? 5432),
      user: process.env.NEKO_PG_USER ?? "neko",
      password: process.env.NEKO_PG_PASSWORD ?? "secret",
      database: TEMP_DB,
    });
    await admin.connect();
    await admin.query(`
      insert into organization (id, name, created_at)
      values ('older-org', 'Older', now() - interval '1 hour')`);
    await admin.end();

    for (let i = 0; i < 3; i++) {
      _resetOrgIdCacheForTesting();
      expect(await getOrgId()).toBe("older-org");
    }
  });
});
