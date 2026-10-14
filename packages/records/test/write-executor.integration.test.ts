import pg from "pg";
import { execFile } from "node:child_process";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  buildRecordsPoolConfig,
  runRecordsMigrations,
} from "@neko/db/records-migrate";
import {
  mintRecordsGraphjinToken,
  RecordsGraphjinClient,
} from "../src/graphjin/client";
import { writeRecordsGraphjinConfig } from "../src/graphjin/config";
import {
  loadRecordsGraphjinPolicyModel,
  projectRecordsGraphjinRoles,
} from "../src/policy/graphjin";
import { ensureRecordsAuditTrigger } from "../src/schema/audit";
import {
  RecordConcurrencyConflictError,
  RecordMirrorWriteBlockedError,
  RecordNotFoundOrDeniedError,
  RecordWriteExecutor,
} from "../src/write/executor";
import { RecordOwnerBackfillExecutor } from "../src/identity/backfill";
import { RecordValidationError } from "../src/write/validate";
import { validateRecordIdentifier } from "../src/naming";

const graphjinImage = process.env.RECORDS_GRAPHJIN_IMAGE;
const JWT_SECRET = "write-executor-jwt-secret-that-is-at-least-thirty-two-bytes";

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
        if (error) {
          const output = [String(stderr).trim(), String(stdout).trim()]
            .filter(Boolean)
            .join("\n");
          reject(new Error(output || error.message));
        }
        else resolve({ stdout: String(stdout), stderr: String(stderr) });
      },
    );
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
    "[records-write-executor] skipping: records Postgres or RECORDS_GRAPHJIN_IMAGE unavailable.",
  );
}

