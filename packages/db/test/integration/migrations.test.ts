/**
 * Migration tests against a real Postgres.
 *
 * Each test creates a fresh, empty database (via the admin connection),
 * applies the schema migrations against it, asserts behaviour, then drops it.
 * A temp database — not just a schema — is the only way the migration's
 * global pg_constraint guard runs in isolation.
 */

import { readFile } from "node:fs/promises";
import { join } from "node:path";
import pg from "pg";
import { afterAll, describe, expect, it } from "vitest";
import { dbReachable } from "./_helpers";
import { buildPoolConfig } from "../../src/connection";
import { pool } from "../../src";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[migrations] skipping: Postgres unreachable.");
}

const REPO_ROOT = join(__dirname, "..", "..", "..", "..");
const M_0001 = join(REPO_ROOT, "db", "migrations", "0001_init.sql");
const M_0002 = join(REPO_ROOT, "db", "migrations", "0002_agent_backend_and_setup.sql");
const M_0003 = join(REPO_ROOT, "db", "migrations", "0003_rename_claude_sdk_to_claude_agent.sql");
const M_0004 = join(REPO_ROOT, "db", "migrations", "0004_drop_organization_plan.sql");
const M_0005 = join(REPO_ROOT, "db", "migrations", "0005_metric_refresh_status.sql");
const M_0006 = join(REPO_ROOT, "db", "migrations", "0006_work_runtime.sql");
const M_0007 = join(REPO_ROOT, "db", "migrations", "0007_work_memory.sql");
const M_0009 = join(REPO_ROOT, "db", "migrations", "0009_workflows.sql");
const M_0010 = join(REPO_ROOT, "db", "migrations", "0010_subscriptions_observations.sql");
const M_0011 = join(REPO_ROOT, "db", "migrations", "0011_action_stack.sql");
const M_0019 = join(REPO_ROOT, "db", "migrations", "0019_install_policy_scope.sql");
const M_0048 = join(REPO_ROOT, "db", "migrations", "0048_graphjin_config_scope.sql");
const M_0110 = join(REPO_ROOT, "db", "migrations", "0110_graphjin_agent_scope.sql");
const M_0051 = join(REPO_ROOT, "db", "migrations", "0051_app_state.sql");
const M_0062 = join(REPO_ROOT, "db", "migrations", "0062_hermes_only_agent.sql");
const M_0063 = join(
  REPO_ROOT,
  "db",
  "migrations",
  "0063_durable_workflow_scheduler.sql",
);
const M_0064 = join(
  REPO_ROOT,
  "db",
  "migrations",
  "0064_magento_store_management_v2.sql",
);
const M_0065 = join(REPO_ROOT, "db", "migrations", "0065_skill_usage.sql");
const M_0066 = join(REPO_ROOT, "db", "migrations", "0066_skill_learn.sql");
const M_0067 = join(REPO_ROOT, "db", "migrations", "0067_workflow_api.sql");
const M_0122 = join(REPO_ROOT, "db", "migrations", "0122_ax_only_agent.sql");

function uniqueDbName(): string {
  return `vitest_migrations_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`;
}


async function withTempDb<T>(
  fn: (client: pg.Client) => Promise<T>,
): Promise<T> {
  const adminClient = await pool().connect();
  const dbName = uniqueDbName();
  try {
    await adminClient.query(`create database ${dbName}`);
  } finally {
    adminClient.release();
  }

  const tempClient = new pg.Client(buildPoolConfig({ database: dbName }));
  await tempClient.connect();
  try {
    return await fn(tempClient);
  } finally {
    await tempClient.end();
    const cleanup = await pool().connect();
    try {
      await cleanup.query(`drop database if exists ${dbName}`);
    } finally {
      cleanup.release();
    }
  }
}

async function applyFile(client: pg.Client, path: string) {
  const sql = await readFile(path, "utf8");
  await client.query(sql);
}

