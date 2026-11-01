import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";
import pg from "pg";
import { afterAll, describe, expect, it } from "vitest";
import { buildPoolConfig } from "../../src/connection";
import { pool } from "../../src";
import { dbReachable } from "./_helpers";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

const MIGRATIONS = join(__dirname, "..", "..", "..", "..", "db", "migrations");
const TARGET = "0074_user_groups.sql";

async function withTempDb<T>(fn: (client: pg.Client) => Promise<T>): Promise<T> {
  const name = `vitest_groups_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`;
  const admin = await pool().connect();
  try {
    await admin.query(`create database ${name}`);
  } finally {
    admin.release();
  }
  const client = new pg.Client(buildPoolConfig({ database: name }));
  await client.connect();
  try {
    return await fn(client);
  } finally {
    await client.end();
    const cleanup = await pool().connect();
    try {
      await cleanup.query(`drop database if exists ${name}`);
    } finally {
      cleanup.release();
    }
  }
}

async function applyMigrations(client: pg.Client, filter: (file: string) => boolean) {
  const files = (await readdir(MIGRATIONS)).filter((f) => f.endsWith(".sql") && filter(f)).sort();
  for (const file of files) {
    await client.query(await readFile(join(MIGRATIONS, file), "utf8"));
  }
}

async function adminIds(client: pg.Client, orgId: string): Promise<string[]> {
  const { rows } = await client.query<{ user_id: string }>(
    `select distinct m.user_id from user_group_membership m
     join user_group g on g.id = m.group_id
     where m.org_id = $1 and g.slug = 'administrators' order by m.user_id`,
    [orgId],
  );
  return rows.map((r) => r.user_id);
}

async function roleOf(client: pg.Client, userId: string): Promise<string | undefined> {
  const { rows } = await client.query<{ role: string }>("select role from app_user where id = $1", [userId]);
  return rows[0]?.role;
}

async function groupId(client: pg.Client, orgId: string, slug: string): Promise<string> {
  const { rows } = await client.query<{ id: string }>(
    "select id from user_group where org_id = $1 and slug = $2",
    [orgId, slug],
  );
  return rows[0]!.id;
}

