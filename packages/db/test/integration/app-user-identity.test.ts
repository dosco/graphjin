/**
 * Migration 0061: app_user identity uniqueness.
 *
 * The concurrent first-sign-in race could persist duplicate rows for one
 * identity. The migration must repair existing duplicates (keeping the
 * oldest, neutralizing the newer in place — never deleting, rows may be
 * referenced) and leave unique indexes that make any future race lose
 * loudly.
 */

import { readFile } from "node:fs/promises";
import { join } from "node:path";
import pg from "pg";
import { afterAll, describe, expect, it } from "vitest";
import { dbReachable } from "./_helpers";
import { pool } from "../../src";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[app-user-identity] skipping: Postgres unreachable.");
}

const REPO_ROOT = join(__dirname, "..", "..", "..", "..");
const M_0061 = join(REPO_ROOT, "db", "migrations", "0061_app_user_identity_unique.sql");

const TEMP_DB = `vitest_appuser_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`;

afterAll(async () => {
  if (!reachable) return;
  await pool().query(`drop database if exists ${TEMP_DB} with (force)`);
});

describeIfDb("0061 app_user identity uniqueness", () => {
  it("repairs duplicates and blocks new ones", async () => {
    await pool().query(`create database ${TEMP_DB}`);
    const client = new pg.Client({
      host: process.env.NEKO_PG_HOST ?? "localhost",
      port: Number(process.env.NEKO_PG_PORT ?? 5432),
      user: process.env.NEKO_PG_USER ?? "neko",
      password: process.env.NEKO_PG_PASSWORD ?? "secret",
      database: TEMP_DB,
    });
    await client.connect();
    try {
      await client.query(`
        create table app_user (
          id text primary key,
          sub text,
          email text not null,
          name text,
          org_id text not null,
          role text not null,
          disabled_at timestamptz,
          created_at timestamptz not null default now(),
          updated_at timestamptz not null default now(),
          last_login_at timestamptz
        )`);
      // Race artifacts: two rows claimed the same sub, two the same mailbox
      // (case-insensitively).
      await client.query(`
        insert into app_user (id, sub, email, org_id, role, created_at) values
          ('u1', 'sub-1', 'alice@example.com',  'org', 'admin',  now() - interval '2 hours'),
          ('u2', 'sub-1', 'alice2@example.com', 'org', 'admin',  now() - interval '1 hour'),
          ('u3', null,    'Bob@Example.com',    'org', 'member', now() - interval '2 hours'),
          ('u4', null,    'bob@example.com',    'org', 'member', now() - interval '1 hour')`);

      await client.query(await readFile(M_0061, "utf8"));

      const u1 = (await client.query("select sub, disabled_at from app_user where id='u1'")).rows[0];
      const u2 = (await client.query("select sub, disabled_at from app_user where id='u2'")).rows[0];
      expect(u1.sub).toBe("sub-1");
      expect(u1.disabled_at).toBeNull();
      expect(u2.sub).toBeNull();
      expect(u2.disabled_at).not.toBeNull();

      const u3 = (await client.query("select email, disabled_at from app_user where id='u3'")).rows[0];
      const u4 = (await client.query("select email, disabled_at from app_user where id='u4'")).rows[0];
      expect(u3.email).toBe("Bob@Example.com");
      expect(u3.disabled_at).toBeNull();
      expect(u4.email).toBe("bob@example.com+duplicate-u4");
      expect(u4.disabled_at).not.toBeNull();

      // The indexes now block fresh duplicates loudly.
      await expect(
        client.query(
          "insert into app_user (id, sub, email, org_id, role) values ('u5','sub-1','carol@example.com','org','member')",
        ),
      ).rejects.toMatchObject({ code: "23505" });
      await expect(
        client.query(
          "insert into app_user (id, sub, email, org_id, role) values ('u6',null,'ALICE@example.com','org','member')",
        ),
      ).rejects.toMatchObject({ code: "23505" });

      // Re-running the migration is a no-op (idempotent for upgrades).
      await client.query(await readFile(M_0061, "utf8"));
    } finally {
      await client.end();
    }
  }, 30_000);
});
