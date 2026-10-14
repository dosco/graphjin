import pg from "pg";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  buildRecordsPoolConfig,
  runRecordsMigrations,
} from "@neko/db/records-migrate";
import { validateRecordIdentifier } from "../src/naming";
import { ensureRecordsAuditTrigger } from "../src/schema/audit";
import { loadRecordsCatalogSnapshot } from "../src/schema/catalog";

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
  console.warn("[records-schema-substrate] skipping: records Postgres unreachable.");
}

describeIfRecordsDb("records schema substrate integration", () => {
  const database = `records_substrate_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`;
  let adminPool: pg.Pool;
  let testPool: pg.Pool;

  beforeAll(async () => {
    adminPool = new pg.Pool(buildRecordsPoolConfig());
    await adminPool.query(`create database ${database}`);
    testPool = new pg.Pool(buildRecordsPoolConfig(process.env, { database }));
    await runRecordsMigrations({ pool: testPool });
    await testPool.query(`
      insert into engine.record_app (org_id, app_id, label, status)
      values ('org-a', 'equipment', 'Equipment', 'provisioning');
      insert into engine.record_object
        (id, org_id, app_id, api_name, label, plural_label, table_name,
         name_field, visibility)
      values
        ('00000000-0000-0000-0000-000000000101', 'org-a', 'equipment',
         'loan', 'Loan', 'Loans', 'equipment__loan', 'name', 'owner');

      create table public.equipment__loan (
        id text primary key,
        org_id text not null,
        owner_user_id text not null,
        data jsonb not null,
        nk_created_at timestamptz not null,
        nk_created_by text not null,
        nk_updated_at timestamptz not null,
        nk_updated_by text not null,
        nk_action_request_id text not null,
        nk_mutation_id text not null,
        nk_deleted_at timestamptz
      );
    `);
  });

  afterAll(async () => {
    if (testPool) await testPool.end();
    if (adminPool) {
      await adminPool.query(`drop database if exists ${database}`);
      await adminPool.end();
    }
  });

  it("changes the catalog revision on physical drift and stays stable otherwise", async () => {
    const initial = await loadRecordsCatalogSnapshot(testPool);
    const repeated = await loadRecordsCatalogSnapshot(testPool);
    expect(repeated.revision).toBe(initial.revision);
    expect(initial.revision).toMatch(/^[0-9a-f]{64}$/);

    await testPool.query("create index idx_equipment_test on public.equipment__loan ((data->>'name'))");
    const drifted = await loadRecordsCatalogSnapshot(testPool);
    expect(drifted.revision).not.toBe(initial.revision);
    expect(drifted.indexes).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ table: "equipment__loan", name: "idx_equipment_test" }),
      ]),
    );
  });

  it("captures create, update, and soft-delete history in the row transaction", async () => {
    const triggerInput = {
      tableSchema: validateRecordIdentifier("public"),
      tableName: validateRecordIdentifier("equipment__loan"),
      appId: "equipment",
      objectApiName: validateRecordIdentifier("loan"),
    };
    await expect(ensureRecordsAuditTrigger(testPool, triggerInput)).resolves.toBe("created");
    await expect(ensureRecordsAuditTrigger(testPool, triggerInput)).resolves.toBe("current");

    await expect(
      testPool.query(`
        insert into public.equipment__loan (
          id, org_id, owner_user_id, data,
          nk_created_at, nk_created_by, nk_updated_at, nk_updated_by,
          nk_action_request_id, nk_mutation_id
        ) values (
          'unclaimed', 'org-a', 'member-1', '{"name": "Must Fail"}',
          now(), 'member-1', now(), 'member-1',
          'missing-request', 'missing-mutation'
        )
      `),
    ).rejects.toThrow(/live action execution claim/);

    await testPool.query(`
      insert into engine.action_execution
        (action_request_id, org_id, app_id, action_kind, status)
      values
        ('request-create', 'org-a', 'equipment', 'record_create', 'running'),
        ('request-update', 'org-a', 'equipment', 'record_update', 'running'),
        ('request-delete', 'org-a', 'equipment', 'record_delete', 'running'),
        ('request-restore', 'org-a', 'equipment', 'record_restore', 'running');

      insert into public.equipment__loan (
        id, org_id, owner_user_id, data,
        nk_created_at, nk_created_by, nk_updated_at, nk_updated_by,
        nk_action_request_id, nk_mutation_id
      ) values (
        'loan-1', 'org-a', 'member-1', '{"name": "Laptop"}',
        now(), 'member-1', now(), 'member-1',
        'request-create', 'mutation-create'
      );

      update public.equipment__loan
      set data = data || '{"name": "Laptop Pro"}',
          nk_updated_at = now(),
          nk_updated_by = 'member-1',
          nk_action_request_id = 'request-update',
          nk_mutation_id = 'mutation-update'
      where id = 'loan-1';

      update public.equipment__loan
      set nk_deleted_at = now(),
          nk_updated_at = now(),
          nk_updated_by = 'member-1',
          nk_action_request_id = 'request-delete',
          nk_mutation_id = 'mutation-delete'
      where id = 'loan-1';
    `);

    const history = await testPool.query<{
      action: string;
      actor_user_id: string;
      action_request_id: string;
      mutation_id: string;
      owner_user_id: string;
      changes: Record<string, { old: unknown; new: unknown }>;
    }>(`
      select action, actor_user_id, action_request_id, mutation_id, owner_user_id, changes
      from engine.record_change_log
      where record_id = 'loan-1'
      order by id
    `);
    expect(history.rows.map((row) => row.action)).toEqual(["create", "update", "delete"]);
    expect(history.rows.map((row) => row.owner_user_id)).toEqual([
      "member-1",
      "member-1",
      "member-1",
    ]);
    expect(history.rows[0]).toMatchObject({
      actor_user_id: "member-1",
      action_request_id: "request-create",
      mutation_id: "mutation-create",
      changes: { name: { old: null, new: "Laptop" } },
    });
    expect(history.rows[1]?.changes).toMatchObject({
      name: { old: "Laptop", new: "Laptop Pro" },
    });
    expect(history.rows[2]?.changes).toEqual({});

    const recycled = await testPool.query<{
      app_id: string;
      object_api_name: string;
      visibility: string;
      record_id: string;
      record_name: string;
      owner_user_id: string;
      deleted_by: string;
      deletion_action_request_id: string;
    }>(`
      select app_id, object_api_name, visibility, record_id, record_name, owner_user_id,
             deleted_by, deletion_action_request_id
      from engine.recycle_record
      where org_id = 'org-a' and record_id = 'loan-1'
    `);
    expect(recycled.rows).toEqual([
      {
        app_id: "equipment",
        object_api_name: "loan",
        visibility: "owner",
        record_id: "loan-1",
        record_name: "Laptop Pro",
        owner_user_id: "member-1",
        deleted_by: "member-1",
        deletion_action_request_id: "request-delete",
      },
    ]);

    await testPool.query(`
      update public.equipment__loan
      set nk_deleted_at = null,
          nk_updated_at = now(),
          nk_updated_by = 'member-1',
          nk_action_request_id = 'request-restore',
          nk_mutation_id = 'mutation-restore'
      where id = 'loan-1';
    `);
    await expect(
      testPool.query(
        "select record_id from engine.recycle_record where org_id = 'org-a' and record_id = 'loan-1'",
      ),
    ).resolves.toMatchObject({ rows: [] });
  });
});