describeIfLive("records write executor live integration", () => {
  const database = `records_write_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`;
  let adminPool: pg.Pool;
  let testPool: pg.Pool;

  beforeAll(async () => {
    adminPool = new pg.Pool(buildRecordsPoolConfig());
    await adminPool.query(`create database ${database}`);
    testPool = new pg.Pool(buildRecordsPoolConfig(process.env, { database }));
    await runRecordsMigrations({ pool: testPool });
    await testPool.query(`
      insert into engine.registry_version (org_id, revision) values ('org-a', 1);
      insert into engine.record_app
        (org_id, app_id, label, status, registry_revision)
      values ('org-a', 'equipment', 'Equipment', 'active', 1);
      insert into engine.record_object
        (id, org_id, app_id, api_name, label, plural_label, table_name,
         name_field, visibility)
      values
        ('00000000-0000-0000-0000-000000000701', 'org-a', 'equipment',
         'loan', 'Loan', 'Loans', 'equipment__loan', 'name', 'owner');
      insert into engine.record_field
        (id, org_id, object_id, api_name, source_api_name, label, kind, column_name,
         required, read_only, picklist_values)
      values
        ('00000000-0000-0000-0000-000000000702', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'name', null, 'Name', 'text',
         'name', true, false, null),
        ('00000000-0000-0000-0000-000000000703', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'status', null, 'Status',
         'picklist', 'status', true, false, '["new","active"]'::jsonb),
        ('00000000-0000-0000-0000-000000000704', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'legacy_code', null, 'Legacy code',
         'readonly_formula', 'legacy_code', false, true, null),
        ('00000000-0000-0000-0000-000000000705', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'owner_id', 'OwnerId', 'Source owner',
         'text', 'owner_id', true, false, null);
      insert into engine.record_field
        (id, org_id, object_id, api_name, label, kind, column_name,
         required, read_only, picklist_values, reference_targets, length, scale)
      values
        ('00000000-0000-0000-0000-000000000706', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'notes', 'Notes', 'textarea',
         'notes', false, false, null, null, 1000, null),
        ('00000000-0000-0000-0000-000000000707', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'enabled', 'Enabled', 'boolean',
         'enabled', false, false, null, null, null, null),
        ('00000000-0000-0000-0000-000000000708', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'quantity', 'Quantity', 'integer',
         'quantity', false, false, null, null, null, null),
        ('00000000-0000-0000-0000-000000000709', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'score', 'Score', 'decimal',
         'score', false, false, null, null, 12, 3),
        ('00000000-0000-0000-0000-000000000710', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'cost', 'Cost', 'currency',
         'cost', false, false, null, null, 12, 2),
        ('00000000-0000-0000-0000-000000000711', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'utilization', 'Utilization', 'percent',
         'utilization', false, false, null, null, 6, 2),
        ('00000000-0000-0000-0000-000000000712', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'due_on', 'Due on', 'date',
         'due_on', false, false, null, null, null, null),
        ('00000000-0000-0000-0000-000000000713', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'due_at', 'Due at', 'datetime',
         'due_at', false, false, null, null, null, null),
        ('00000000-0000-0000-0000-000000000714', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'contact_email', 'Contact email', 'email',
         'contact_email', false, false, null, null, 320, null),
        ('00000000-0000-0000-0000-000000000715', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'contact_phone', 'Contact phone', 'phone',
         'contact_phone', false, false, null, null, 50, null),
        ('00000000-0000-0000-0000-000000000716', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'website', 'Website', 'url',
         'website', false, false, null, null, 500, null),
        ('00000000-0000-0000-0000-000000000717', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'tags', 'Tags', 'multipicklist',
         'tags', false, false, '["portable","priority"]'::jsonb, null, null, null),
        ('00000000-0000-0000-0000-000000000718', 'org-a',
         '00000000-0000-0000-0000-000000000701', 'related_loan', 'Related loan', 'reference',
         'related_loan_id', false, false, null, '["loan"]'::jsonb, null, null);
      insert into engine.record_permission
        (org_id, app_id, role, object_api_name,
         can_read, can_create, can_update, can_delete)
      values
        ('org-a', 'equipment', 'member', 'loan', true, true, true, true),
        ('org-a', 'equipment', 'admin', 'loan', true, true, true, true);
      insert into engine.app_access_grant
        (org_id, app_id, subject_type, subject_id)
      values
        ('org-a', 'equipment', 'role', 'member'),
        ('org-a', 'equipment', 'role', 'admin');
      insert into engine.object_access_grant
        (org_id, app_id, subject_type, subject_id, object_api_name,
         can_read, can_create, can_update, can_delete)
      values
        ('org-a', 'equipment', 'role', 'member', 'loan', true, true, true, true),
        ('org-a', 'equipment', 'role', 'admin', 'loan', true, true, true, true);
      insert into engine.field_access_grant
        (org_id, app_id, subject_type, subject_id, object_api_name, field_id,
         can_read, can_write)
      select 'org-a', 'equipment', 'role', role_name, 'loan', field.id,
             true, not field.read_only
      from engine.record_field field
      cross join (values ('member'), ('admin')) as roles(role_name)
      where field.object_id = '00000000-0000-0000-0000-000000000701';
      insert into engine.actor (org_id, user_id, role)
      values
        ('org-a', 'records-service', 'service'),
        ('org-a', 'admin-1', 'admin');
      insert into engine.identity_map
        (org_id, source_instance_id, app_id, source_user_id, source_email,
         source_name, source_is_active, app_user_id, status, linked_at)
      values
        ('org-a', 'sf-prod', 'equipment', '005-alice', 'alice@example.com',
         'Alice', true, 'user-alice', 'linked', now());

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
    await ensureRecordsAuditTrigger(testPool, {
      tableSchema: validateRecordIdentifier("public"),
      tableName: validateRecordIdentifier("equipment__loan"),
      appId: "equipment",
      objectApiName: validateRecordIdentifier("loan"),
    });
  });

  afterAll(async () => {
    if (testPool) await testPool.end();
    if (adminPool) {
      await adminPool.query(`drop database if exists ${database}`);
      await adminPool.end();
    }
  });

  it(
    "validates, authorizes, mutates, audits, replays, deletes, and restores",
    async () => {
      const directory = await mkdtemp(join(tmpdir(), "records-write-config-"));
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
            await loadRecordsGraphjinPolicyModel(testPool, "org-a"),
          ),
          database: { connectionString: connection.toString() },
          jwt: { secret: JWT_SECRET },
          secretKey: "write-cursor-secret-that-is-at-least-thirty-two-bytes",
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
      const containerId = started.stdout.trim();
      try {
        const portOutput = await runCommand("docker", ["port", containerId, "8090/tcp"]);
        const port = Number.parseInt(portOutput.stdout.trim().split(":").at(-1) ?? "", 10);
        let healthy = false;
        for (let attempt = 0; attempt < 50; attempt += 1) {
          try {
            if ((await fetch(`http://127.0.0.1:${port}/health`)).ok) {
              healthy = true;
              break;
            }
          } catch {
            // Container is still starting.
          }
          await new Promise((resolve) => setTimeout(resolve, 100));
        }
        if (!healthy) throw new Error("records GraphJin did not become healthy");

        const sourceWrites: Array<{ actionRequestId: string; recordId: string }> = [];
        const graphjin = new RecordsGraphjinClient({
          baseUrl: `http://127.0.0.1:${port}`,
        });
        const serviceToken = () =>
          mintRecordsGraphjinToken({
            secret: JWT_SECRET,
            orgId: "org-a",
            userId: "records-service",
            role: "service",
          });
        const mirrorExecutor = new RecordWriteExecutor({
          pool: testPool,
          graphjin,
          serviceToken,
          leaseOwner: "mirror-test-worker",
          recordSourceWrite: async () => {},
          appWriteMode: async () => "mirror",
        });
        await expect(
          mirrorExecutor.execute({
            actionRequestId: "request-mirror-write",
            orgId: "org-a",
            appId: "equipment",
            objectApiName: "loan",
            operation: "create",
            actor: { userId: "admin-1", role: "admin" },
            fields: { name: "Blocked", status: "new", owner_id: "005-alice" },
          }),
        ).rejects.toMatchObject({
          code: "records_mirror_write_blocked",
          message: "mirrored from Salesforce — writes happen there until cutover",
        } satisfies Partial<RecordMirrorWriteBlockedError>);
        const syncIdentity = {
          actor: {
            userId: "urn:openneko:sync:sf-prod",
            role: "admin" as const,
          },
          system: { kind: "sync" as const, sourceInstanceId: "sf-prod" },
        };
        await expect(
          mirrorExecutor.execute({
            actionRequestId: "sync-upsert-create",
            orgId: "org-a",
            appId: "equipment",
            objectApiName: "loan",
            operation: "upsert",
            id: "loan-sync",
            fields: {
              name: "Synced laptop",
              status: "new",
              legacy_code: "SF-001",
              owner_id: "005-alice",
              owner_user_id: "user-alice",
            },
            ...syncIdentity,
          }),
        ).resolves.toMatchObject({ operation: "create", replayed: false });
        await expect(
          mirrorExecutor.execute({
            actionRequestId: "sync-upsert-update",
            orgId: "org-a",
            appId: "equipment",
            objectApiName: "loan",
            operation: "upsert",
            id: "loan-sync",
            fields: {
              name: "Synced laptop",
              status: "active",
              legacy_code: "SF-002",
              owner_id: "005-alice",
              owner_user_id: "user-alice",
            },
            ...syncIdentity,
          }),
        ).resolves.toMatchObject({ operation: "update", replayed: false });
        await expect(
          mirrorExecutor.execute({
            actionRequestId: "sync-delete",
            orgId: "org-a",
            appId: "equipment",
            objectApiName: "loan",
            operation: "delete",
            id: "loan-sync",
            ...syncIdentity,
          }),
        ).resolves.toMatchObject({ operation: "delete", replayed: false });
        await expect(
          mirrorExecutor.execute({
            actionRequestId: "sync-delete-missing",
            orgId: "org-a",
            appId: "equipment",
            objectApiName: "loan",
            operation: "delete",
            id: "never-imported",
            ...syncIdentity,
          }),
        ).resolves.toMatchObject({ noOp: true });
        const synced = await testPool.query<{
          status: string;
          legacy_code: string;
          nk_deleted_at: Date | null;
        }>(
          `select data->>'status' as status, data->>'legacy_code' as legacy_code, nk_deleted_at
           from public.equipment__loan where id = 'loan-sync'`,
        );
        expect(synced.rows).toEqual([
          {
            status: "active",
            legacy_code: "SF-002",
            nk_deleted_at: expect.any(Date),
          },
        ]);
        const syncHistory = await testPool.query<{ action: string }>(
          `select action from engine.record_change_log
           where record_id = 'loan-sync' order by id`,
        );
        expect(syncHistory.rows).toEqual([
          { action: "sync" },
          { action: "sync" },
          { action: "sync" },
        ]);
        const syncRecycle = await testPool.query(
          `select 1 from engine.recycle_record where record_id = 'loan-sync'`,
        );
        expect(syncRecycle.rowCount).toBe(0);
        const executor = new RecordWriteExecutor({
          pool: testPool,
          graphjin,
          serviceToken,
          leaseOwner: "test-worker",
          recordSourceWrite: async (write) => {
            sourceWrites.push({
              actionRequestId: write.actionRequestId,
              recordId: write.recordId,
            });
          },
          now: () => new Date("2026-08-02T12:00:00.000Z"),
        });
        const member = { userId: "member-1", role: "member" as const };
        const admin = { userId: "admin-1", role: "admin" as const };

        const created = await executor.execute({
          actionRequestId: "request-create-loan",
          orgId: "org-a",
          appId: "equipment",
          objectApiName: "loan",
          operation: "create",
          actor: member,
          fields: {
            id: "loan-1",
            name: "Laptop",
            status: "new",
            owner_id: "005-alice",
            notes: "Issued for field inspections.\nReturn with charger.",
            enabled: true,
            quantity: 7,
            score: "98.125",
            cost: "1234.50",
            utilization: "63.5",
            due_on: "2026-08-15",
            due_at: "2026-08-15T16:30:00+05:30",
            contact_email: "ops@example.com",
            contact_phone: "+91 98765 43210",
            website: "https://example.com/loans/loan-1",
            tags: ["portable", "priority"],
          },
        });
        expect(created).toMatchObject({
          id: "loan-1",
          operation: "create",
          replayed: false,
          recovered: false,
        });
        await expect(
          executor.execute({
            actionRequestId: "request-create-loan",
            orgId: "org-a",
            appId: "equipment",
            objectApiName: "loan",
            operation: "create",
            actor: member,
            fields: {
              id: "loan-1",
              name: "Laptop",
              status: "new",
              owner_id: "005-alice",
            },
          }),
        ).resolves.toMatchObject({ id: "loan-1", replayed: true });

        await executor.execute({
          actionRequestId: "request-create-other",
          orgId: "org-a",
          appId: "equipment",
          objectApiName: "loan",
          operation: "create",
          actor: admin,
          fields: {
            id: "loan-2",
            name: "Other Laptop",
            status: "new",
            owner_id: "005-alice",
            owner_user_id: "member-2",
          },
        });
        await executor.execute({
          actionRequestId: "request-link-loan",
          orgId: "org-a",
          appId: "equipment",
          objectApiName: "loan",
          operation: "update",
          actor: member,
          id: "loan-1",
          fields: { related_loan: "loan-2" },
        });
        const fieldMatrix = await testPool.query<{ data: Record<string, unknown> }>(
          "select data from public.equipment__loan where id = 'loan-1'",
        );
        expect(fieldMatrix.rows[0]?.data).toMatchObject({
          notes: "Issued for field inspections.\nReturn with charger.",
          enabled: true,
          quantity: 7,
          score: 98.125,
          cost: 1234.5,
          utilization: 63.5,
          due_on: "2026-08-15",
          due_at: "2026-08-15T11:00:00.000Z",
          contact_email: "ops@example.com",
          contact_phone: "+91 98765 43210",
          website: "https://example.com/loans/loan-1",
          tags: ["portable", "priority"],
          related_loan_id: "loan-2",
        });
        await expect(
          executor.execute({
            actionRequestId: "request-update-other",
            orgId: "org-a",
            appId: "equipment",
            objectApiName: "loan",
            operation: "update",
            actor: member,
            id: "loan-2",
            fields: { status: "active" },
          }),
        ).rejects.toBeInstanceOf(RecordNotFoundOrDeniedError);

        const readLoan = () =>
          graphjin
            .execute<{ rows: Array<{ id: string; data: { status: string } }> }>({
              operationName: "RecordsWriteReadAfterMutation",
              query:
                "query RecordsWriteReadAfterMutation { rows: equipment__loan(where: { id: { eq: $id } }, limit: 1) { id data } }",
              variables: { id: "loan-1" },
              token: serviceToken(),
            })
            .then((result) => ({
              rows: result.rows.map((row) => ({ ...row.data, id: row.id })),
            }));
        await expect(readLoan()).resolves.toMatchObject({
          rows: [{ id: "loan-1", status: "new" }],
        });

        await executor.execute({
          actionRequestId: "request-update-loan",
          orgId: "org-a",
          appId: "equipment",
          objectApiName: "loan",
          operation: "update",
          actor: member,
          id: "loan-1",
          fields: { status: "active" },
          expected: { status: "new" },
        });
        // GraphJin returns an empty mutation selection when the updated value
        // was part of the where predicate. Its default response cache cannot
        // infer which row to invalidate in that case, so records configs keep
        // response caching disabled and the same named read must be fresh.
        await expect(readLoan()).resolves.toMatchObject({
          rows: [{ id: "loan-1", status: "active" }],
        });
        await expect(
          executor.execute({
            actionRequestId: "request-update-stale",
            orgId: "org-a",
            appId: "equipment",
            objectApiName: "loan",
            operation: "update",
            actor: member,
            id: "loan-1",
            fields: { name: "Stale Rename" },
            expected: { status: "new" },
          }),
        ).rejects.toBeInstanceOf(RecordConcurrencyConflictError);
        await expect(
          executor.execute({
            actionRequestId: "request-readonly",
            orgId: "org-a",
            appId: "equipment",
            objectApiName: "loan",
            operation: "update",
            actor: member,
            id: "loan-1",
            fields: { legacy_code: "forbidden" },
          }),
        ).rejects.toBeInstanceOf(RecordValidationError);

        await executor.execute({
          actionRequestId: "request-delete-loan",
          orgId: "org-a",
          appId: "equipment",
          objectApiName: "loan",
          operation: "delete",
          actor: member,
          id: "loan-1",
        });
        await executor.execute({
          actionRequestId: "request-restore-loan",
          orgId: "org-a",
          appId: "equipment",
          objectApiName: "loan",
          operation: "restore",
          actor: member,
          id: "loan-1",
        });

        const rows = await testPool.query<{
          id: string;
          org_id: string;
          owner_user_id: string;
          status: string;
          nk_deleted_at: Date | null;
        }>(`
          select id, org_id, owner_user_id, data->>'status' as status, nk_deleted_at
          from public.equipment__loan where nk_deleted_at is null order by id
        `);
        expect(rows.rows).toEqual([
          {
            id: "loan-1",
            org_id: "org-a",
            owner_user_id: "member-1",
            status: "active",
            nk_deleted_at: null,
          },
          {
            id: "loan-2",
            org_id: "org-a",
            owner_user_id: "member-2",
            status: "new",
            nk_deleted_at: null,
          },
        ]);
        const history = await testPool.query<{ action: string }>(
          `select action from engine.record_change_log
           where record_id = 'loan-1' order by id`,
        );
        expect(history.rows.map((row) => row.action)).toEqual([
          "create",
          "update",
          "update",
          "delete",
          "restore",
        ]);
        expect(sourceWrites.map((write) => write.actionRequestId)).toEqual([
          "request-create-loan",
          "request-create-other",
          "request-link-loan",
          "request-update-loan",
          "request-delete-loan",
          "request-restore-loan",
        ]);
        const succeeded = await testPool.query<{ count: string }>(
          "select count(*) from engine.action_execution where status = 'succeeded'",
        );
        expect(succeeded.rows).toEqual([{ count: "10" }]);

        const ownerBackfill = new RecordOwnerBackfillExecutor({
          pool: testPool,
          graphjin,
          serviceToken,
          leaseOwner: "identity-worker",
        });
        await expect(
          ownerBackfill.execute({
            actionRequestId: "request-owner-backfill",
            orgId: "org-a",
            appId: "equipment",
            sourceInstanceId: "sf-prod",
            actorUserId: "admin-1",
          }),
        ).resolves.toMatchObject({ scanned: 2, updated: 2, unchanged: 0 });
        await expect(
          ownerBackfill.execute({
            actionRequestId: "request-owner-backfill",
            orgId: "org-a",
            appId: "equipment",
            sourceInstanceId: "sf-prod",
            actorUserId: "admin-1",
          }),
        ).resolves.toMatchObject({ updated: 2, replayed: true });
        const ownership = await testPool.query<{
          id: string;
          owner_user_id: string;
        }>(
          `select id, owner_user_id from public.equipment__loan
           where nk_deleted_at is null order by id`,
        );
        expect(ownership.rows).toEqual([
          { id: "loan-1", owner_user_id: "user-alice" },
          { id: "loan-2", owner_user_id: "user-alice" },
        ]);
        const ownershipAudit = await testPool.query<{
          record_id: string;
          action: string;
        }>(
          `select record_id, action from engine.record_change_log
           where action_request_id = 'request-owner-backfill' order by record_id`,
        );
        expect(ownershipAudit.rows).toEqual([
          { record_id: "loan-1", action: "sync" },
          { record_id: "loan-2", action: "sync" },
        ]);
      } finally {
        await runCommand("docker", ["rm", "--force", containerId]).catch(() => undefined);
      }
    },
    45_000,
  );
});
