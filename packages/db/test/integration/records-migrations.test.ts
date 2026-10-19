import pg from "pg";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  buildRecordsPoolConfig,
  discoverRecordsMigrations,
  runRecordsMigrations,
  type RecordsMigrationResult,
} from "../../src/records-migrate";

async function recordsDbReachable(): Promise<boolean> {
  const probe = new pg.Pool(buildRecordsPoolConfig());
  try {
    await probe.query("select 1");
    return true;
  } catch {
    return false;
  } finally {
    await probe.end();
  }
}

const reachable = await recordsDbReachable();
const describeIfRecordsDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[records-migrations] skipping: records Postgres unreachable.");
}

describeIfRecordsDb("records migration stream", () => {
  const database = `records_migrations_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`;
  let adminPool: pg.Pool;
  let testPool: pg.Pool;
  let initialResult: RecordsMigrationResult;

  beforeAll(async () => {
    adminPool = new pg.Pool(buildRecordsPoolConfig());
    await adminPool.query(`create database ${database}`);
    testPool = new pg.Pool(buildRecordsPoolConfig(process.env, { database }));
    initialResult = await runRecordsMigrations({ pool: testPool });
  });

  afterAll(async () => {
    if (testPool) await testPool.end();
    if (adminPool) {
      await adminPool.query(`drop database if exists ${database}`);
      await adminPool.end();
    }
  });

  it("installs the engine registry once and records its checksum", async () => {
    expect(initialResult).toEqual({
      applied: [
        "0001_engine_registry.sql",
        "0002_record_audit_trigger.sql",
        "0003_schema_action_names.sql",
        "0004_polymorphic_relationships.sql",
        "0005_import_batch_durability.sql",
        "0006_recycle_index.sql",
        "0007_identity_mapping.sql",
        "0009_change_log_owner_scope.sql",
        "0010_subject_entitlements.sql",
        "0011_record_documents.sql",
      ],
      current: "0011_record_documents.sql",
    });
    await expect(runRecordsMigrations({ pool: testPool })).resolves.toEqual({
      applied: [],
      current: "0011_record_documents.sql",
    });

    const tables = await testPool.query<{ table_name: string }>(
      `select table_name from information_schema.tables
       where table_schema = 'engine' order by table_name`,
    );
    expect(tables.rows.map((row) => row.table_name)).toEqual(
      expect.arrayContaining([
        "action_execution",
        "actor",
        "app_page",
        "app_schema_change",
        "app_schema_log",
        "record_app",
        "record_change_log",
        "record_field",
        "record_object",
        "record_permission",
        "recycle_record",
        "registry_version",
        "app_access_grant",
        "object_access_grant",
        "field_access_grant",
        "entitlement_revision",
        "entitlement_audit",
        "schema_migrations",
        "substrate_version",
      ]),
    );
    const auditVersion = await testPool.query<{ version: number }>(
      "select version from engine.substrate_version where name = 'record_audit_trigger'",
    );
    expect(auditVersion.rows).toEqual([{ version: 5 }]);
    const migration = await testPool.query<{ checksum_sha256: string }>(
      "select checksum_sha256 from engine.schema_migrations where name = '0001_engine_registry.sql'",
    );
    expect(migration.rows[0]?.checksum_sha256).toMatch(/^[0-9a-f]{64}$/);

    await testPool.query(
      `update engine.schema_migrations
       set checksum_sha256 = repeat('0', 64)
       where name = '0001_engine_registry.sql'`,
    );
    await expect(runRecordsMigrations({ pool: testPool })).rejects.toThrow(
      /checksum mismatch/,
    );
    const diskMigration = (await discoverRecordsMigrations())[0];
    await testPool.query(
      `update engine.schema_migrations set checksum_sha256 = $1
       where name = '0001_engine_registry.sql'`,
      [diskMigration?.checksumSha256],
    );
  });

  it("runs GraphJin's store SQL after the migrations, idempotently", async () => {
    const storeSql = `
      CREATE SCHEMA IF NOT EXISTS "_graphjin";
      CREATE TABLE IF NOT EXISTS "_graphjin"."watches" (id TEXT PRIMARY KEY);
      ALTER TABLE "_graphjin"."watches" ADD COLUMN IF NOT EXISTS lifecycle TEXT NOT NULL DEFAULT 'durable';
    `;
    const logs: string[] = [];
    for (let run = 0; run < 2; run++) {
      await expect(
        runRecordsMigrations({ pool: testPool, graphjinStoreSql: async () => storeSql, log: (m) => logs.push(m) }),
      ).resolves.toEqual({ applied: [], current: "0011_record_documents.sql" });
    }
    const columns = await testPool.query<{ column_name: string }>(
      `select column_name from information_schema.columns
       where table_schema = '_graphjin' and table_name = 'watches' order by column_name`,
    );
    expect(columns.rows.map((row) => row.column_name)).toEqual(["id", "lifecycle"]);
    expect(logs.filter((m) => m.includes("GraphJin store is current"))).toHaveLength(2);

    await expect(
      runRecordsMigrations({ pool: testPool, graphjinStoreSql: async () => "select broken(" }),
    ).rejects.toThrow();
  });

  it("enforces org consistency through composite foreign keys", async () => {
    await testPool.query(`
      insert into engine.record_app (org_id, app_id, label)
      values ('org-one', 'crm', 'CRM'), ('org-two', 'crm', 'CRM');
      insert into engine.record_object
        (id, org_id, app_id, api_name, label, plural_label, table_name)
      values
        ('00000000-0000-0000-0000-000000000201', 'org-one', 'crm',
         'account', 'Account', 'Accounts', 'crm__account');
    `);
    await expect(
      testPool.query(`
        insert into engine.record_field
          (org_id, object_id, api_name, label, kind, column_name)
        values
          ('org-two', '00000000-0000-0000-0000-000000000201',
           'name', 'Name', 'text', 'name')
      `),
    ).rejects.toThrow(/foreign key/i);
  });

  it("backfills legacy role permissions and keeps revision triggers valid on revoke", async () => {
    const upgradeDatabase = `${database}_upgrade`;
    let upgradePool: pg.Pool | undefined;
    try {
      await adminPool.query(`create database ${upgradeDatabase}`);
      upgradePool = new pg.Pool(
        buildRecordsPoolConfig(process.env, { database: upgradeDatabase }),
      );
      const migrations = await discoverRecordsMigrations();
      await upgradePool.query("create schema if not exists engine");
      await upgradePool.query(`
        create table engine.schema_migrations (
          name text primary key,
          checksum_sha256 text not null,
          applied_at timestamptz not null default now()
        )
      `);
      for (const migration of migrations.filter((m) => m.name < "0010_subject_entitlements.sql")) {
        await upgradePool.query(migration.sql);
        await upgradePool.query(
          `insert into engine.schema_migrations (name, checksum_sha256)
           values ($1, $2)`,
          [migration.name, migration.checksumSha256],
        );
      }
      await upgradePool.query(`
        insert into engine.record_app (org_id, app_id, label)
        values ('org-upgrade', 'crm', 'CRM');
        insert into engine.record_object
          (id, org_id, app_id, api_name, label, plural_label, table_name)
        values
          ('00000000-0000-0000-0000-000000000301', 'org-upgrade', 'crm',
           'activity', 'Activity', 'Activities', 'crm__activity');
        insert into engine.record_field
          (id, org_id, object_id, api_name, label, kind, column_name)
        values
          ('00000000-0000-0000-0000-000000000302', 'org-upgrade',
           '00000000-0000-0000-0000-000000000301',
           'subject', 'Subject', 'text', 'subject');
        insert into engine.record_permission
          (org_id, app_id, role, object_api_name,
           can_read, can_create, can_update, can_delete)
        values
          ('org-upgrade', 'crm', 'member', 'activity',
           false, true, false, false);
      `);

      await expect(runRecordsMigrations({ pool: upgradePool })).resolves.toEqual({
        applied: ["0010_subject_entitlements.sql", "0011_record_documents.sql"],
        current: "0011_record_documents.sql",
      });
      const objectGrant = await upgradePool.query(
        `select can_read, can_create, can_update, can_delete
         from engine.object_access_grant
         where org_id = 'org-upgrade' and app_id = 'crm'
           and subject_type = 'role' and subject_id = 'member'
           and object_api_name = 'activity'`,
      );
      expect(objectGrant.rows).toEqual([
        {
          can_read: true,
          can_create: true,
          can_update: false,
          can_delete: false,
        },
      ]);
      const fieldGrant = await upgradePool.query(
        `select can_read, can_write from engine.field_access_grant
         where org_id = 'org-upgrade' and app_id = 'crm'
           and subject_type = 'role' and subject_id = 'member'`,
      );
      expect(fieldGrant.rows).toEqual([{ can_read: true, can_write: true }]);

      const before = await upgradePool.query<{ revision: string }>(
        `select revision::text as revision from engine.entitlement_revision
         where org_id = 'org-upgrade'`,
      );
      await upgradePool.query(
        `delete from engine.app_access_grant
         where org_id = 'org-upgrade' and app_id = 'crm'
           and subject_type = 'role' and subject_id = 'member'`,
      );
      const after = await upgradePool.query<{ revision: string }>(
        `select revision::text as revision from engine.entitlement_revision
         where org_id = 'org-upgrade'`,
      );
      expect(BigInt(after.rows[0]?.revision ?? "0")).toBeGreaterThan(
        BigInt(before.rows[0]?.revision ?? "0"),
      );
    } finally {
      if (upgradePool) await upgradePool.end();
      await adminPool.query(`drop database if exists ${upgradeDatabase}`);
    }
  });
});
