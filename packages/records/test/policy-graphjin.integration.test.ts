import pg from "pg";
import { execFile } from "node:child_process";
import { createHmac } from "node:crypto";
import { mkdtemp, writeFile } from "node:fs/promises";
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
  RecordsGraphjinRequestError,
} from "../src/graphjin/client";
import {
  buildRecordsWatchGraphjinConfig,
  createRecordsGraphjinConfigValidator,
  writeRecordsGraphjinConfig,
} from "../src/graphjin/config";
import {
  loadRecordsGraphjinPolicyModel,
  projectRecordsGraphjinRoles,
} from "../src/policy/graphjin";
import {
  buildRecordListQuery,
  buildRecordAggregateQuery,
  buildRecordRecycleListQuery,
  projectRecordRows,
} from "../src/read/query";
import { RecordRegistry } from "../src/registry";
import {
  buildRecordsStarterWatchDefinition,
  upsertRecordsNativeWatch,
} from "../src/watchers/starter";

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
const graphjinBinary = process.env.RECORDS_GRAPHJIN_BIN;
const graphjinImage = process.env.RECORDS_GRAPHJIN_IMAGE;
const itIfGraphjin = graphjinBinary || graphjinImage ? it : it.skip;
const itIfGraphjinImage = graphjinImage ? it : it.skip;
const LIVE_JWT_SECRET = "live-jwt-secret-that-is-at-least-thirty-two-bytes";

if (process.env.REQUIRE_GRAPHJIN_INTEGRATION === "1") {
  if (!reachable) throw new Error("required records Postgres is unreachable");
  if (!graphjinImage) throw new Error("required RECORDS_GRAPHJIN_IMAGE is not set");
}

function runTestCommand(command: string, args: string[]): Promise<{ stdout: string; stderr: string }> {
  return new Promise((resolve, reject) => {
    execFile(
      command,
      args,
      { timeout: 30_000, env: { ...process.env, GO_ENV: "development" } },
      (error, stdout, stderr) => {
        if (error) {
          reject(
            new Error(
              [String(stderr).trim(), String(stdout).trim(), error.message]
                .filter(Boolean)
                .join("\n"),
            ),
          );
        }
        else resolve({ stdout: String(stdout), stderr: String(stderr) });
      },
    );
  });
}

async function validateWithTestGraphjin(configDirectory: string): Promise<void> {
  if (graphjinImage) {
    const version = await runTestCommand("docker", [
      "run",
      "--rm",
      "--entrypoint",
      "graphjin",
      graphjinImage,
      "version",
    ]);
    expect(version.stdout).toContain("GraphJin 3.21.16");
    await runTestCommand("docker", [
      "run",
      "--rm",
      "--entrypoint",
      "graphjin",
      "-v",
      `${configDirectory}:/config:ro`,
      graphjinImage,
      "serve",
      "test",
      "--json",
      "--path",
      "/config",
    ]);
    return;
  }
  await createRecordsGraphjinConfigValidator({ binary: graphjinBinary! })({
    configDirectory,
    configFile: join(configDirectory, "dev.yml"),
  });
}

function graphjinJwt(userId: string): string {
  const encode = (value: unknown) =>
    Buffer.from(JSON.stringify(value), "utf8").toString("base64url");
  const unsigned = `${encode({ alg: "HS256", typ: "JWT" })}.${encode({
    sub: userId,
    iat: Math.floor(Date.now() / 1_000),
    exp: Math.floor(Date.now() / 1_000) + 300,
  })}`;
  const signature = createHmac("sha256", LIVE_JWT_SECRET)
    .update(unsigned)
    .digest("base64url");
  return `${unsigned}.${signature}`;
}

if (!reachable) {
  console.warn("[records-policy] skipping: records Postgres unreachable.");
}

