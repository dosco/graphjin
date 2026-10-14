import pg from "pg";
import { execFile } from "node:child_process";
import { spawn } from "node:child_process";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  buildRecordsPoolConfig,
  runRecordsMigrations,
} from "@neko/db/records-migrate";
import {
  mintRecordsGraphjinToken,
  RecordsGraphjinClient,
  RecordsGraphjinRequestError,
  type RecordsGraphjinExecuteInput,
  type RecordsGraphjinTransport,
} from "../src/graphjin/client";
import { writeRecordsGraphjinConfig } from "../src/graphjin/config";
import { RecordImportExecutor } from "../src/import/executor";
import { buildRecordImportPlan } from "../src/import/plan";
import {
  cancelRecordImportRun,
  createRecordImportRun,
  getRecordImportRun,
} from "../src/import/store";
import { recordIdentifier } from "../src/naming";
import {
  loadRecordsGraphjinPolicyModel,
  projectRecordsGraphjinRoles,
} from "../src/policy/graphjin";
import { ensureRecordsAuditTrigger } from "../src/schema/audit";
import type { AppRegistrySnapshot } from "../src/types";

const graphjinImage = process.env.RECORDS_GRAPHJIN_IMAGE;
const JWT_SECRET = "import-executor-jwt-secret-that-is-at-least-thirty-two-bytes";

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

function runCommand(command: string, args: string[]): Promise<{ stdout: string; stderr: string }> {
  return new Promise((resolve, reject) => {
    execFile(
      command,
      args,
      { timeout: 30_000, env: { ...process.env, GO_ENV: "development" } },
      (error, stdout, stderr) => {
        if (error) reject(new Error(String(stderr) || error.message));
        else resolve({ stdout: String(stdout), stderr: String(stderr) });
      },
    );
  });
}

function runKilledImportWorker(env: NodeJS.ProcessEnv): Promise<void> {
  return new Promise((resolve, reject) => {
    const helper = fileURLToPath(
      new URL("./helpers/import-kill-worker.ts", import.meta.url),
    );
    const child = spawn(
      join(process.cwd(), "node_modules", ".bin", "tsx"),
      [helper],
      {
        env: { ...process.env, ...env },
        stdio: ["ignore", "ignore", "pipe"],
      },
    );
    let stderr = "";
    let settled = false;
    child.stderr.on("data", (chunk) => {
      stderr += String(chunk);
    });
    const timeout = setTimeout(() => {
      if (settled) return;
      settled = true;
      child.kill("SIGKILL");
      reject(new Error(`kill-worker timed out: ${stderr}`));
    }, 30_000);
    child.once("error", (error) => {
      if (settled) return;
      settled = true;
      clearTimeout(timeout);
      reject(error);
    });
    child.once("exit", (code, signal) => {
      if (settled) return;
      settled = true;
      clearTimeout(timeout);
      // Direct Node reports SIGKILL as a signal; the tsx launcher forwards its
      // child exit as the conventional 128 + 9 status instead.
      if (signal === "SIGKILL" || code === 137) resolve();
      else {
        reject(
          new Error(
            `kill-worker exited code=${code} signal=${signal}: ${stderr}`,
          ),
        );
      }
    });
  });
}

const reachable = await recordsDbReachable();
const describeIfLive = reachable && graphjinImage ? describe : describe.skip;

if (process.env.REQUIRE_GRAPHJIN_INTEGRATION === "1") {
  if (!reachable) throw new Error("required records Postgres is unreachable");
  if (!graphjinImage) throw new Error("required RECORDS_GRAPHJIN_IMAGE is not set");
}

if (!reachable || !graphjinImage) {
  console.warn(
    "[records-import-executor] skipping: records Postgres or RECORDS_GRAPHJIN_IMAGE unavailable",
  );
}

class LoseFirstMutationResponse implements RecordsGraphjinTransport {
  lost = false;

  constructor(private readonly inner: RecordsGraphjinTransport) {}

  async execute<T>(input: RecordsGraphjinExecuteInput): Promise<T> {
    const result = await this.inner.execute<T>(input);
    if (!this.lost && input.operationName === "RecordsImportBatch") {
      this.lost = true;
      throw new RecordsGraphjinRequestError("simulated lost response", null);
    }
    return result;
  }
}

