import pg from "pg";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  buildRecordsPoolConfig,
  runRecordsMigrations,
} from "@neko/db/records-migrate";
import {
  claimRecordsActionExecution,
  setRecordsActionExecutionContext,
} from "../src/actions/execution";
import { buildRecordImportPlan } from "../src/import/plan";
import {
  createRecordImportRun,
  getRecordImportReceipt,
  summarizeRecordImportRun,
} from "../src/import/store";
import { recordIdentifier } from "../src/naming";
import { ensureRecordsAuditTrigger } from "../src/schema/audit";
import type { AppRegistrySnapshot } from "../src/types";

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
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[records-import-store] skipping: records Postgres unreachable");
}

describeIfDb("records import target-side receipts", () => {
  const database = `records_import_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`;
  const objectId = "00000000-0000-0000-0000-000000000951";
  let adminPool: pg.Pool;
  let pool: pg.Pool;

  beforeAll(async () => {
    adminPool = new pg.Pool(buildRecordsPoolConfig());
    await adminPool.query(`create database ${database}`);
    pool = new pg.Pool(buildRecordsPoolConfig(process.env, { database }));
    await runRecordsMigrations({ pool });
    await pool.query(`
      insert into engine.registry_version (org_id, revision) values ('org-a', 1);
      insert into engine.record_app
        (org_id, app_id, label, status, registry_revision)
      values ('org-a', 'equipment', 'Equipment', 'active', 1);
      insert into engine.record_object
        (id, org_id, app_id, api_name, label, plural_label, table_name,
         name_field, visibility)
      values
        ('${objectId}', 'org-a', 'equipment', 'loan', 'Loan', 'Loans',
         'equipment__loan', 'name', 'org');
      insert into engine.record_field
        (id, org_id, object_id, api_name, label, kind, column_name,
         required, read_only)
      values
        ('00000000-0000-0000-0000-000000000952', 'org-a', '${objectId}',
         'name', 'Name', 'text', 'name', true, false);
      create table public.equipment__loan (
        id text primary key,
        org_id text not null,
        data jsonb not null default '{}',
        nk_created_at timestamptz not null default now(),
        nk_created_by text not null,
        nk_updated_at timestamptz not null default now(),
        nk_updated_by text not null,
        nk_action_request_id text not null,
        nk_mutation_id text not null,
        nk_deleted_at timestamptz
      );
    `);
    await ensureRecordsAuditTrigger(pool, {
      tableSchema: recordIdentifier("public"),
      tableName: recordIdentifier("equipment__loan"),
      appId: "equipment",
      objectApiName: recordIdentifier("loan"),
    });
  });

  afterAll(async () => {
    if (pool) await pool.end();
    if (adminPool) {
      await adminPool.query(`drop database if exists ${database}`);
      await adminPool.end();
    }
  });

  it("commits and rolls back each batch receipt with its app rows", async () => {
    const snapshot: AppRegistrySnapshot = {
      revision: "1",
      app: {
        orgId: "org-a",
        appId: "equipment",
        label: "Equipment",
        purpose: null,
        status: "active",
        navOrder: 0,
        registryRevision: "1",
      },
      objects: [
        {
          id: objectId,
          orgId: "org-a",
          appId: "equipment",
          apiName: recordIdentifier("loan"),
          sourceApiName: null,
          label: "Loan",
          pluralLabel: "Loans",
          tableSchema: recordIdentifier("public"),
          tableName: recordIdentifier("equipment__loan"),
          nameField: recordIdentifier("name"),
          visibility: "org",
          custom: false,
          archivedAt: null,
          recordCount: null,
        },
      ],
      fields: [
        {
          id: "00000000-0000-0000-0000-000000000952",
          orgId: "org-a",
          objectId,
          apiName: recordIdentifier("name"),
          sourceApiName: null,
          label: "Name",
          kind: "text",
          columnName: recordIdentifier("name"),
          required: true,
          readOnly: false,
          archivedAt: null,
          picklistValues: null,
          referenceTargets: null,
          length: null,
          scale: null,
        },
      ],
      layouts: [],
      pages: [],
      permissions: [],
    };
    const plan = buildRecordImportPlan({
      snapshot,
      objectApiName: "loan",
      sourcePath: "imports/run-1/loans.csv",
      sourceName: "loans.csv",
      bytes: Buffer.from("id,name\nloan-1,Laptop\nloan-2,Monitor\n"),
    });
    const run = await createRecordImportRun(pool, {
      id: "00000000-0000-4000-a000-000000000953",
      orgId: "org-a",
      actionRequestId: "import-action-1",
      plan,
    });
    await claimRecordsActionExecution(pool, {
      actionRequestId: run.actionRequestId,
      orgId: run.orgId,
      appId: run.appId,
      actionKind: "records_import_start",
      leaseOwner: "test-worker",
      leaseMs: 60_000,
    });
    await setRecordsActionExecutionContext(pool, {
      actionRequestId: run.actionRequestId,
      leaseOwner: "test-worker",
      context: {
        import_batch: {
          run_id: run.id,
          object_api_name: "loan",
          batch_no: 0,
          batch_hash: "batch-a",
        },
      },
    });
    await pool.query(`
      insert into public.equipment__loan
        (id, org_id, data, nk_created_by, nk_updated_by,
         nk_action_request_id, nk_mutation_id)
      values
        ('loan-1', 'org-a', '{"name": "Laptop"}', 'admin-1', 'admin-1',
         'import-action-1', 'mutation-import-1'),
        ('loan-2', 'org-a', '{"name": "Monitor"}', 'admin-1', 'admin-1',
         'import-action-1', 'mutation-import-2')
    `);
    expect(
      await getRecordImportReceipt(pool, {
        runId: run.id,
        objectApiName: "loan",
        batchHash: "batch-a",
      }),
    ).toMatchObject({ batchNo: 0, rowCount: 2 });
    const changes = await pool.query<{ action: string }>(
      `select action from engine.record_change_log
       where action_request_id = 'import-action-1' order by record_id`,
    );
    expect(changes.rows).toEqual([{ action: "import" }, { action: "import" }]);

    await setRecordsActionExecutionContext(pool, {
      actionRequestId: run.actionRequestId,
      leaseOwner: "test-worker",
      context: {
        import_batch: {
          run_id: run.id,
          object_api_name: "loan",
          batch_no: 2,
          batch_hash: "batch-rollback",
        },
      },
    });
    const client = await pool.connect();
    try {
      await client.query("begin");
      await client.query(`
        insert into public.equipment__loan
          (id, org_id, data, nk_created_by, nk_updated_by,
           nk_action_request_id, nk_mutation_id)
        values ('loan-3', 'org-a', '{"name": "Dock"}', 'admin-1', 'admin-1',
                'import-action-1', 'mutation-import-3')
      `);
      await client.query("rollback");
    } finally {
      client.release();
    }
    expect(
      await getRecordImportReceipt(pool, {
        runId: run.id,
        objectApiName: "loan",
        batchHash: "batch-rollback",
      }),
    ).toBeNull();
    expect(await summarizeRecordImportRun(pool, run.id)).toEqual({
      inserted: 2,
      rejected: 0,
      duplicates: 0,
      batches: 1,
    });
  });
});
