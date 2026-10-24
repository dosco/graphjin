import { randomUUID } from "node:crypto";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { pool } from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  claimWorkflowScheduleFiring,
  leasePendingWorkflowFirings,
  materializeDueWorkflowFirings,
  recoverStaleWorkflowScheduleFirings,
} from "../../src/workflows/durable-scheduler";

const reachable = await dbReachable();
const durableSchema = reachable
  ? Boolean(
      (
        await pool().query<{ table_name: string | null }>(
          "select to_regclass('public.workflow_schedule_firing')::text as table_name",
        )
      ).rows[0]?.table_name,
    )
  : false;
const describeIfDb = reachable && durableSchema ? describe : describe.skip;

if (reachable && !durableSchema) {
  console.warn(
    "[durable-scheduler] skipping: migration 0063 is not applied to metadata Postgres.",
  );
}

describeIfDb("durable workflow scheduler persistence", () => {
  const orgId = uniqueOrgId("durable-scheduler");
  const workflowId = randomUUID();
  const coldStartWorkflowId = randomUUID();
  const disabledWorkflowId = randomUUID();
  const invalidWorkflowId = randomUUID();
  const recoveryWorkflowId = randomUUID();
  const cancelledRecoveryWorkflowId = randomUUID();
  const boundary = new Date("2030-08-26T07:00:00.000Z");

  beforeAll(async () => {
    await createTestOrg(orgId);
    await pool().query(
      `insert into workflow_definition (
         id, org_id, name, steps, cron, cron_timezone, cron_enabled,
         enabled, updated_at
       ) values ($1, $2, 'Durable scheduler test', '[]'::jsonb,
                 '* * * * *', 'UTC', true, true, $3)`,
      [workflowId, orgId, boundary],
    );
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  it("starts strictly after enable and never invents a pre-enable firing", async () => {
    const initialized = await materializeDueWorkflowFirings(boundary, {
      orgId,
    });
    expect(initialized.materialized).toBe(0);

    const state = await pool().query<{ next_fire_at: Date }>(
      "select next_fire_at from workflow_schedule_state where workflow_id = $1",
      [workflowId],
    );
    expect(state.rows[0]?.next_fire_at.toISOString()).toBe(
      "2030-08-26T07:01:00.000Z",
    );
    const firings = await pool().query(
      "select id from workflow_schedule_firing where workflow_id = $1",
      [workflowId],
    );
    expect(firings.rowCount).toBe(0);
  });

  it("coalesces an overdue persisted cursor once across restart sweeps", async () => {
    const afterDowntime = new Date("2030-08-26T07:05:30.000Z");
    const first = await materializeDueWorkflowFirings(afterDowntime, { orgId });
    const second = await materializeDueWorkflowFirings(afterDowntime, {
      orgId,
    });

    expect(first.materialized).toBe(1);
    expect(first.coalescedOccurrences).toBe(4);
    expect(second.materialized).toBe(0);

    const rows = await pool().query<{
      scheduled_for: Date;
      status: string;
    }>(
      `select scheduled_for, status from workflow_schedule_firing
       where workflow_id = $1 order by scheduled_for`,
      [workflowId],
    );
    expect(rows.rows).toHaveLength(1);
    expect(rows.rows[0]?.scheduled_for.toISOString()).toBe(
      "2030-08-26T07:05:00.000Z",
    );
    expect(rows.rows[0]?.status).toBe("pending");
  });

  it("uses locking and the unique firing key under concurrent sweepers", async () => {
    await pool().query(
      `update workflow_schedule_state
       set next_fire_at = $2 where workflow_id = $1`,
      [workflowId, new Date("2030-08-26T07:06:00.000Z")],
    );
    const now = new Date("2030-08-26T07:06:30.000Z");
    const results = await Promise.all([
      materializeDueWorkflowFirings(now, { orgId }),
      materializeDueWorkflowFirings(now, { orgId }),
    ]);
    expect(results.reduce((sum, result) => sum + result.materialized, 0)).toBe(
      1,
    );

    const count = await pool().query<{ total: string }>(
      `select count(*) as total from workflow_schedule_firing
       where workflow_id = $1 and scheduled_for = $2`,
      [workflowId, new Date("2030-08-26T07:06:00.000Z")],
    );
    expect(Number(count.rows[0]?.total)).toBe(1);
  });

  it("re-leases a crashed dispatch and permits only one consumer claim", async () => {
    const firstLeaseAt = new Date("2030-08-26T07:06:31.000Z");
    const first = await leasePendingWorkflowFirings({
      orgId,
      now: firstLeaseAt,
      leaseMs: 1_000,
      limit: 10,
    });
    expect(first.length).toBeGreaterThan(0);
    const target = first[0]!;

    const duringLease = await leasePendingWorkflowFirings({
      orgId,
      now: new Date("2030-08-26T07:06:31.500Z"),
      leaseMs: 1_000,
      limit: 10,
    });
    expect(duringLease.some((row) => row.id === target.id)).toBe(false);

    const afterCrash = await leasePendingWorkflowFirings({
      orgId,
      now: new Date("2030-08-26T07:06:32.001Z"),
      leaseMs: 1_000,
      limit: 10,
    });
    expect(afterCrash.find((row) => row.id === target.id)?.attempts).toBe(2);

    expect(
      await claimWorkflowScheduleFiring({
        firingId: target.id,
        orgId,
        workflowId,
      }),
    ).toBe(true);
    expect(
      await claimWorkflowScheduleFiring({
        firingId: target.id,
        orgId,
        workflowId,
      }),
    ).toBe(false);
  });

  it("catches up a definition first seen after worker downtime", async () => {
    await pool().query(
      `insert into workflow_definition (
         id, org_id, name, steps, cron, cron_timezone, cron_enabled,
         enabled, updated_at
       ) values ($1, $2, 'Created while worker was down', '[]'::jsonb,
                 '* * * * *', 'UTC', true, true, $3)`,
      [coldStartWorkflowId, orgId, new Date("2030-08-26T07:10:00.000Z")],
    );

    await materializeDueWorkflowFirings(new Date("2030-08-26T07:20:30.000Z"), {
      orgId,
    });

    const rows = await pool().query<{
      scheduled_for: Date;
      next_fire_at: Date;
    }>(
      `select firing.scheduled_for, state.next_fire_at
       from workflow_schedule_firing firing
       join workflow_schedule_state state
         on state.workflow_id = firing.workflow_id
       where firing.workflow_id = $1`,
      [coldStartWorkflowId],
    );
    expect(rows.rows).toHaveLength(1);
    expect(rows.rows[0]?.scheduled_for.toISOString()).toBe(
      "2030-08-26T07:20:00.000Z",
    );
    expect(rows.rows[0]?.next_fire_at.toISOString()).toBe(
      "2030-08-26T07:21:00.000Z",
    );
  });

  it("cancels an unclaimed firing when its schedule is disabled", async () => {
    const enabledAt = new Date("2030-08-26T08:00:00.000Z");
    await pool().query(
      `insert into workflow_definition (
         id, org_id, name, steps, cron, cron_timezone, cron_enabled,
         enabled, updated_at
       ) values ($1, $2, 'Disable race', '[]'::jsonb,
                 '* * * * *', 'UTC', true, true, $3)`,
      [disabledWorkflowId, orgId, enabledAt],
    );
    await materializeDueWorkflowFirings(enabledAt, { orgId });
    await materializeDueWorkflowFirings(new Date("2030-08-26T08:01:30.000Z"), {
      orgId,
    });
    await pool().query(
      `update workflow_definition
       set cron_enabled = false, updated_at = $2 where id = $1`,
      [disabledWorkflowId, new Date("2030-08-26T08:01:31.000Z")],
    );
    await materializeDueWorkflowFirings(new Date("2030-08-26T08:01:32.000Z"), {
      orgId,
    });

    const firing = await pool().query<{ status: string; last_error: string }>(
      `select status, last_error from workflow_schedule_firing
       where workflow_id = $1`,
      [disabledWorkflowId],
    );
    expect(firing.rows).toEqual([
      {
        status: "cancelled",
        last_error: "schedule disabled before dispatch",
      },
    ]);
  });

  it("cancels an unclaimed firing when an edit makes its schedule invalid", async () => {
    const enabledAt = new Date("2030-08-26T08:05:00.000Z");
    await pool().query(
      `insert into workflow_definition (
         id, org_id, name, steps, cron, cron_timezone, cron_enabled,
         enabled, updated_at
       ) values ($1, $2, 'Invalid edit race', '[]'::jsonb,
                 '* * * * *', 'UTC', true, true, $3)`,
      [invalidWorkflowId, orgId, enabledAt],
    );
    await materializeDueWorkflowFirings(enabledAt, { orgId });
    await materializeDueWorkflowFirings(new Date("2030-08-26T08:06:30.000Z"), {
      orgId,
    });
    await pool().query(
      `update workflow_definition
       set cron = 'not-a-cron', updated_at = $2 where id = $1`,
      [invalidWorkflowId, new Date("2030-08-26T08:06:31.000Z")],
    );
    const sweep = await materializeDueWorkflowFirings(
      new Date("2030-08-26T08:06:32.000Z"),
      { orgId },
    );

    expect(sweep.invalidSchedules).toBe(1);
    const firing = await pool().query<{ status: string; last_error: string }>(
      `select status, last_error from workflow_schedule_firing
       where workflow_id = $1`,
      [invalidWorkflowId],
    );
    expect(firing.rows).toEqual([
      {
        status: "cancelled",
        last_error: "schedule is invalid before dispatch",
      },
    ]);
    const state = await pool().query(
      "select workflow_id from workflow_schedule_state where workflow_id = $1",
      [invalidWorkflowId],
    );
    expect(state.rowCount).toBe(0);
  });

  it("repairs a lost ledger-completion update from the terminal run", async () => {
    const threadId = randomUUID();
    const workRunId = randomUUID();
    const workflowRunId = randomUUID();
    const firingId = randomUUID();
    await pool().query(
      `insert into workflow_definition (id, org_id, name, steps)
       values ($1, $2, 'Completion recovery', '[]'::jsonb)`,
      [recoveryWorkflowId, orgId],
    );
    await pool().query(
      `insert into work_thread (id, org_id, title)
       values ($1, $2, 'Completion recovery')`,
      [threadId, orgId],
    );
    await pool().query(
      `insert into work_run (id, org_id, thread_id, backend, status)
       values ($1, $2, $3, 'ax', 'completed')`,
      [workRunId, orgId, threadId],
    );
    await pool().query(
      `insert into workflow_run (
         id, org_id, workflow_id, thread_id, work_run_id, trigger_kind, status
       ) values ($1, $2, $3, $4, $5, 'cron', 'completed')`,
      [workflowRunId, orgId, recoveryWorkflowId, threadId, workRunId],
    );
    await pool().query(
      `insert into workflow_schedule_firing (
         id, org_id, workflow_id, scheduled_for, status, workflow_run_id,
         lease_until
       ) values ($1, $2, $3, $4, 'running', $5, $6)`,
      [
        firingId,
        orgId,
        recoveryWorkflowId,
        new Date("2030-08-26T08:15:00.000Z"),
        workflowRunId,
        new Date("2030-08-26T08:16:00.000Z"),
      ],
    );

    const recovered = await recoverStaleWorkflowScheduleFirings(
      new Date("2030-08-26T08:17:00.000Z"),
      { orgId },
    );
    expect(recovered).toBeGreaterThanOrEqual(1);
    const row = await pool().query<{ status: string; completed_at: Date }>(
      `select status, completed_at from workflow_schedule_firing where id = $1`,
      [firingId],
    );
    expect(row.rows[0]?.status).toBe("completed");
    expect(row.rows[0]?.completed_at.toISOString()).toBe(
      "2030-08-26T08:17:00.000Z",
    );
  });

  it("immediately retries a linked run that restart reconciliation cancelled", async () => {
    const threadId = randomUUID();
    const workRunId = randomUUID();
    const workflowRunId = randomUUID();
    const firingId = randomUUID();
    await pool().query(
      `insert into workflow_definition (id, org_id, name, steps)
       values ($1, $2, 'Cancelled run recovery', '[]'::jsonb)`,
      [cancelledRecoveryWorkflowId, orgId],
    );
    await pool().query(
      `insert into work_thread (id, org_id, title)
       values ($1, $2, 'Cancelled run recovery')`,
      [threadId, orgId],
    );
    await pool().query(
      `insert into work_run (id, org_id, thread_id, backend, status)
       values ($1, $2, $3, 'ax', 'cancelled')`,
      [workRunId, orgId, threadId],
    );
    await pool().query(
      `insert into workflow_run (
         id, org_id, workflow_id, thread_id, work_run_id, trigger_kind, status
       ) values ($1, $2, $3, $4, $5, 'cron', 'cancelled')`,
      [
        workflowRunId,
        orgId,
        cancelledRecoveryWorkflowId,
        threadId,
        workRunId,
      ],
    );
    await pool().query(
      `insert into workflow_schedule_firing (
         id, org_id, workflow_id, scheduled_for, status, workflow_run_id,
         lease_until
       ) values ($1, $2, $3, $4, 'running', $5, $6)`,
      [
        firingId,
        orgId,
        cancelledRecoveryWorkflowId,
        new Date("2030-08-26T08:20:00.000Z"),
        workflowRunId,
        new Date("2030-08-26T08:50:00.000Z"),
      ],
    );

    const recovered = await recoverStaleWorkflowScheduleFirings(
      new Date("2030-08-26T08:21:00.000Z"),
      { orgId },
    );
    expect(recovered).toBeGreaterThanOrEqual(1);
    const row = await pool().query<{
      status: string;
      workflow_run_id: string | null;
      available_at: Date;
    }>(
      `select status, workflow_run_id, available_at
       from workflow_schedule_firing where id = $1`,
      [firingId],
    );
    expect(row.rows[0]).toEqual({
      status: "pending",
      workflow_run_id: null,
      available_at: new Date("2030-08-26T08:21:00.000Z"),
    });
  });
});
