import pg from "pg";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  buildRecordsPoolConfig,
  runRecordsMigrations,
} from "@neko/db/records-migrate";
import { RecordsGraphjinSchemaCli } from "../src/graphjin/schema";
import {
  quoteRecordIdentifier,
  recordTableName,
  validateRecordIdentifier,
} from "../src/naming";
import type { RecordDdlObjectDefinition } from "../src/schema/ddl";
import {
  createRecordsSchemaWorkspace,
  RecordsSchemaApprovalStaleError,
  RecordsSchemaSaga,
  type RecordSchemaChange,
} from "../src/schema/saga";

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
  console.warn("[records-schema-saga] skipping: records Postgres unreachable.");
}

describeIfRecordsDb("records schema saga integration", () => {
  const database = `records_saga_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`;
  let adminPool: pg.Pool;
  let testPool: pg.Pool;
  let configFile: string;

  beforeAll(async () => {
    adminPool = new pg.Pool(buildRecordsPoolConfig());
    await adminPool.query(`create database ${database}`);
    testPool = new pg.Pool(buildRecordsPoolConfig(process.env, { database }));
    await runRecordsMigrations({ pool: testPool });
    const configDirectory = await mkdtemp(join(tmpdir(), "records-saga-config-"));
    configFile = join(configDirectory, "dev.yml");
    await writeFile(configFile, "database: {}\n", { mode: 0o600 });
  });

  afterAll(async () => {
    if (testPool) await testPool.end();
    if (adminPool) {
      let activeConnections = 1;
      for (let attempt = 0; attempt < 20 && activeConnections > 0; attempt += 1) {
        const sessions = await adminPool.query<{ count: string }>(
          "select count(*) from pg_stat_activity where datname = $1",
          [database],
        );
        activeConnections = Number(sessions.rows[0]?.count ?? "0");
        if (activeConnections > 0) {
          await new Promise((resolve) => setTimeout(resolve, 10));
        }
      }
      expect(activeConnections).toBe(0);
      await adminPool.query(`drop database if exists ${database}`);
      await adminPool.end();
    }
  });

  function desiredObject(appId: string): RecordDdlObjectDefinition {
    return {
      appId,
      apiName: validateRecordIdentifier("item"),
      tableName: recordTableName(appId, "item"),
      nameField: validateRecordIdentifier("name"),
      visibility: "owner",
      fields: [
        { columnName: validateRecordIdentifier("name"), kind: "text", required: true },
      ],
    };
  }

  async function createPhysical(object: RecordDdlObjectDefinition): Promise<void> {
    const table = quoteRecordIdentifier(object.tableName);
    await testPool.query(`
      create table ${table} (
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
  }

  async function seedApp(appId: string): Promise<void> {
    await testPool.query(
      `insert into engine.record_app (org_id, app_id, label, status)
       values ('org-a', $1, $2, 'provisioning')`,
      [appId, appId],
    );
  }

  async function projectRegistry(change: RecordSchemaChange): Promise<void> {
    const object = change.artifact.objects[0]!;
    await testPool.query(
      `insert into engine.record_object
         (org_id, app_id, api_name, label, plural_label, table_name,
          name_field, visibility)
       values ($1, $2, $3, 'Item', 'Items', $4, 'name', 'owner')
       on conflict (org_id, app_id, api_name) do update
       set table_name = excluded.table_name, updated_at = now()`,
      [change.orgId, change.appId, object.apiName, object.tableName],
    );
    await testPool.query(
      `insert into engine.record_field
         (org_id, object_id, api_name, label, kind, column_name, required)
       select $1, id, 'name', 'Name', 'text', 'name', true
       from engine.record_object where org_id = $1 and app_id = $2 and api_name = $3
       on conflict (object_id, api_name) do update set updated_at = now()`,
      [change.orgId, change.appId, object.apiName],
    );
    await testPool.query(
      `update engine.record_app
       set status = 'active', registry_revision = $3::bigint, updated_at = now()
       where org_id = $1 and app_id = $2`,
      [change.orgId, change.appId, change.desiredRevision],
    );
  }

  function makeSaga(input: {
    object: RecordDdlObjectDefinition;
    readiness: boolean[];
    syncCalls: { count: number };
    project?: (change: RecordSchemaChange) => Promise<void>;
  }): RecordsSchemaSaga {
    let physicallyApplied = false;
    const cli = new RecordsGraphjinSchemaCli({
      command: async (_binary, args) => {
        if (args[0] === "version") return { stdout: "GraphJin 3.21.13\n", stderr: "" };
        if (args.includes("diff")) {
          return {
            stdout: physicallyApplied
              ? ""
              : `CREATE TABLE "${input.object.tableName}" ("id" TEXT PRIMARY KEY);\n`,
            stderr: "",
          };
        }
        if (args.includes("sync")) {
          input.syncCalls.count += 1;
          await createPhysical(input.object);
          physicallyApplied = true;
          return { stdout: "applied\n", stderr: "" };
        }
        throw new Error(`unexpected fake GraphJin command: ${args.join(" ")}`);
      },
    });
    return new RecordsSchemaSaga({
      pool: testPool,
      cli,
      workspace: createRecordsSchemaWorkspace(configFile),
      setDataPlaneReady: async (ready) => {
        input.readiness.push(ready);
      },
      project: input.project ?? projectRegistry,
    });
  }

  async function planAndApprove(
    saga: RecordsSchemaSaga,
    appId: string,
    object: RecordDdlObjectDefinition,
  ): Promise<RecordSchemaChange> {
    const planned = await saga.plan({
      orgId: "org-a",
      appId,
      actionRequestId: `request-${appId}`,
      desiredRevision: "1",
      action: "app_create",
      actorUserId: "admin-1",
      detail: { label: appId, objects: ["item"] },
      objects: [object],
    });
    return saga.approve(planned.actionRequestId, planned.previewHash);
  }

  it("binds approval bytes and projects every durable phase", async () => {
    const appId = "equipment";
    const object = desiredObject(appId);
    await seedApp(appId);
    const readiness: boolean[] = [];
    const syncCalls = { count: 0 };
    const saga = makeSaga({ object, readiness, syncCalls });
    const approved = await planAndApprove(saga, appId, object);

    await expect(saga.execute(approved.actionRequestId)).resolves.toMatchObject({
      phase: "projected",
      attempts: 1,
    });
    expect(syncCalls.count).toBe(1);
    expect(readiness).toEqual([false, true]);
    const phases = await testPool.query<{ phase: string }>(
      `select detail->>'sagaPhase' as phase from engine.app_schema_log
       where action_request_id = $1 order by id`,
      [approved.actionRequestId],
    );
    expect(phases.rows.map((row) => row.phase)).toEqual([
      "planned",
      "approved",
      "applying",
      "applied",
      "projected",
    ]);
    const trigger = await testPool.query<{ count: string }>(
      `select count(*) from pg_catalog.pg_trigger t
       join pg_catalog.pg_class c on c.oid = t.tgrelid
       where c.relname = $1 and t.tgname = 'nk_record_change_audit'`,
      [object.tableName],
    );
    expect(trigger.rows).toEqual([{ count: "1" }]);
  });

  it("recovers when GraphJin committed before the applying journal advanced", async () => {
    const appId = "support";
    const object = desiredObject(appId);
    await seedApp(appId);
    const readiness: boolean[] = [];
    const syncCalls = { count: 0 };
    const saga = makeSaga({ object, readiness, syncCalls });
    const approved = await planAndApprove(saga, appId, object);
    await testPool.query(
      `update engine.app_schema_change set phase = 'applying', attempts = 1
       where action_request_id = $1`,
      [approved.actionRequestId],
    );
    await createPhysical(object);

    await expect(saga.reconcilePending()).resolves.toEqual({
      projected: [approved.actionRequestId],
      failed: [],
    });
    expect(syncCalls.count).toBe(0);
    expect(await saga.get(approved.actionRequestId)).toMatchObject({ phase: "projected" });
  });

  it("keeps applied work resumable when projection fails", async () => {
    const appId = "inventory";
    const object = desiredObject(appId);
    await seedApp(appId);
    const readiness: boolean[] = [];
    const syncCalls = { count: 0 };
    let failProjection = true;
    const saga = makeSaga({
      object,
      readiness,
      syncCalls,
      project: async (change) => {
        if (failProjection) {
          failProjection = false;
          throw new Error("simulated metadata mirror outage");
        }
        await projectRegistry(change);
      },
    });
    const approved = await planAndApprove(saga, appId, object);

    await expect(saga.execute(approved.actionRequestId)).rejects.toThrow(/mirror outage/);
    expect(await saga.get(approved.actionRequestId)).toMatchObject({
      phase: "applied",
      lastError: { code: "records_schema_execution_failed" },
    });
    await expect(saga.execute(approved.actionRequestId)).resolves.toMatchObject({
      phase: "projected",
    });
    expect(syncCalls.count).toBe(1);
    expect(readiness.at(-1)).toBe(true);
  });

  it("invalidates approval when the live catalog drifts", async () => {
    const appId = "crm";
    const object = desiredObject(appId);
    await seedApp(appId);
    const readiness: boolean[] = [];
    const syncCalls = { count: 0 };
    const saga = makeSaga({ object, readiness, syncCalls });
    const approved = await planAndApprove(saga, appId, object);
    await testPool.query("create table public.unapproved_drift (id text primary key)");

    await expect(saga.execute(approved.actionRequestId)).rejects.toBeInstanceOf(
      RecordsSchemaApprovalStaleError,
    );
    expect(syncCalls.count).toBe(0);
    expect(await saga.get(approved.actionRequestId)).toMatchObject({
      phase: "failed",
      lastError: { code: "records_schema_approval_stale" },
    });
    expect(readiness).toEqual([false]);
  });
});