async function applyAll(client: pg.Client) {
  for (const path of [M_0001, M_0002, M_0003, M_0004, M_0005, M_0006, M_0007]) {
    await applyFile(client, path);
  }
}

describeIfDb("schema migrations", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("0001 creates organization + every expected sibling", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      const tables = await client.query<{ table_name: string }>(
        `select table_name from information_schema.tables
         where table_schema = 'public' order by table_name`,
      );
      const names = tables.rows.map((r) => r.table_name);
      for (const expected of [
        "organization",
        "data_source",
        "onboarding_wizard",
        "processing_job",
        "metric",
        "metric_snapshot",
        "llm_provider_config",
      ]) {
        expect(names, `missing ${expected}`).toContain(expected);
      }
    });
  });

  it("0002 adds setup_complete_at + scope check constraint", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0002);

      const cols = await client.query<{ column_name: string }>(
        `select column_name from information_schema.columns
         where table_schema = 'public' and table_name = 'organization'`,
      );
      expect(cols.rows.map((r) => r.column_name)).toContain("setup_complete_at");

      const constraints = await client.query<{ conname: string }>(
        `select con.conname
         from pg_constraint con
         join pg_class t on t.oid = con.conrelid
         join pg_namespace n on n.oid = t.relnamespace
         where n.nspname = 'public'
           and t.relname = 'llm_provider_config'`,
      );
      expect(constraints.rows.map((r) => r.conname)).toContain(
        "llm_provider_config_scope_check",
      );
    });
  });

  it("0002 is idempotent (re-applying succeeds without error)", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0002);
      await applyFile(client, M_0002); // second apply must succeed
    });
  });

  it("0002 backfills setup_complete_at for orgs with data + enabled primary", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);

      // Seed prerequisites BEFORE 0002 runs.
      await client.query(`
        insert into organization (id, name) values ('test-backfill', 'Backfill Test');
        insert into data_source (org_id, kind, graphql_url) values ('test-backfill', 'graphjin', 'http://example.com/graphql');
        insert into llm_provider_config (org_id, scope, provider, enabled) values ('test-backfill', 'primary', 'anthropic', true);
      `);

      await applyFile(client, M_0002);

      const result = await client.query<{ setup_complete_at: Date | null }>(
        `select setup_complete_at from organization where id = 'test-backfill'`,
      );
      expect(result.rows[0].setup_complete_at).not.toBeNull();
    });
  });

  it("0002 does NOT backfill orgs without prerequisites", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await client.query(`insert into organization (id, name) values ('no-prereqs', 'No prereqs')`);
      await applyFile(client, M_0002);
      const result = await client.query<{ setup_complete_at: Date | null }>(
        `select setup_complete_at from organization where id = 'no-prereqs'`,
      );
      expect(result.rows[0].setup_complete_at).toBeNull();
    });
  });

  it("scope check constraint rejects unknown values", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0002);

      await client.query(`insert into organization (id, name) values ('chk', 'Check Test')`);
      await expect(
        client.query(
          `insert into llm_provider_config (org_id, scope, provider) values ('chk', 'bogus-scope', 'anthropic')`,
        ),
      ).rejects.toThrow(/check constraint|scope/i);
    });
  });

  it("scope check accepts the three known scopes", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0002);

      await client.query(`insert into organization (id, name) values ('chk2', 'Check Test 2')`);
      for (const scope of ["primary", "research", "agent"]) {
        await client.query(
          `insert into llm_provider_config (org_id, scope, provider) values ($1, $2, 'anthropic')`,
          ["chk2", scope],
        );
      }
    });
  });

  it("0110 extends the scope check for the GraphJin agent opt-in", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0002);
      await applyFile(client, M_0048);
      await applyFile(client, M_0110);

      await client.query(`insert into organization (id, name) values ('chk110', 'Check Test 110')`);
      for (const scope of ["primary", "research", "agent", "install-policy", "graphjin-config", "graphjin-agent"]) {
        await client.query(
          `insert into llm_provider_config (org_id, scope, provider) values ($1, $2, 'settings')`,
          ["chk110", scope],
        );
      }
    });
  });

  it("0048 extends the scope check for GraphJin config settings", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0002);
      await applyFile(client, M_0019);
      await applyFile(client, M_0048);

      await client.query(`insert into organization (id, name) values ('chk48', 'Check Test 48')`);
      for (const scope of [
        "primary",
        "research",
        "agent",
        "install-policy",
        "graphjin-config",
      ]) {
        await client.query(
          `insert into llm_provider_config (org_id, scope, provider) values ($1, $2, 'settings')`,
          ["chk48", scope],
        );
      }
    });
  });

  it("0051 creates the records app lifecycle mirror with safe defaults", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0051);

      await client.query(`
        insert into organization (id, name) values ('records-org', 'Records Org');
        insert into app_user (id, org_id, email, role)
        values ('records-admin', 'records-org', 'admin@example.com', 'admin');
        insert into app_state (org_id, app_id, created_by)
        values ('records-org', 'equipment', 'records-admin');
      `);

      const state = await client.query<{
        status: string;
        config: Record<string, unknown>;
      }>(
        `select status, config from app_state
         where org_id = 'records-org' and app_id = 'equipment'`,
      );
      expect(state.rows).toEqual([{ status: "draft", config: {} }]);

      await expect(
        client.query(`
          insert into app_state (org_id, app_id, status)
          values ('records-org', 'invalid', 'unknown')
        `),
      ).rejects.toThrow(/check constraint|status/i);

      await client.query(`delete from app_user where id = 'records-admin'`);
      const owner = await client.query<{ created_by: string | null }>(
        `select created_by from app_state
         where org_id = 'records-org' and app_id = 'equipment'`,
      );
      expect(owner.rows[0]?.created_by).toBeNull();
    });
  });

  it("0062 converts only agent runtime state to Hermes and is idempotent", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0002);
      await client.query(`
        insert into organization (id, name) values ('hermes-upgrade', 'Hermes Upgrade');
        insert into llm_provider_config
          (org_id, scope, provider, model, enabled, config, secrets)
        values
          ('hermes-upgrade', 'primary', 'anthropic', 'claude-sonnet-4-5', true,
           '{"baseUrl":"https://api.anthropic.com"}', '{"apiKey":"encrypted-primary"}'),
          ('hermes-upgrade', 'agent', 'claude-agent', 'legacy-agent-model', false,
           '{"backend":"claude-agent","globalCap":7,"claudeAgentCap":2,"futureSetting":"keep"}',
           '{"apiKey":"legacy-agent-secret"}');
      `);

      await applyFile(client, M_0062);
      await applyFile(client, M_0062);

      const result = await client.query<{
        scope: string;
        provider: string;
        model: string | null;
        enabled: boolean;
        config: Record<string, unknown>;
        secrets: Record<string, unknown>;
      }>(`
        select scope, provider, model, enabled, config, secrets
        from llm_provider_config
        where org_id = 'hermes-upgrade'
        order by scope
      `);

      expect(result.rows).toEqual([
        {
          scope: "agent",
          provider: "hermes",
          model: null,
          enabled: true,
          config: { globalCap: 7, futureSetting: "keep" },
          secrets: {},
        },
        {
          scope: "primary",
          provider: "anthropic",
          model: "claude-sonnet-4-5",
          enabled: true,
          config: { baseUrl: "https://api.anthropic.com" },
          secrets: { apiKey: "encrypted-primary" },
        },
      ]);
    });
  });

  it("0122 moves agent rows and queued runs to Ax and is idempotent", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0002);
      await applyFile(client, M_0006);
      await client.query(`
        insert into organization (id, name) values ('ax-upgrade', 'Ax Upgrade');
        insert into llm_provider_config
          (org_id, scope, provider, model, enabled, config, secrets)
        values
          ('ax-upgrade', 'primary', 'google-gemini', 'gemini-3.6-flash', true,
           '{}', '{"apiKey":"encrypted-primary"}'),
          ('ax-upgrade', 'agent', 'legacy-runtime', null, true,
           '{"backend":"legacy-runtime","globalCap":7}', '{}');
        insert into work_thread (id, org_id, title)
        values ('00000000-0000-0000-0000-000000000119', 'ax-upgrade', 'upgrade');
        insert into work_run (org_id, thread_id, backend, status)
        values
          ('ax-upgrade', '00000000-0000-0000-0000-000000000119', 'legacy-runtime', 'queued'),
          ('ax-upgrade', '00000000-0000-0000-0000-000000000119', 'legacy-runtime', 'completed');
      `);

      await applyFile(client, M_0122);
      await applyFile(client, M_0122);

      const providers = await client.query<{
        scope: string;
        provider: string;
        config: Record<string, unknown>;
        secrets: Record<string, unknown>;
      }>(`
        select scope, provider, config, secrets
        from llm_provider_config
        where org_id = 'ax-upgrade'
        order by scope
      `);
      expect(providers.rows).toEqual([
        { scope: "agent", provider: "ax", config: { globalCap: 7 }, secrets: {} },
        {
          scope: "primary",
          provider: "google-gemini",
          config: {},
          secrets: { apiKey: "encrypted-primary" },
        },
      ]);

      const runs = await client.query<{ status: string; backend: string }>(`
        select status, backend from work_run where org_id = 'ax-upgrade' order by status
      `);
      expect(runs.rows).toEqual([
        { status: "completed", backend: "legacy-runtime" },
        { status: "queued", backend: "ax" },
      ]);
    });
  });

  it("0063 creates an idempotent workflow cursor, firing ledger, and heartbeat", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0006);
      await applyFile(client, M_0009);
      await applyFile(client, M_0063);
      await applyFile(client, M_0063);

      const tables = await client.query<{ table_name: string }>(
        `select table_name from information_schema.tables
         where table_schema = 'public'
           and table_name like 'workflow_schedule%'
         order by table_name`,
      );
      expect(tables.rows.map((row) => row.table_name)).toEqual([
        "workflow_schedule_firing",
        "workflow_schedule_state",
        "workflow_scheduler_health",
      ]);

      await client.query(`
        insert into organization (id, name) values ('scheduler-org', 'Scheduler');
        insert into workflow_definition (id, org_id, name, steps, cron)
        values ('00000000-0000-0000-0000-000000000063', 'scheduler-org',
                'Daily briefing', '[]'::jsonb, '0 9 * * *');
        insert into workflow_schedule_firing
          (org_id, workflow_id, scheduled_for)
        values
          ('scheduler-org', '00000000-0000-0000-0000-000000000063',
           '2030-08-26T09:00:00Z');
      `);
      await expect(
        client.query(`
          insert into workflow_schedule_firing
            (org_id, workflow_id, scheduled_for)
          values
            ('scheduler-org', '00000000-0000-0000-0000-000000000063',
             '2030-08-26T09:00:00Z')
        `),
      ).rejects.toThrow(/unique|duplicate/i);
      await expect(
        client.query(`
          update workflow_schedule_firing set status = 'lost'
          where workflow_id = '00000000-0000-0000-0000-000000000063'
        `),
      ).rejects.toThrow(/check constraint|status/i);
    });
  });

  it("0064 creates governed Magento change-sets and keeps handoff-only work out of execution", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0006);
      await applyFile(client, M_0009);
      await applyFile(client, M_0010);
      await applyFile(client, M_0011);
      await applyFile(client, M_0064);
      await applyFile(client, M_0064);

      const tables = await client.query<{ table_name: string }>(`
        select table_name from information_schema.tables
         where table_schema = 'public'
           and table_name in (
             'action_changeset', 'action_changeset_row',
             'magento_store_control', 'magento_attribute_classification',
             'magento_auto_rule', 'magento_financial_handoff'
           )
         order by table_name
      `);
      expect(tables.rows.map((row) => row.table_name)).toEqual([
        "action_changeset",
        "action_changeset_row",
        "magento_attribute_classification",
        "magento_auto_rule",
        "magento_financial_handoff",
        "magento_store_control",
      ]);

      await client.query(`
        insert into organization (id, name) values ('magento-v2-org', 'Magento V2');
        insert into action_request (id, org_id, scope, kind, status)
        values (
          '00000000-0000-0000-0000-000000000064',
          'magento-v2-org', 'external', 'magento.manage_catalog', 'approved'
        );
        insert into action_changeset
          (id, org_id, action_request_id, domain, operation_id, risk_class,
           status, summary, idempotency_key)
        values (
          '10000000-0000-0000-0000-000000000064',
          'magento-v2-org', '00000000-0000-0000-0000-000000000064',
          'catalog', 'product_update', 2, 'approved', 'Rename product', 'fixture-0064'
        );
        insert into action_changeset_row
          (changeset_id, row_index, entity_ref, operation_id, before_image,
           after_image, expected_current)
        values (
          '10000000-0000-0000-0000-000000000064', 0, 'SKU-1', 'product_update',
          '{"name":"Before"}', '{"product":{"name":"After"}}', '{"name":"Before"}'
        );
        insert into action_execution
          (org_id, action_request_id, executor, status, changeset_id)
        values (
          'magento-v2-org', '00000000-0000-0000-0000-000000000064',
          'magento_ops_executor', 'succeeded',
          '10000000-0000-0000-0000-000000000064'
        );
        insert into magento_financial_handoff
          (org_id, kind, entity_ref, status, draft, evidence)
        values (
          'magento-v2-org', 'online_refund', 'order:100000001',
          'ready_for_human', '{"amount":10}', '{"creditMemo":"draft"}'
        );
      `);

      await expect(client.query(`
        insert into action_changeset
          (org_id, domain, operation_id, risk_class, summary, idempotency_key)
        values ('magento-v2-org', 'orders', 'online_refund', 0, 'Forbidden', 'class-zero')
      `)).rejects.toThrow(/risk_class|check constraint/i);
      await expect(client.query(`
        insert into action_changeset
          (org_id, domain, operation_id, risk_class, summary, idempotency_key)
        values ('magento-v2-org', 'catalog', 'product_update', 2, 'Duplicate', 'fixture-0064')
      `)).rejects.toThrow(/unique|duplicate/i);

      const linked = await client.query<{ count: string }>(`
        select count(*)::text as count from action_execution
         where changeset_id = '10000000-0000-0000-0000-000000000064'
      `);
      expect(linked.rows[0]?.count).toBe("1");
    });
  });

  it("0065 records one skill usage row per run and skill", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0006);
      await applyFile(client, M_0065);
      await applyFile(client, M_0065);

      await client.query(`
        insert into organization (id, name) values ('skill-org', 'Skill Org');
        insert into work_thread (id, org_id, title)
        values ('00000000-0000-0000-0000-000000000065', 'skill-org', 'Thread');
        insert into work_run (id, org_id, thread_id, backend, status)
        values (
          '10000000-0000-0000-0000-000000000065',
          'skill-org',
          '00000000-0000-0000-0000-000000000065',
          'ax',
          'running'
        );
        insert into skill_usage (
          org_id, run_id, skill_name, content_hash, origin, source, first_event_id
        ) values (
          'skill-org',
          '10000000-0000-0000-0000-000000000065',
          'pdf',
          'abc',
          'builtin',
          'read',
          1
        );
      `);
      await expect(
        client.query(`
          insert into skill_usage (
            org_id, run_id, skill_name, content_hash, origin, source, first_event_id
          ) values (
            'skill-org',
            '10000000-0000-0000-0000-000000000065',
            'pdf',
            'def',
            'builtin',
            'read',
            2
          )
        `),
      ).rejects.toThrow(/unique|duplicate/i);
      await expect(
        client.query(`
          insert into skill_usage (
            org_id, run_id, skill_name, content_hash, origin, source, first_event_id
          ) values (
            'skill-org',
            '10000000-0000-0000-0000-000000000065',
            'bad',
            'abc',
            'wiki',
            'read',
            3
          )
        `),
      ).rejects.toThrow(/origin|check constraint/i);
    });
  });

  it("0066 creates skill learn tables with flags default off", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0006);
      await applyFile(client, M_0065);
      await applyFile(client, M_0066);
      await applyFile(client, M_0066);
      const tables = await client.query<{ table_name: string }>(
        `select table_name from information_schema.tables
         where table_schema = 'public'
           and table_name like 'skill_learn%'
         order by table_name`,
      );
      expect(tables.rows.map((row) => row.table_name)).toEqual([
        "skill_learn_event",
        "skill_learn_org",
        "skill_learn_state",
      ]);
      await client.query(`
        insert into organization (id, name) values ('learn-org', 'Learn');
        insert into skill_learn_org (org_id) values ('learn-org');
      `);
      const flags = await client.query<{ enabled: boolean }>(
        `select enabled from skill_learn_org where org_id = 'learn-org'`,
      );
      expect(flags.rows[0]?.enabled).toBe(false);
    });
  });

  it("0067 creates an idempotent, fail-closed workflow API admission schema", async () => {
    await withTempDb(async (client) => {
      await applyFile(client, M_0001);
      await applyFile(client, M_0006);
      await applyFile(client, M_0009);
      await applyFile(client, M_0067);
      await applyFile(client, M_0067);

      const tables = await client.query<{ table_name: string }>(`
        select table_name from information_schema.tables
         where table_schema = 'public'
           and table_name like 'workflow_api_%'
         order by table_name
      `);
      expect(tables.rows.map((row) => row.table_name)).toEqual([
        "workflow_api_access",
        "workflow_api_admission",
        "workflow_api_rate_bucket",
      ]);

      await client.query(`
        insert into organization (id, name) values ('api-org', 'API Org');
        insert into work_thread (id, org_id, title)
        values ('00000000-0000-0000-0000-000000000067', 'api-org', 'API Thread');
        insert into work_run (id, org_id, thread_id, backend, status)
        values (
          '10000000-0000-0000-0000-000000000067', 'api-org',
          '00000000-0000-0000-0000-000000000067', 'ax', 'queued'
        );
        insert into workflow_definition (id, org_id, name, steps)
        values (
          '20000000-0000-0000-0000-000000000067', 'api-org',
          'API workflow', '[]'::jsonb
        ), (
          '21000000-0000-0000-0000-000000000067', 'api-org',
          'Second API workflow', '[]'::jsonb
        );
        insert into workflow_run (
          id, org_id, workflow_id, thread_id, work_run_id,
          trigger_kind, execution_mode, admitted_at, result_expires_at
        ) values (
          '30000000-0000-0000-0000-000000000067', 'api-org',
          '20000000-0000-0000-0000-000000000067',
          '00000000-0000-0000-0000-000000000067',
          '10000000-0000-0000-0000-000000000067',
          'api', 'single', now(), now() + interval '1 day'
        );
        insert into workflow_api_access (
          workflow_id, org_id, enabled, token_verifier, token_prefix,
          token_created_at
        ) values (
          '20000000-0000-0000-0000-000000000067', 'api-org', true,
          repeat('a', 64), 'onk_wf_0123456789ab', now()
        );
        insert into workflow_api_admission (
          id, org_id, workflow_id, workflow_run_id, idempotency_hash,
          payload_hash, execution_mode, request_payload, request_bytes,
          expires_at
        ) values (
          '40000000-0000-0000-0000-000000000067', 'api-org',
          '20000000-0000-0000-0000-000000000067',
          '30000000-0000-0000-0000-000000000067', repeat('b', 64),
          repeat('c', 64), 'single', '{}'::jsonb, 2, now() + interval '1 day'
        );
      `);

      await expect(
        client.query(`
          update workflow_api_access
          set batch_max_records = 10, batch_chunk_size = 11
          where workflow_id = '20000000-0000-0000-0000-000000000067'
        `),
      ).rejects.toThrow(/batch_bounds|check constraint/i);
      await expect(
        client.query(`
          insert into workflow_api_access (workflow_id, org_id, enabled)
          values ('21000000-0000-0000-0000-000000000067', 'api-org', true)
        `),
      ).rejects.toThrow(/token_state|check constraint/i);
      await expect(
        client.query(`
          update workflow_run
          set trigger_kind = 'manual', execution_mode = 'batch'
          where id = '30000000-0000-0000-0000-000000000067'
        `),
      ).rejects.toThrow(/execution_mode|check constraint/i);
      await expect(
        client.query(`
          insert into workflow_api_admission (
            org_id, workflow_id, workflow_run_id, idempotency_hash,
            payload_hash, execution_mode, request_payload, request_bytes,
            expires_at
          ) values (
            'api-org', '20000000-0000-0000-0000-000000000067',
            '30000000-0000-0000-0000-000000000067', repeat('b', 64),
            repeat('d', 64), 'single', '{}'::jsonb, 2,
            now() + interval '1 day'
          )
        `),
      ).rejects.toThrow(/unique|duplicate/i);
    });
  });

  it("0006 creates the work runtime tables", async () => {
    await withTempDb(async (client) => {
      await applyAll(client);
      const tables = await client.query<{ table_name: string }>(
        `select table_name from information_schema.tables
         where table_schema = 'public' order by table_name`,
      );
      const names = tables.rows.map((row) => row.table_name);
      for (const expected of [
        "work_thread",
        "work_run",
        "work_message",
        "work_run_event",
      ]) {
        expect(names, `missing ${expected}`).toContain(expected);
      }
    });
  });

  it("0006 enforces one sequence slot per run event", async () => {
    await withTempDb(async (client) => {
      await applyAll(client);
      await client.query(`
        insert into organization (id, name) values ('work-org', 'Work Org');
        insert into work_thread (id, org_id, title) values ('00000000-0000-0000-0000-000000000001', 'work-org', 'Thread');
        insert into work_run (id, org_id, thread_id, backend, status)
        values ('00000000-0000-0000-0000-000000000002', 'work-org', '00000000-0000-0000-0000-000000000001', 'ax', 'running');
        insert into work_run_event (org_id, thread_id, run_id, seq, kind, payload)
        values ('work-org', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002', 1, 'status', '{"type":"status","message":"ok"}');
      `);
      await expect(
        client.query(`
          insert into work_run_event (org_id, thread_id, run_id, seq, kind, payload)
          values ('work-org', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002', 1, 'status', '{"type":"status","message":"dup"}')
        `),
      ).rejects.toThrow(/unique/i);
    });
  });

  it("0007 creates work memory tables", async () => {
    await withTempDb(async (client) => {
      await applyAll(client);
      const tables = await client.query<{ table_name: string }>(
        `select table_name from information_schema.tables
         where table_schema = 'public' order by table_name`,
      );
      const names = tables.rows.map((row) => row.table_name);
      for (const expected of [
        "work_memory",
        "work_memory_event",
        "work_pending_memory",
      ]) {
        expect(names, `missing ${expected}`).toContain(expected);
      }
    });
  });
});