describeIfRecordsDb("records GraphJin live-catalog policy integration", () => {
  const database = `records_policy_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`;
  let adminPool: pg.Pool;
  let testPool: pg.Pool;

  beforeAll(async () => {
    adminPool = new pg.Pool(buildRecordsPoolConfig());
    await adminPool.query(`create database ${database}`);
    testPool = new pg.Pool(buildRecordsPoolConfig(process.env, { database }));
    await runRecordsMigrations({ pool: testPool });
    await testPool.query(`
      create table public.equipment__loan (
        id text primary key,
        org_id text not null,
        data jsonb not null default '{}',
        owner_user_id text,
        nk_created_at timestamptz not null default now(),
        nk_updated_at timestamptz not null default now(),
        nk_deleted_at timestamptz
      );
      create table public.equipment__asset (
        id text primary key,
        org_id text not null,
        data jsonb not null default '{}',
        nk_created_at timestamptz not null default now(),
        nk_updated_at timestamptz not null default now(),
        nk_deleted_at timestamptz
      );
      create table public.unregistered_secret (id text primary key, secret text);
      create table public.crm__opportunity (
        id text primary key,
        org_id text not null,
        owner_user_id text,
        data jsonb not null default '{}',
        nk_updated_at timestamptz not null default now()
      );
      create table public.crm__activity (
        id text primary key,
        org_id text not null,
        data jsonb not null default '{}',
        nk_updated_at timestamptz not null default now()
      );
      insert into engine.record_app (org_id, app_id, label, status)
      values
        ('org-a', 'equipment', 'Equipment', 'active'),
        ('org-a', 'crm', 'CRM', 'active'),
        ('org-b', 'crm', 'Other CRM', 'active');
      insert into engine.record_object
        (id, org_id, app_id, api_name, label, plural_label, table_name,
         name_field, visibility)
      values
        ('00000000-0000-0000-0000-000000000201', 'org-a', 'equipment',
         'loan', 'Loan', 'Loans', 'equipment__loan', 'name', 'owner'),
        ('00000000-0000-0000-0000-000000000202', 'org-a', 'equipment',
         'asset', 'Asset', 'Assets', 'equipment__asset', 'name', 'org');
      insert into engine.record_field
        (org_id, object_id, api_name, label, kind, column_name, required)
      values
        ('org-a', '00000000-0000-0000-0000-000000000201',
         'name', 'Name', 'text', 'name', true),
        ('org-a', '00000000-0000-0000-0000-000000000201',
         'amount', 'Amount', 'currency', 'amount', false),
        ('org-a', '00000000-0000-0000-0000-000000000202',
         'name', 'Name', 'text', 'name', true);
      insert into engine.record_permission
        (org_id, app_id, role, object_api_name, can_read)
      values
        ('org-a', 'equipment', 'member', 'loan', true),
        ('org-a', 'equipment', 'admin', 'loan', true),
        ('org-a', 'equipment', 'member', 'asset', true),
        ('org-a', 'equipment', 'admin', 'asset', true);
      insert into engine.actor (org_id, user_id, role) values
        ('org-a', 'member-1', 'member'),
        ('org-a', 'member-2', 'member'),
        ('org-a', 'admin-1', 'admin'),
        ('org-a', 'service-1', 'service');
      insert into public.equipment__loan (id, org_id, data, owner_user_id) values
        ('loan-1', 'org-a', '{"name": "Member One Loan", "amount": 100}', 'member-1'),
        ('loan-2', 'org-a', '{"name": "Member Two Loan", "amount": 250}', 'member-2'),
        ('loan-deleted-1', 'org-a', '{"name": "Deleted Member One Loan", "amount": 500}', 'member-1'),
        ('loan-deleted-2', 'org-a', '{"name": "Deleted Member Two Loan", "amount": 750}', 'member-2');
      update public.equipment__loan
      set nk_deleted_at = '2026-08-01T10:00:00Z'
      where id like 'loan-deleted-%';
      insert into public.equipment__asset (id, org_id, data, nk_deleted_at)
      values ('asset-deleted', 'org-a', '{"name": "Deleted Shared Asset"}', '2026-08-01T11:00:00Z');
      insert into public.crm__opportunity (id, org_id, owner_user_id, data)
      values
        ('opportunity-stale', 'org-a', 'member-1',
         '{"name": "Stale opportunity", "stage": "proposal", "amount": 12000, "close_date": "2026-08-20"}'),
        ('opportunity-active', 'org-a', 'member-2',
         '{"name": "Active opportunity", "stage": "discovery", "amount": 5000, "close_date": "2026-08-22"}'),
        ('opportunity-cross-org', 'org-b', null,
         '{"name": "Other org opportunity", "stage": "proposal", "amount": 99999, "close_date": "2026-08-25"}');
      insert into public.crm__activity (id, org_id, data)
      values ('activity-current', 'org-a', jsonb_build_object(
        'opportunity', 'opportunity-active',
        'occurred_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')));
      insert into engine.record_change_log
        (org_id, app_id, object_api_name, record_id, action, mutation_id, changes, owner_user_id)
      values
        ('org-a', 'crm', 'opportunity', 'opportunity-stale', 'create',
         'watch-change-initial', '{}'::jsonb, 'member-1'),
        ('org-a', 'equipment', 'loan', 'loan-1', 'update',
         'loan-change-member-1', '{"name":{"old":"Old","new":"Member One Loan"}}'::jsonb,
         'member-1'),
        ('org-a', 'equipment', 'loan', 'loan-2', 'update',
         'loan-change-member-2', '{"name":{"old":"Old","new":"Member Two Loan"}}'::jsonb,
         'member-2');
      insert into engine.app_schema_log
        (org_id, app_id, action, detail, ddl, actor_user_id, action_request_id)
      values
        ('org-a', 'equipment', 'app_create', '{"effect":"Created equipment"}'::jsonb,
         '{"previewSql":"secret ddl"}'::jsonb, 'admin-1', 'schema-request-1');
      insert into engine.identity_map
        (org_id, source_instance_id, app_id, source_user_id, source_email,
         source_name, source_is_active, status)
      values
        ('org-a', 'salesforce-production', 'crm', 'source-owner-1',
         'departed@example.com', 'Departed owner', false, 'unlinked'),
        ('org-b', 'salesforce-production', 'crm', 'source-owner-other',
         'other@example.com', 'Other org owner', false, 'unlinked');
      insert into engine.recycle_record
        (org_id, app_id, object_api_name, visibility, record_id, record_name,
         owner_user_id, deleted_at, deleted_by, deletion_action_request_id)
      values
        ('org-a', 'equipment', 'loan', 'owner', 'loan-deleted-1',
         'Deleted Member One Loan', 'member-1', '2026-08-01T10:00:00Z',
         'member-1', 'delete-request-1'),
        ('org-a', 'equipment', 'loan', 'owner', 'loan-deleted-2',
         'Deleted Member Two Loan', 'member-2', '2026-08-01T10:30:00Z',
         'member-2', 'delete-request-2'),
        ('org-a', 'equipment', 'asset', 'org', 'asset-deleted',
         'Deleted Shared Asset', null, '2026-08-01T11:00:00Z',
         'admin-1', 'delete-request-3');
    `);
  });

  afterAll(async () => {
    if (testPool) await testPool.end();
    if (adminPool) {
      await adminPool.query(`drop database if exists ${database}`);
      await adminPool.end();
    }
  });

  it("uses the live catalog as the exhaustive universe", async () => {
    const model = await loadRecordsGraphjinPolicyModel(testPool, "org-a");
    const roles = projectRecordsGraphjinRoles(model);
    const liveRelations = model.catalog.map((table) => `${table.schema}.${table.name}`);

    expect(liveRelations).toContain("public.equipment__loan");
    expect(liveRelations).toContain("public.unregistered_secret");
    expect(liveRelations).toContain("engine.actor");
    for (const role of roles) expect(role.tables).toHaveLength(model.catalog.length);

    const member = roles.find((role) => role.name === "member")!;
    expect(member.tables.find((table) => table.name === "equipment__loan")?.query).toMatchObject({
      block: false,
      filters: expect.arrayContaining(["{ owner_user_id: { eq: $user_id } }"]),
    });
    expect(member.tables.find((table) => table.name === "unregistered_secret")?.query.block).toBe(
      true,
    );
    expect(member.tables.find((table) => table.name === "actor")?.query.block).toBe(true);
  });

  itIfGraphjin(
    "loads the complete config in pinned GraphJin",
    async () => {
      const model = await loadRecordsGraphjinPolicyModel(testPool, "org-a");
      const directory = await mkdtemp(join(tmpdir(), "records-graphjin-live-"));
      const poolConfig = buildRecordsPoolConfig(process.env, { database });
      const connection = new URL("postgres://localhost/records");
      connection.hostname = graphjinImage
        ? (process.env.RECORDS_GRAPHJIN_DB_HOST ?? "host.internal")
        : String(poolConfig.host);
      connection.port = String(poolConfig.port);
      connection.username = String(poolConfig.user);
      connection.password = String(poolConfig.password ?? "");
      connection.pathname = `/${database}`;
      connection.searchParams.set("sslmode", "disable");

      await expect(
        writeRecordsGraphjinConfig({
          configFile: join(directory, "dev.yml"),
          config: {
            orgId: "org-a",
            roles: projectRecordsGraphjinRoles(model),
            database: { connectionString: connection.toString() },
            jwt: { secret: LIVE_JWT_SECRET },
            secretKey: "live-cursor-secret-that-is-at-least-thirty-two-bytes",
          },
          validate: async ({ configDirectory }) =>
            validateWithTestGraphjin(configDirectory),
          reloadRecordsGraphjin: async () => {},
        }),
      ).resolves.toMatchObject({ sha256: expect.stringMatching(/^[0-9a-f]{64}$/) });
    },
    40_000,
  );

  itIfGraphjinImage(
    "creates, evaluates, and advances a durable native watch live",
    async () => {
      const directory = await mkdtemp(join(tmpdir(), "records-watch-graphjin-runtime-"));
      const poolConfig = buildRecordsPoolConfig(process.env, { database });
      const connection = new URL("postgres://localhost/records");
      connection.hostname = process.env.RECORDS_GRAPHJIN_DB_HOST ?? "host.internal";
      connection.port = String(poolConfig.port);
      connection.username = String(poolConfig.user);
      connection.password = String(poolConfig.password ?? "");
      connection.pathname = `/${database}`;
      connection.searchParams.set("sslmode", "disable");
      const config = buildRecordsWatchGraphjinConfig({
        orgId: "org-a",
        database: { connectionString: connection.toString() },
        jwt: { secret: LIVE_JWT_SECRET },
        secretKey: "live-watch-runtime-cursor-secret-at-least-thirty-two-bytes",
      })
        .replace("subs_poll_duration: 30s", "subs_poll_duration: 1s")
        .replace("poll_seconds: 5", "poll_seconds: 1");
      await writeFile(join(directory, "dev.yml"), config, { mode: 0o600 });
      await validateWithTestGraphjin(directory);

      const started = await runTestCommand("docker", [
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
        const portOutput = await runTestCommand("docker", ["port", containerId, "8090/tcp"]);
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
        if (!healthy) throw new Error("records watch GraphJin did not become healthy");

        const graphjin = new RecordsGraphjinClient({
          baseUrl: `http://127.0.0.1:${port}`,
        });
        const token = mintRecordsGraphjinToken({
          secret: LIVE_JWT_SECRET,
          orgId: "org-a",
          userId: "records-watch-service",
          role: "service",
        });
        const createNativeWatch = async (
          key:
            | "opportunities_without_activity"
            | "unlinked_or_departed_owners"
            | "deals_closing_this_month",
        ) => {
          const definition = buildRecordsStarterWatchDefinition({
            orgId: "org-a",
            appId: "crm",
            key,
          });
          try {
            return await upsertRecordsNativeWatch({
              graphjin,
              token,
              definition,
              approvedActionHash: "a".repeat(64),
            });
          } catch (error) {
            if (error instanceof RecordsGraphjinRequestError) {
              throw new Error(JSON.stringify(error.graphjinErrors));
            }
            throw error;
          }
        };
        // Keep only one runner active while proving cursor advancement. The
        // other definitions are created after that proof so suite load cannot
        // turn a correctness assertion into a polling/rate-limit race.
        const watch = await createNativeWatch("opportunities_without_activity");
        expect(watch).toMatchObject({
          id: expect.any(String),
          definitionHash: expect.stringMatching(/^[0-9a-f]{64}$/),
        });

        const evaluation = await graphjin.execute<{
          opportunities: Array<{ id: string }>;
          activities: Array<{ data: { opportunity: string } }>;
        }>({
          operationName: "EvaluateRecordsStaleOpportunities",
          query: `query EvaluateRecordsStaleOpportunities { opportunities: crm__opportunity(limit: 500) { id owner_user_id data } activities: crm__activity(where: { data: { occurred_at: { gte: $cutoff } } }, limit: 500) { id data } }`,
          variables: { cutoff: "2026-07-03T12:00:00.000Z" },
          token,
        });
        expect(evaluation.opportunities.map((row) => row.id).sort()).toEqual([
          "opportunity-active",
          "opportunity-stale",
        ]);
        expect(evaluation.activities.map((row) => row.data.opportunity)).toEqual([
          "opportunity-active",
        ]);
        const identities = await graphjin.execute<{
          identities: Array<{ source_user_id: string; source_is_active: boolean }>;
        }>({
          operationName: "EvaluateRecordsOwnerMappings",
          query: `query EvaluateRecordsOwnerMappings($app_id: String!) { identities: identity_map(where: { app_id: { eq: $app_id } }, limit: 500) { source_instance_id source_user_id source_email source_name source_is_active app_user_id status updated_at } }`,
          variables: { app_id: "crm" },
          token,
        });
        expect(identities.identities).toEqual([
          expect.objectContaining({
            source_user_id: "source-owner-1",
            source_is_active: false,
          }),
        ]);
        const closing = await graphjin.execute<{
          opportunities: Array<{ id: string; data: { close_date: string } }>;
        }>({
          operationName: "EvaluateRecordsClosingDeals",
          query: `query EvaluateRecordsClosingDeals { opportunities: crm__opportunity(limit: 500) { id owner_user_id data } }`,
          token,
        });
        expect(closing.opportunities).toHaveLength(2);

        const waitForEventCount = async (minimum: number): Promise<number> => {
          let count = 0;
          for (let attempt = 0; attempt < 300; attempt += 1) {
            const result = await testPool.query<{ count: string }>(
              "select count(*) from _graphjin.watch_events where watch_id = $1",
              [watch.id],
            );
            count = Number(result.rows[0]?.count ?? 0);
            if (count >= minimum) return count;
            await new Promise((resolve) => setTimeout(resolve, 100));
          }
          return count;
        };
        const initialCount = await waitForEventCount(1);
        expect(initialCount).toBeGreaterThanOrEqual(1);
        await testPool.query(
          `insert into engine.record_change_log
             (org_id, app_id, object_api_name, record_id, action, mutation_id, changes)
           values ('org-a', 'crm', 'activity', 'activity-new', 'create',
                   'watch-change-new', '{}'::jsonb)`,
        );
        const advancedCount = await waitForEventCount(initialCount + 1);
        expect(advancedCount).toBeGreaterThan(initialCount);
        const [stored] = (
          await testPool.query<{
            account_id: string;
            approval: string;
            last_cursor_json: string | null;
          }>(
            `select account_id, approval, last_cursor_json
             from _graphjin.watches where id = $1`,
            [watch.id],
          )
        ).rows;
        expect(stored).toMatchObject({
          account_id: "org-a",
          approval: "approved",
          last_cursor_json: expect.any(String),
        });
        const cursors = JSON.parse(stored!.last_cursor_json!) as Record<string, string>;
        expect(cursors.record_change_log_cursor).toEqual(expect.any(String));
        const remainingWatches = await Promise.all([
          createNativeWatch("unlinked_or_departed_owners"),
          createNativeWatch("deals_closing_this_month"),
        ]);
        expect([watch, ...remainingWatches]).toHaveLength(3);
      } finally {
        await runTestCommand("docker", ["rm", "--force", containerId]).catch(
          () => undefined,
        );
      }
    },
    60_000,
  );

  itIfGraphjin(
    "loads the isolated native watch config in pinned GraphJin",
    async () => {
      const directory = await mkdtemp(join(tmpdir(), "records-watch-graphjin-live-"));
      const poolConfig = buildRecordsPoolConfig(process.env, { database });
      const connection = new URL("postgres://localhost/records");
      connection.hostname = graphjinImage
        ? (process.env.RECORDS_GRAPHJIN_DB_HOST ?? "host.internal")
        : String(poolConfig.host);
      connection.port = String(poolConfig.port);
      connection.username = String(poolConfig.user);
      connection.password = String(poolConfig.password ?? "");
      connection.pathname = `/${database}`;
      connection.searchParams.set("sslmode", "disable");
      await writeFile(
        join(directory, "dev.yml"),
        buildRecordsWatchGraphjinConfig({
          orgId: "org-a",
          database: { connectionString: connection.toString() },
          jwt: { secret: LIVE_JWT_SECRET },
          secretKey: "live-watch-cursor-secret-that-is-at-least-thirty-two-bytes",
          watchWebhookAllow: [
            "http://172.20.0.8:4100/admin/events/records-watch",
          ],
        }),
        { mode: 0o600 },
      );
      await expect(validateWithTestGraphjin(directory)).resolves.toBeUndefined();
    },
    40_000,
  );

  itIfGraphjinImage(
    "enforces owner reads, fallback denial, and worker-only mutations live",
    async () => {
      const model = await loadRecordsGraphjinPolicyModel(testPool, "org-a");
      const directory = await mkdtemp(join(tmpdir(), "records-graphjin-rbac-"));
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
          roles: projectRecordsGraphjinRoles(model),
          database: { connectionString: connection.toString() },
          jwt: { secret: LIVE_JWT_SECRET },
          secretKey: "live-cursor-secret-that-is-at-least-thirty-two-bytes",
        },
        validate: async ({ configDirectory }) =>
          validateWithTestGraphjin(configDirectory),
        reloadRecordsGraphjin: async () => {},
      });

      const started = await runTestCommand("docker", [
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
        const portOutput = await runTestCommand("docker", [
          "port",
          containerId,
          "8090/tcp",
        ]);
        const port = Number.parseInt(portOutput.stdout.trim().split(":").at(-1) ?? "", 10);
        expect(port).toBeGreaterThan(0);
        const endpoint = `http://127.0.0.1:${port}/api/v1/graphql`;

        let healthy = false;
        for (let attempt = 0; attempt < 50; attempt += 1) {
          try {
            const response = await fetch(`http://127.0.0.1:${port}/health`);
            if (response.ok) {
              healthy = true;
              break;
            }
          } catch {
            // Container is still starting.
          }
          await new Promise((resolve) => setTimeout(resolve, 100));
        }
        if (!healthy) {
          const logs = await runTestCommand("docker", ["logs", containerId]);
          throw new Error(`records GraphJin did not become healthy: ${logs.stderr}`);
        }

        const request = async (
          userId: string,
          query: string,
          variables: Record<string, unknown> = {},
        ) => {
          const response = await fetch(endpoint, {
            method: "POST",
            headers: {
              authorization: `Bearer ${graphjinJwt(userId)}`,
              "content-type": "application/json",
            },
            body: JSON.stringify({ query, variables }),
          });
          return (await response.json()) as {
            data?: Record<string, unknown>;
            errors?: unknown[];
          };
        };

        const member = await request(
          "member-1",
          "query MemberLoans { equipment__loan(order_by: { id: asc }) { id data } }",
        );
        expect(member.errors).toBeUndefined();
        expect(member.data?.equipment__loan).toEqual([
          { id: "loan-1", data: { name: "Member One Loan", amount: 100 } },
        ]);

        const memberHistory = await request(
          "member-1",
          "query MemberHistory { record_change_log(order_by: { id: asc }) { record_id action changes } }",
        );
        expect(memberHistory.errors).toBeUndefined();
        expect(memberHistory.data?.record_change_log).toEqual([
          {
            record_id: "loan-1",
            action: "update",
            changes: { name: { old: "Old", new: "Member One Loan" } },
          },
        ]);
        const memberSchemaHistory = await request(
          "member-1",
          "query MemberSchemaHistory { app_schema_log { action detail } }",
        );
        expect(memberSchemaHistory.data?.app_schema_log ?? []).toEqual([]);
        expect(JSON.stringify(memberSchemaHistory)).not.toContain("Created equipment");

        const admin = await request(
          "admin-1",
          "query AdminLoans { equipment__loan(order_by: { id: asc }) { id data } }",
        );
        expect(admin.errors).toBeUndefined();
        expect(admin.data?.equipment__loan).toEqual([
          { id: "loan-1", data: { name: "Member One Loan", amount: 100 } },
          { id: "loan-2", data: { name: "Member Two Loan", amount: 250 } },
        ]);
        const adminHistory = await request(
          "admin-1",
          "query AdminHistory { record_change_log(order_by: { id: asc }) { record_id } }",
        );
        expect(adminHistory.errors).toBeUndefined();
        expect(adminHistory.data?.record_change_log).toEqual([
          { record_id: "loan-1" },
          { record_id: "loan-2" },
        ]);
        const adminSchemaHistory = await request(
          "admin-1",
          "query AdminSchemaHistory { app_schema_log { app_id action detail actor_user_id } }",
        );
        expect(adminSchemaHistory.errors).toBeUndefined();
        expect(adminSchemaHistory.data?.app_schema_log).toEqual([
          {
            app_id: "equipment",
            action: "app_create",
            detail: { effect: "Created equipment" },
            actor_user_id: "admin-1",
          },
        ]);
        expect(JSON.stringify(adminSchemaHistory)).not.toContain("secret ddl");

        const memberRecycle = await request(
          "member-1",
          "query MemberRecycle { recycle_record(order_by: { deleted_at: asc }) { app_id object_api_name record_id record_name owner_user_id } }",
        );
        expect(memberRecycle.errors).toBeUndefined();
        expect(memberRecycle.data?.recycle_record).toEqual([
          {
            app_id: "equipment",
            object_api_name: "loan",
            record_id: "loan-deleted-1",
            record_name: "Deleted Member One Loan",
            owner_user_id: "member-1",
          },
          {
            app_id: "equipment",
            object_api_name: "asset",
            record_id: "asset-deleted",
            record_name: "Deleted Shared Asset",
            owner_user_id: null,
          },
        ]);
        expect(JSON.stringify(memberRecycle)).not.toContain("loan-deleted-2");

        const recycleRegistry = await new RecordRegistry(testPool).loadApp(
          "org-a",
          "equipment",
        );
        expect(recycleRegistry).not.toBeNull();
        const generatedRecycle = buildRecordRecycleListQuery({
          snapshot: recycleRegistry!,
          objectApiName: "loan",
          role: "member",
          first: 1,
        });
        const generatedRecyclePage = await request(
          "member-1",
          generatedRecycle.query,
          generatedRecycle.variables,
        );
        expect(generatedRecyclePage.errors).toBeUndefined();
        expect(generatedRecyclePage.data?.rows).toEqual([
          expect.objectContaining({ record_id: "loan-deleted-1" }),
        ]);
        expect(generatedRecyclePage.data?.[generatedRecycle.cursorField]).toEqual(
          expect.any(String),
        );
        expect(generatedRecyclePage.data?.totals).toEqual([{ count: 1 }]);

        const adminRecycle = await request(
          "admin-1",
          "query AdminRecycle { recycle_record(order_by: { deleted_at: asc }) { record_id } }",
        );
        expect(adminRecycle.errors).toBeUndefined();
        expect(adminRecycle.data?.recycle_record).toEqual([
          { record_id: "loan-deleted-1" },
          { record_id: "loan-deleted-2" },
          { record_id: "asset-deleted" },
        ]);

        const serviceRecycle = await request(
          "service-1",
          "query ServiceRecycle { recycle_record { record_id } }",
        );
        expect(serviceRecycle.data?.recycle_record ?? []).toEqual([]);
        expect(JSON.stringify(serviceRecycle)).not.toContain("loan-deleted-1");

        const registry = await new RecordRegistry(testPool).loadApp("org-a", "equipment");
        expect(registry).not.toBeNull();
        const generated = buildRecordListQuery({
          snapshot: registry!,
          objectApiName: "loan",
          role: "admin",
          userId: "admin-1",
          first: 1,
          search: "Loan",
        });
        const generatedPage = await request(
          "admin-1",
          generated.query,
          generated.variables,
        );
        expect(generatedPage.errors).toBeUndefined();
        expect(
          projectRecordRows(generated.view, generatedPage.data?.rows as Array<Record<string, unknown>>),
        ).toEqual([
          {
            id: "loan-1",
            name: "Member One Loan",
            amount: 100,
            owner_user_id: "member-1",
          },
        ]);
        expect(generatedPage.data?.[generated.cursorField]).toEqual(expect.any(String));
        expect(generatedPage.data?.totals).toEqual([{ count_id: 2 }]);

        const generatedMetric = buildRecordAggregateQuery({
          snapshot: registry!,
          objectApiName: "loan",
          role: "admin",
          aggregate: "sum",
          field: "amount",
        });
        const metric = await request(
          "admin-1",
          generatedMetric.query,
          generatedMetric.variables,
        );
        expect(metric.errors).toBeUndefined();
        expect(metric.data?.metric).toEqual([{ total: 350 }]);

        const missingActor = await request(
          "missing-actor",
          "query MissingActorLoans { equipment__loan { id data } }",
        );
        expect(missingActor.data?.equipment__loan ?? []).toEqual([]);
        expect(JSON.stringify(missingActor)).not.toContain("Member One Loan");
        expect(JSON.stringify(missingActor)).not.toContain("Member Two Loan");

        const memberMutation = await request(
          "member-1",
          "mutation MemberCreateLoan { equipment__loan(insert: $data) { id } }",
          { data: { id: "loan-member-blocked", data: { name: "Blocked" } } },
        );
        expect(memberMutation.errors?.length).toBeGreaterThan(0);
        const blockedMemberWrite = await testPool.query<{ count: string }>(
          "select count(*) from public.equipment__loan where id = 'loan-member-blocked'",
        );
        expect(blockedMemberWrite.rows).toEqual([{ count: "0" }]);

        const serviceMutation = await request(
          "service-1",
          "mutation ServiceCreateLoan { equipment__loan(insert: $data) { id data } }",
          {
            data: {
              id: "loan-service",
              data: { name: "Service Created" },
              owner_user_id: "member-1",
            },
          },
        );
        expect(serviceMutation.errors).toBeUndefined();
        expect(serviceMutation.data?.equipment__loan).toEqual([
          {
            id: "loan-service",
            data: { name: "Service Created" },
          },
        ]);
        const inserted = await testPool.query<{ org_id: string }>(
          "select org_id from public.equipment__loan where id = 'loan-service'",
        );
        expect(inserted.rows).toEqual([{ org_id: "org-a" }]);

        await request(
          "service-1",
          "mutation ServiceCannotChooseOrg { equipment__loan(insert: $data) { id } }",
          {
            data: {
              id: "loan-service-forced-org",
              data: { name: "Attempted Cross-org Write" },
              org_id: "org-b",
              owner_user_id: "member-1",
            },
          },
        );
        const crossOrgRows = await testPool.query<{ org_id: string }>(
          "select org_id from public.equipment__loan where org_id <> 'org-a'",
        );
        expect(crossOrgRows.rows).toEqual([]);
      } finally {
        await runTestCommand("docker", ["rm", "--force", containerId]).catch(
          () => undefined,
        );
      }
    },
    40_000,
  );
});