class CancelAfterFirstMutation implements RecordsGraphjinTransport {
  mutations = 0;

  constructor(
    private readonly inner: RecordsGraphjinTransport,
    private readonly cancel: () => Promise<void>,
  ) {}

  async execute<T>(input: RecordsGraphjinExecuteInput): Promise<T> {
    const result = await this.inner.execute<T>(input);
    if (input.operationName === "RecordsImportBatch" && this.mutations++ === 0) {
      await this.cancel();
    }
    return result;
  }
}

describeIfLive("records CSV import executor live integration", () => {
  const database = `records_import_exec_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`;
  const objectId = "00000000-0000-0000-0000-000000000991";
  let adminPool: pg.Pool;
  let pool: pg.Pool;
  let containerId = "";
  let graphjin: RecordsGraphjinClient;
  let graphjinUrl = "";

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
        (id, org_id, object_id, api_name, source_api_name, label, kind, column_name,
         required, read_only)
      values
        ('00000000-0000-0000-0000-000000000992', 'org-a', '${objectId}',
         'name', 'Name', 'Name', 'text', 'name', true, false),
        ('00000000-0000-0000-0000-000000000993', 'org-a', '${objectId}',
         'available', 'Available', 'Available', 'boolean', 'available', false, false),
        ('00000000-0000-0000-0000-000000000997', 'org-a', '${objectId}',
         'score__c', 'Score__c', 'Score', 'readonly_formula', 'score__c', false, true);
      insert into engine.record_permission
        (org_id, app_id, role, object_api_name,
         can_read, can_create, can_update, can_delete)
      values
        ('org-a', 'equipment', 'admin', 'loan', true, true, true, true),
        ('org-a', 'equipment', 'member', 'loan', true, true, true, true);
      insert into engine.actor (org_id, user_id, role)
      values ('org-a', 'records-service', 'service');
      create table public.equipment__loan (
        id text primary key,
        org_id text not null,
        data jsonb not null,
        nk_created_at timestamptz not null default now(),
        nk_created_by text not null,
        nk_updated_at timestamptz not null default now(),
        nk_updated_by text not null,
        nk_action_request_id text not null,
        nk_mutation_id text not null,
        nk_deleted_at timestamptz
      );
      insert into public.equipment__loan
        (id, org_id, data, nk_created_by, nk_updated_by,
         nk_action_request_id, nk_mutation_id)
      values ('loan-existing', 'org-a', '{"name": "Existing", "available": true}', 'seed', 'seed',
              'seed-request', 'seed-mutation');
    `);
    await ensureRecordsAuditTrigger(pool, {
      tableSchema: recordIdentifier("public"),
      tableName: recordIdentifier("equipment__loan"),
      appId: "equipment",
      objectApiName: recordIdentifier("loan"),
    });

    const directory = await mkdtemp(join(tmpdir(), "records-import-config-"));
    const poolConfig = buildRecordsPoolConfig(process.env, { database });
    const connection = new URL("postgres://localhost/records");
    connection.hostname = process.env.RECORDS_GRAPHJIN_DB_HOST ?? "host.internal";
    connection.port = String(poolConfig.port);
    connection.username = String(poolConfig.user);
    connection.password = String(poolConfig.password ?? "");
    connection.pathname = `/${database}`;
    connection.searchParams.set("sslmode", "disable");
    await writeRecordsGraphjinConfig({
      configFile: join(directory, "dev.yml"),
      config: {
        orgId: "org-a",
        roles: projectRecordsGraphjinRoles(
          await loadRecordsGraphjinPolicyModel(pool, "org-a"),
        ),
        database: { connectionString: connection.toString() },
        jwt: { secret: JWT_SECRET },
        secretKey: "import-cursor-secret-that-is-at-least-thirty-two-bytes",
      },
      validate: async ({ configDirectory }) => {
        await runCommand("docker", [
          "run",
          "--rm",
          "--entrypoint",
          "graphjin",
          "--volume",
          `${configDirectory}:/config:ro`,
          graphjinImage!,
          "serve",
          "test",
          "--json",
          "--path",
          "/config",
        ]);
      },
      reloadRecordsGraphjin: async () => {},
    });
    const started = await runCommand("docker", [
      "run",
      "--detach",
      "--rm",
      "--entrypoint",
      "graphjin",
      "--publish",
      "127.0.0.1::8090",
      "--volume",
      `${directory}:/config:ro`,
      graphjinImage!,
      "serve",
      "--path",
      "/config",
    ]);
    containerId = started.stdout.trim();
    const portOutput = await runCommand("docker", ["port", containerId, "8090/tcp"]);
    const port = Number.parseInt(portOutput.stdout.trim().split(":").at(-1) ?? "", 10);
    for (let attempt = 0; attempt < 50; attempt += 1) {
      try {
        if ((await fetch(`http://127.0.0.1:${port}/health`)).ok) break;
      } catch {
        // Still starting.
      }
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    graphjinUrl = `http://127.0.0.1:${port}`;
    graphjin = new RecordsGraphjinClient({ baseUrl: graphjinUrl });
  }, 30_000);

  afterAll(async () => {
    if (containerId) await runCommand("docker", ["stop", containerId]).catch(() => undefined);
    if (pool) await pool.end();
    if (adminPool) {
      await adminPool.query(`drop database if exists ${database}`);
      await adminPool.end();
    }
  });

  it("bulk inserts, quarantines rejects, and recovers a lost commit response", async () => {
    const bytes = Buffer.from(
      [
        "id,name,available",
        "loan-1,Laptop,true",
        "loan-2,Monitor,false",
        "loan-2,Duplicate,true",
        "loan-existing,Already here,true",
        "loan-3,,true",
        "loan-4,Dock,not-a-boolean",
      ].join("\n"),
    );
    const registry = new (await import("../src/registry")).RecordRegistry(pool);
    const loaded = await registry.loadApp("org-a", "equipment");
    expect(loaded).not.toBeNull();
    const plan = buildRecordImportPlan({
      snapshot: loaded as AppRegistrySnapshot,
      objectApiName: "loan",
      sourcePath: "imports/run-1/loans.csv",
      sourceName: "loans.csv",
      bytes,
      batchSize: 3,
    });
    const run = await createRecordImportRun(pool, {
      id: "00000000-0000-4000-a000-000000000994",
      orgId: "org-a",
      actionRequestId: "import-executor-action",
      plan,
    });
    const lossyTransport = new LoseFirstMutationResponse(graphjin);
    const executor = new RecordImportExecutor({
      pool,
      graphjin: lossyTransport,
      serviceToken: () =>
        mintRecordsGraphjinToken({
          secret: JWT_SECRET,
          orgId: "org-a",
          userId: "records-service",
          role: "service",
        }),
      leaseOwner: "import-test-worker",
      readSource: async () => bytes,
    });

    const report = await executor.execute({
      orgId: "org-a",
      importRunId: run.id,
      actorUserId: "admin-1",
    });
    expect(lossyTransport.lost).toBe(true);
    expect(report).toEqual({
      status: "succeeded",
      importRunId: run.id,
      appId: "equipment",
      objectApiName: "loan",
      sourceName: "loans.csv",
      sourceRows: 6,
      inserted: 2,
      rejected: 4,
      duplicates: 2,
      batches: 2,
      reconciled: true,
    });
    const rows = await pool.query<{ id: string }>(
      "select id from public.equipment__loan order by id",
    );
    expect(rows.rows.map((row) => row.id)).toEqual([
      "loan-1",
      "loan-2",
      "loan-existing",
    ]);
    const audit = await pool.query<{ action: string }>(
      `select action from engine.record_change_log
       where action_request_id = 'import-executor-action' order by record_id`,
    );
    expect(audit.rows).toEqual([{ action: "import" }, { action: "import" }]);
    const rejectCodes = await pool.query<{ reason_code: string }>(
      `select reason_code from engine.import_reject
       where import_run_id = $1 order by row_number`,
      [run.id],
    );
    expect(rejectCodes.rows.map((row) => row.reason_code)).toEqual([
      "duplicate",
      "duplicate",
      "validation",
      "validation",
    ]);

    await expect(
      executor.execute({
        orgId: "org-a",
        importRunId: run.id,
        actorUserId: "admin-1",
      }),
    ).resolves.toEqual(report);
  }, 30_000);

  it("honors cancellation at the next committed batch boundary", async () => {
    const bytes = Buffer.from(
      [
        "id,name,available",
        "cancel-1,Keyboard,true",
        "cancel-2,Mouse,true",
        "cancel-3,Headset,false",
      ].join("\n"),
    );
    const registry = new (await import("../src/registry")).RecordRegistry(pool);
    const loaded = await registry.loadApp("org-a", "equipment");
    expect(loaded).not.toBeNull();
    const plan = buildRecordImportPlan({
      snapshot: loaded as AppRegistrySnapshot,
      objectApiName: "loan",
      sourcePath: "imports/run-cancel/loans.csv",
      sourceName: "loans.csv",
      bytes,
      batchSize: 1,
    });
    const run = await createRecordImportRun(pool, {
      id: "00000000-0000-4000-a000-000000000995",
      orgId: "org-a",
      actionRequestId: "import-executor-cancel-action",
      plan,
    });
    const cancellingTransport = new CancelAfterFirstMutation(graphjin, async () => {
      const requested = await cancelRecordImportRun(pool, {
        orgId: "org-a",
        id: run.id,
      });
      expect(requested).toMatchObject({
        status: "running",
        cancelRequestedAt: expect.any(Date),
      });
    });
    const executor = new RecordImportExecutor({
      pool,
      graphjin: cancellingTransport,
      serviceToken: () =>
        mintRecordsGraphjinToken({
          secret: JWT_SECRET,
          orgId: "org-a",
          userId: "records-service",
          role: "service",
        }),
      leaseOwner: "import-cancel-test-worker",
      readSource: async () => bytes,
    });

    const report = await executor.execute({
      orgId: "org-a",
      importRunId: run.id,
      actorUserId: "admin-1",
    });

    expect(cancellingTransport.mutations).toBe(1);
    expect(report).toEqual({
      status: "cancelled",
      importRunId: run.id,
      appId: "equipment",
      objectApiName: "loan",
      sourceName: "loans.csv",
      sourceRows: 3,
      inserted: 1,
      rejected: 0,
      duplicates: 0,
      batches: 1,
      reconciled: false,
    });
    await expect(
      getRecordImportRun(pool, { orgId: "org-a", id: run.id }),
    ).resolves.toMatchObject({
      status: "cancelled",
      currentStage: "cancelled",
      report,
      cancelRequestedAt: expect.any(Date),
    });
    const rows = await pool.query<{ id: string }>(
      "select id from public.equipment__loan where id like 'cancel-%' order by id",
    );
    expect(rows.rows).toEqual([{ id: "cancel-1" }]);
  }, 30_000);

  it("materializes source-computed fields and canonicalizes Salesforce ids", async () => {
    const bytes = Buffer.from(
      "Id,Name,Score__c\n001A000010khO8J,Salesforce account,12.50\n",
    );
    const registry = new (await import("../src/registry")).RecordRegistry(pool);
    const loaded = await registry.loadApp("org-a", "equipment");
    const plan = buildRecordImportPlan({
      snapshot: loaded as AppRegistrySnapshot,
      objectApiName: "loan",
      sourcePath: "imports/salesforce/job/data/account.csv",
      sourceName: "account.csv",
      bytes,
      mapping: { Id: "id", Name: "name", Score__c: "score__c" },
      allowReadOnly: true,
      normalizeIdColumns: ["Id"],
    });
    const run = await createRecordImportRun(pool, {
      id: "00000000-0000-4000-a000-000000000998",
      orgId: "org-a",
      actionRequestId: "artifact-import-account-action",
      sourceInstanceId: "salesforce-production",
      plan,
    });
    const executor = new RecordImportExecutor({
      pool,
      graphjin,
      serviceToken: () =>
        mintRecordsGraphjinToken({
          secret: JWT_SECRET,
          orgId: "org-a",
          userId: "records-service",
          role: "service",
        }),
      leaseOwner: "artifact-import-worker",
      readSource: async () => bytes,
    });
    await expect(
      executor.execute({
        orgId: "org-a",
        importRunId: run.id,
        actorUserId: "admin-1",
      }),
    ).resolves.toMatchObject({ inserted: 1, rejected: 0, reconciled: true });
    await expect(
      pool.query(
        `select id, data->>'score__c' as score__c from public.equipment__loan
         where id = '001A000010khO8JIAU'`,
      ),
    ).resolves.toMatchObject({
      rows: [{ id: "001A000010khO8JIAU", score__c: "12.50" }],
    });
  }, 30_000);

  it("reconciles 5,000 rows after kills before and after every batch commit", async () => {
    const rowCount = 5_000;
    const batchSize = 250;
    const bytes = Buffer.from(
      [
        "id,name,available",
        ...Array.from({ length: rowCount }, (_, index) => {
          const ordinal = String(index + 1).padStart(5, "0");
          const available = index % 2 === 0 ? "true" : "false";
          return `scale-${ordinal},Asset ${ordinal},${available}`;
        }),
      ].join("\n"),
    );
    const sourceDirectory = await mkdtemp(join(tmpdir(), "records-import-kill-"));
    const sourceFile = join(sourceDirectory, "loans.csv");
    await writeFile(sourceFile, bytes);

    const registry = new (await import("../src/registry")).RecordRegistry(pool);
    const loaded = await registry.loadApp("org-a", "equipment");
    expect(loaded).not.toBeNull();
    const plan = buildRecordImportPlan({
      snapshot: loaded as AppRegistrySnapshot,
      objectApiName: "loan",
      sourcePath: "imports/run-scale/loans.csv",
      sourceName: "loans.csv",
      bytes,
      batchSize,
    });
    const run = await createRecordImportRun(pool, {
      id: "00000000-0000-4000-a000-000000000996",
      orgId: "org-a",
      actionRequestId: "import-executor-scale-action",
      plan,
    });
    const leaseOwner = "records-import-job:scale-job";
    const childEnv = {
      RECORDS_IMPORT_TEST_DATABASE: database,
      RECORDS_IMPORT_TEST_GRAPHJIN_URL: graphjinUrl,
      RECORDS_IMPORT_TEST_JWT_SECRET: JWT_SECRET,
      RECORDS_IMPORT_TEST_LEASE_OWNER: leaseOwner,
      RECORDS_IMPORT_TEST_RUN_ID: run.id,
      RECORDS_IMPORT_TEST_SOURCE: sourceFile,
    };
    const batchCount = rowCount / batchSize;

    for (let batch = 0; batch < batchCount; batch += 1) {
      await runKilledImportWorker({
        ...childEnv,
        RECORDS_IMPORT_KILL_MODE: "before",
      });
      const before = await pool.query<{ count: string }>(
        "select count(*)::text as count from engine.import_batch_receipt where import_run_id = $1",
        [run.id],
      );
      expect(Number(before.rows[0]?.count)).toBe(batch);

      await runKilledImportWorker({
        ...childEnv,
        RECORDS_IMPORT_KILL_MODE: "after",
      });
      const after = await pool.query<{ count: string }>(
        "select count(*)::text as count from engine.import_batch_receipt where import_run_id = $1",
        [run.id],
      );
      expect(Number(after.rows[0]?.count)).toBe(batch + 1);
    }

    const executor = new RecordImportExecutor({
      pool,
      graphjin,
      serviceToken: () =>
        mintRecordsGraphjinToken({
          secret: JWT_SECRET,
          orgId: "org-a",
          userId: "records-service",
          role: "service",
        }),
      leaseOwner,
      readSource: async () => bytes,
    });
    const report = await executor.execute({
      orgId: "org-a",
      importRunId: run.id,
      actorUserId: "admin-1",
      leaseOwner,
    });

    expect(report).toMatchObject({
      status: "succeeded",
      sourceRows: rowCount,
      inserted: rowCount,
      rejected: 0,
      duplicates: 0,
      batches: batchCount,
      reconciled: true,
    });
    const rows = await pool.query<{ total: string; distinct_ids: string }>(
      `select count(*)::text as total, count(distinct id)::text as distinct_ids
       from public.equipment__loan where id like 'scale-%'`,
    );
    expect(rows.rows[0]).toEqual({
      total: String(rowCount),
      distinct_ids: String(rowCount),
    });
    const receipts = await pool.query<{ rows: string; batches: string }>(
      `select coalesce(sum(row_count), 0)::text as rows,
              count(*)::text as batches
       from engine.import_batch_receipt where import_run_id = $1`,
      [run.id],
    );
    expect(receipts.rows[0]).toEqual({
      rows: String(rowCount),
      batches: String(batchCount),
    });
  }, 120_000);
});