describeIfDb("0074 user groups", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("upgrades solo, SSO and empty installations without changing who is an administrator", async () => {
    await withTempDb(async (client) => {
      await applyMigrations(client, (f) => f < TARGET);
      await client.query(`
        insert into organization (id, name) values ('solo', 'Solo'), ('sso', 'SSO'), ('empty', 'Empty');
        insert into app_user (id, org_id, role, email, name) values
          ('solo-owner', 'solo', 'admin', 'usr_x@solo.openneko.invalid', 'Solo administrator');
        update organization set solo_admin_user_id = 'solo-owner' where id = 'solo';
        insert into app_user (id, org_id, role, email, sub, disabled_at) values
          ('sso-admin', 'sso', 'admin', 'admin@example.test', 'sub-admin', null),
          ('sso-member', 'sso', 'member', 'member@example.test', 'sub-member', null),
          ('sso-disabled-admin', 'sso', 'admin', 'old@example.test', 'sub-old', now());
      `);

      await applyMigrations(client, (f) => f === TARGET);
      await applyMigrations(client, (f) => f === TARGET);

      const { rows: groups } = await client.query<{ org_id: string; slug: string; kind: string }>(
        "select org_id, slug, kind from user_group order by org_id, slug",
      );
      expect(groups).toEqual([
        { org_id: "empty", slug: "administrators", kind: "builtin" },
        { org_id: "empty", slug: "everyone", kind: "builtin" },
        { org_id: "solo", slug: "administrators", kind: "builtin" },
        { org_id: "solo", slug: "everyone", kind: "builtin" },
        { org_id: "sso", slug: "administrators", kind: "builtin" },
        { org_id: "sso", slug: "everyone", kind: "builtin" },
      ]);
      expect(await adminIds(client, "solo")).toEqual(["solo-owner"]);
      expect(await adminIds(client, "sso")).toEqual(["sso-admin", "sso-disabled-admin"]);
      expect(await adminIds(client, "empty")).toEqual([]);
      const { rows: sources } = await client.query<{ source: string }>("select distinct source from user_group_membership");
      expect(sources).toEqual([{ source: "local" }]);
    });
  });

  it("keeps the solo owner an administrator when a sign-in plugin attaches their identity", async () => {
    await withTempDb(async (client) => {
      await applyMigrations(client, (f) => f <= TARGET);
      await client.query(`
        insert into organization (id, name) values ('org', 'Org');
        insert into app_user (id, org_id, role, email) values ('owner', 'org', 'admin', 'usr_y@solo.openneko.invalid');
        update organization set solo_admin_user_id = 'owner' where id = 'org';
      `);
      expect(await adminIds(client, "org")).toEqual(["owner"]);

      // SSO setup collects the real mailbox, then the first sign-in attaches the subject.
      await client.query("update app_user set email = 'owner@example.test' where id = 'owner'");
      await client.query("update app_user set sub = 'idp-owner', last_login_at = now() where id = 'owner'");
      // Users that arrive from the plugin are members until an administrator says otherwise.
      await client.query(`insert into app_user (id, org_id, role, email, sub) values
        ('new-1', 'org', 'member', 'a@example.test', 'idp-a'),
        ('new-2', 'org', 'member', 'b@example.test', 'idp-b')`);

      expect(await adminIds(client, "org")).toEqual(["owner"]);
      expect(await roleOf(client, "owner")).toBe("admin");
    });
  });

  it("mirrors role writes into membership and membership writes into role", async () => {
    await withTempDb(async (client) => {
      await applyMigrations(client, (f) => f <= TARGET);
      await client.query(`
        insert into organization (id, name) values ('org', 'Org');
        insert into app_user (id, org_id, role, email) values
          ('admin', 'org', 'admin', 'admin@example.test'),
          ('member', 'org', 'member', 'member@example.test');
      `);
      const admins = await groupId(client, "org", "administrators");
      const everyone = await groupId(client, "org", "everyone");
      expect(await adminIds(client, "org")).toEqual(["admin"]);

      await client.query("update app_user set role = 'admin' where id = 'member'");
      expect(await adminIds(client, "org")).toEqual(["admin", "member"]);
      await client.query("update app_user set role = 'member' where id = 'member'");
      expect(await adminIds(client, "org")).toEqual(["admin"]);

      await client.query(
        "insert into user_group_membership (org_id, group_id, user_id) values ('org', $1, 'member')",
        [admins],
      );
      expect(await roleOf(client, "member")).toBe("admin");

      const rule = "rule:00000000-0000-0000-0000-000000000001";
      await client.query(
        "insert into user_group_membership (org_id, group_id, user_id, source) values ('org', $1, 'member', $2)",
        [admins, rule],
      );
      await client.query(
        "delete from user_group_membership where group_id = $1 and user_id = 'member' and source = 'local'",
        [admins],
      );
      expect(await roleOf(client, "member")).toBe("admin");
      await client.query("delete from user_group_membership where group_id = $1 and user_id = 'member'", [admins]);
      expect(await roleOf(client, "member")).toBe("member");

      await client.query(
        "insert into user_group_membership (org_id, group_id, user_id, source) values ('org', $1, 'member', $2)",
        [admins, rule],
      );
      const { rows: ruleOnly } = await client.query(
        "select source from user_group_membership where group_id = $1 and user_id = 'member'",
        [admins],
      );
      expect(ruleOnly).toEqual([{ source: rule }]);
      await client.query("update app_user set role = 'member' where id = 'member'");
      expect(await adminIds(client, "org")).toEqual(["admin"]);

      await client.query(
        "insert into user_group_membership (org_id, group_id, user_id) values ('org', $1, 'member')",
        [everyone],
      );
      expect(await roleOf(client, "member")).toBe("member");

      await client.query("delete from app_user where id = 'admin'");
      await client.query("delete from organization where id = 'org'");
      const { rows } = await client.query("select 1 from user_group_membership");
      expect(rows).toEqual([]);
    });
  });

  it("0076 gives Everyone every item type on upgrade and for new organizations", async () => {
    await withTempDb(async (client) => {
      await applyMigrations(client, (f) => f < "0076_item_grants.sql");
      await client.query("insert into organization (id, name) values ('old', 'Old')");
      await applyMigrations(client, (f) => f === "0076_item_grants.sql");
      await applyMigrations(client, (f) => f === "0076_item_grants.sql");
      await client.query("insert into organization (id, name) values ('new', 'New')");
      const { rows } = await client.query<{ org_id: string; n: number; wildcard: boolean }>(`
        select ig.org_id, count(*)::int as n, bool_and(ig.item_id = '*') as wildcard
        from item_grant ig join user_group g on g.id = ig.group_id and g.slug = 'everyone'
        group by ig.org_id order by ig.org_id`);
      expect(rows).toEqual([
        { org_id: "new", n: 14, wildcard: true },
        { org_id: "old", n: 14, wildcard: true },
      ]);
      await client.query("delete from organization where id = 'old'");
    });
  });

  it("0078 moves admin approvers to Administrators and admin mappings to IdP rules", async () => {
    await withTempDb(async (client) => {
      await applyMigrations(client, (f) => f < "0078_approver_groups.sql");
      await client.query(`
        insert into organization (id, name) values ('org', 'Org');
        insert into action_policy (org_id, name, mode, approver_role) values ('org', 'admins', 'approval_required', 'admin'), ('org', 'anyone', 'approval_required', null);
        insert into sso_group (org_id, provider, tenant_id, external_id, display_name) values ('org', 'scalekit', 't', 'g-admins', 'Admins'), ('org', 'scalekit', 't', 'g-staff', 'Staff');
        insert into sso_group_mapping (org_id, provider, group_external_id, role) values ('org', 'scalekit', 'g-admins', 'admin'), ('org', 'scalekit', 'g-staff', 'member');
      `);
      await applyMigrations(client, (f) => f === "0078_approver_groups.sql");
      await applyMigrations(client, (f) => f === "0078_approver_groups.sql");
      const admins = await groupId(client, "org", "administrators");
      const { rows: policies } = await client.query("select name, approver_group_id from action_policy order by name");
      expect(policies).toEqual([{ name: "admins", approver_group_id: admins }, { name: "anyone", approver_group_id: null }]);
      await client.query("insert into action_policy (org_id, name, mode, approver_role) values ('org', 'later', 'approval_required', 'admin')");
      expect((await client.query("select approver_group_id from action_policy where name = 'later'")).rows[0].approver_group_id).toBe(admins);
      const { rows: rules } = await client.query(
        "select s.external_id, r.user_group_id from idp_group_rule r join sso_group s on s.id = r.sso_group_id",
      );
      expect(rules).toEqual([{ external_id: "g-admins", user_group_id: admins }]);
    });
  });

  it("seeds built-in groups for a new organization and rejects bad rows", async () => {
    await withTempDb(async (client) => {
      await applyMigrations(client, (f) => f <= TARGET);
      await client.query("insert into organization (id, name) values ('fresh', 'Fresh')");
      await client.query("insert into app_user (id, org_id, role, email) values ('first', 'fresh', 'admin', 'f@example.test')");
      expect(await adminIds(client, "fresh")).toEqual(["first"]);

      const admins = await groupId(client, "fresh", "administrators");
      await expect(client.query(
        "insert into user_group_membership (org_id, group_id, user_id, source) values ('fresh', $1, 'first', 'scim')",
        [admins],
      )).rejects.toThrow(/check/);
      await expect(client.query(
        "insert into user_group (org_id, slug, name) values ('fresh', 'Bad Slug', 'Bad')",
      )).rejects.toThrow(/check/);
      await client.query("insert into organization (id, name) values ('other', 'Other')");
      await expect(client.query(
        "insert into user_group_membership (org_id, group_id, user_id) values ('other', $1, 'first')",
        [admins],
      )).rejects.toThrow(/foreign key/);
    });
  });
});
