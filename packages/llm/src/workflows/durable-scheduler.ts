import cronParser from "cron-parser";
import { pool } from "@neko/db";

export const WORKFLOW_SCHEDULER_HEALTH_ID = "cron";
export const WORKFLOW_SCHEDULER_LOCK_KEY =
  "openneko.workflow-scheduler.materialize";
export const DEFAULT_DISPATCH_LEASE_MS = 2 * 60_000;
export const DEFAULT_RUN_LEASE_MS = 30 * 60_000;
export const DEFAULT_REPLAY_LIMIT = 24;
const MAX_COALESCE_SCAN = 1_000;

export type WorkflowCatchUpPolicy = "coalesce" | "replay";

export type ScheduleAdvance = {
  scheduledFor: Date[];
  nextFireAt: Date;
  coalescedOccurrences: number;
};

export function applyWorkflowDailyBudget<T>(input: {
  occurrences: T[];
  dailyRunBudget: number | null;
  alreadyConsumed: number;
}): { selected: T[]; skipped: number } {
  if (input.dailyRunBudget === null) {
    return { selected: input.occurrences, skipped: 0 };
  }
  const remaining = Math.max(0, input.dailyRunBudget - input.alreadyConsumed);
  return {
    selected: input.occurrences.slice(0, remaining),
    skipped: Math.max(0, input.occurrences.length - remaining),
  };
}

/** The first cron occurrence strictly after `after`. */
export function nextWorkflowFireAt(
  cron: string,
  timezone: string,
  after: Date,
): Date {
  return cronParser
    .parseExpression(cron, { tz: timezone, currentDate: after })
    .next()
    .toDate();
}

/**
 * Advance an overdue persisted cursor. Coalesce emits only the latest missed
 * occurrence; replay emits a bounded prefix and leaves the cursor overdue when
 * more history remains so subsequent ticks continue without an unbounded DB
 * transaction.
 */
export function advanceWorkflowSchedule(input: {
  cron: string;
  timezone: string;
  nextFireAt: Date;
  now: Date;
  catchUpPolicy: WorkflowCatchUpPolicy;
  replayLimit?: number;
}): ScheduleAdvance {
  const replayLimit = Math.max(1, input.replayLimit ?? DEFAULT_REPLAY_LIMIT);
  if (input.nextFireAt > input.now) {
    return {
      scheduledFor: [],
      nextFireAt: input.nextFireAt,
      coalescedOccurrences: 0,
    };
  }

  if (input.catchUpPolicy === "coalesce") {
    // Counting every minute across a long outage can itself prevent recovery.
    // Scan only for observability; if the cap is reached, jump directly to
    // the last occurrence at/before now. The emitted occurrence and next
    // cursor remain exact even when the coalesced count is a lower bound.
    let latestDue = input.nextFireAt;
    let coalescedOccurrences = 0;
    while (coalescedOccurrences < MAX_COALESCE_SCAN) {
      const next = nextWorkflowFireAt(input.cron, input.timezone, latestDue);
      if (next > input.now) {
        return {
          scheduledFor: [latestDue],
          nextFireAt: next,
          coalescedOccurrences,
        };
      }
      latestDue = next;
      coalescedOccurrences++;
    }

    latestDue = cronParser
      .parseExpression(input.cron, {
        tz: input.timezone,
        currentDate: new Date(input.now.getTime() + 1),
      })
      .prev()
      .toDate();
    return {
      scheduledFor: [latestDue],
      nextFireAt: nextWorkflowFireAt(input.cron, input.timezone, latestDue),
      coalescedOccurrences,
    };
  }

  let cursor = input.nextFireAt;
  const due: Date[] = [cursor];
  if (due.length >= replayLimit) {
    return {
      scheduledFor: due,
      nextFireAt: nextWorkflowFireAt(input.cron, input.timezone, cursor),
      coalescedOccurrences: 0,
    };
  }
  while (true) {
    const next = nextWorkflowFireAt(input.cron, input.timezone, cursor);
    if (next > input.now) {
      return {
        scheduledFor: due,
        nextFireAt: next,
        coalescedOccurrences: 0,
      };
    }
    cursor = next;
    due.push(cursor);
    if (input.catchUpPolicy === "replay" && due.length >= replayLimit) {
      return {
        scheduledFor: due,
        nextFireAt: nextWorkflowFireAt(input.cron, input.timezone, cursor),
        coalescedOccurrences: 0,
      };
    }
  }
}

type ActiveWorkflowRow = {
  id: string;
  org_id: string;
  cron: string;
  cron_timezone: string;
  updated_at: Date;
};

type DueScheduleRow = ActiveWorkflowRow & {
  next_fire_at: Date;
  catch_up_policy: WorkflowCatchUpPolicy;
  daily_run_budget: number | null;
};

export type MaterializeScheduleResult = {
  acquired: boolean;
  activeWorkflows: number;
  materialized: number;
  coalescedOccurrences: number;
  budgetSkipped: number;
  invalidSchedules: number;
};

/**
 * Synchronize schedule definitions, lock due cursors, insert firing-ledger
 * rows, and advance the cursors in one transaction. A global transaction
 * advisory lock keeps the definition sync deterministic across worker
 * replicas; unique firing keys remain the final idempotency backstop.
 */
export async function materializeDueWorkflowFirings(
  now = new Date(),
  options: { orgId?: string } = {},
): Promise<MaterializeScheduleResult> {
  const client = await pool().connect();
  const result: MaterializeScheduleResult = {
    acquired: false,
    activeWorkflows: 0,
    materialized: 0,
    coalescedOccurrences: 0,
    budgetSkipped: 0,
    invalidSchedules: 0,
  };
  try {
    await client.query("BEGIN");
    const lock = await client.query<{ acquired: boolean }>(
      "select pg_try_advisory_xact_lock(hashtext($1)) as acquired",
      [WORKFLOW_SCHEDULER_LOCK_KEY],
    );
    if (!lock.rows[0]?.acquired) {
      await client.query("ROLLBACK");
      return result;
    }
    result.acquired = true;

    await client.query(
      `insert into workflow_scheduler_health (id, last_started_at, updated_at)
       values ($1, $2, $2)
       on conflict (id) do update
       set last_started_at = excluded.last_started_at,
           updated_at = excluded.updated_at`,
      [WORKFLOW_SCHEDULER_HEALTH_ID, now],
    );

    // Pause-for-today recovery belongs to the durable scheduler now; it must
    // happen before active definitions are snapshotted.
    await client.query(
      `update workflow_definition
       set enabled = true, paused_until = null, updated_at = $1
       where enabled = false
         and paused_until is not null
         and paused_until <= $1
         and ($2::text is null or org_id = $2)`,
      [now, options.orgId ?? null],
    );

    const active = await client.query<ActiveWorkflowRow>(
      `select id, org_id, cron, cron_timezone, updated_at
       from workflow_definition
       where enabled = true and cron_enabled = true and cron is not null
         and ($1::text is null or org_id = $1)
       order by id`,
      [options.orgId ?? null],
    );
    result.activeWorkflows = active.rowCount ?? active.rows.length;

    for (const workflow of active.rows) {
      let nextFireAt: Date;
      try {
        nextFireAt = nextWorkflowFireAt(
          workflow.cron,
          workflow.cron_timezone,
          workflow.updated_at,
        );
      } catch (error) {
        result.invalidSchedules++;
        await client.query(
          `update workflow_schedule_firing
           set status = 'cancelled', completed_at = $2, lease_until = null,
               last_error = 'schedule is invalid before dispatch',
               updated_at = $2
           where workflow_id = $1
             and status in ('pending', 'dispatching', 'enqueued')`,
          [workflow.id, now],
        );
        await client.query(
          "delete from workflow_schedule_state where workflow_id = $1",
          [workflow.id],
        );
        console.warn(
          `[durable-workflow-scheduler] invalid cron "${workflow.cron}" on workflow ${workflow.id}: ${error instanceof Error ? error.message : String(error)}`,
        );
        continue;
      }

      // A queue delivery from the previous definition must not run after an
      // edit. Its eventual duplicate delivery will fail the ledger claim.
      await client.query(
        `update workflow_schedule_firing firing
         set status = 'cancelled', completed_at = $4, lease_until = null,
             last_error = 'schedule definition changed before dispatch',
             updated_at = $4
         from workflow_schedule_state state
         where state.workflow_id = $1
           and firing.workflow_id = state.workflow_id
           and firing.status in ('pending', 'dispatching', 'enqueued')
           and (
             state.cron is distinct from $2
             or state.cron_timezone is distinct from $3
             or state.definition_updated_at is distinct from $5
           )`,
        [
          workflow.id,
          workflow.cron,
          workflow.cron_timezone,
          now,
          workflow.updated_at,
        ],
      );

      await client.query(
        `insert into workflow_schedule_state (
           workflow_id, org_id, cron, cron_timezone, definition_updated_at,
           next_fire_at, catch_up_policy, created_at, updated_at
         ) values ($1, $2, $3, $4, $5, $6, 'coalesce', $7, $7)
         on conflict (workflow_id) do update
         set org_id = excluded.org_id,
             cron = excluded.cron,
             cron_timezone = excluded.cron_timezone,
             definition_updated_at = excluded.definition_updated_at,
             next_fire_at = excluded.next_fire_at,
             updated_at = excluded.updated_at
         where workflow_schedule_state.cron is distinct from excluded.cron
            or workflow_schedule_state.cron_timezone is distinct from excluded.cron_timezone
            or workflow_schedule_state.definition_updated_at is distinct from excluded.definition_updated_at`,
        [
          workflow.id,
          workflow.org_id,
          workflow.cron,
          workflow.cron_timezone,
          workflow.updated_at,
          nextFireAt,
          now,
        ],
      );
    }

    // Disabled, deleted, and cron-less definitions must not retain a cursor
    // that can fire after the operator changed the workflow.
    await client.query(
      `delete from workflow_schedule_state state
       where ($1::text is null or state.org_id = $1)
         and not exists (
         select 1 from workflow_definition workflow
         where workflow.id = state.workflow_id
           and workflow.enabled = true
           and workflow.cron_enabled = true
           and workflow.cron is not null
       )`,
      [options.orgId ?? null],
    );

    await client.query(
      `update workflow_schedule_firing firing
       set status = 'cancelled', completed_at = $2, lease_until = null,
           last_error = 'schedule disabled before dispatch', updated_at = $2
       where ($1::text is null or firing.org_id = $1)
         and firing.status in ('pending', 'dispatching', 'enqueued')
         and exists (
           select 1 from workflow_definition workflow
           where workflow.id = firing.workflow_id
             and (
               workflow.enabled = false
               or workflow.cron_enabled = false
               or workflow.cron is null
             )
         )`,
      [options.orgId ?? null, now],
    );

    const due = await client.query<DueScheduleRow>(
      `select state.workflow_id as id,
              state.org_id,
              state.cron,
              state.cron_timezone,
              state.definition_updated_at as updated_at,
              state.next_fire_at,
              state.catch_up_policy,
              workflow.daily_run_budget
       from workflow_schedule_state state
       join workflow_definition workflow on workflow.id = state.workflow_id
       where state.next_fire_at <= $1
         and ($2::text is null or state.org_id = $2)
       order by state.next_fire_at, state.workflow_id
       for update of state skip locked
       limit 100`,
      [now, options.orgId ?? null],
    );

    const utcDayStart = new Date(now);
    utcDayStart.setUTCHours(0, 0, 0, 0);

    for (const schedule of due.rows) {
      const plan = advanceWorkflowSchedule({
        cron: schedule.cron,
        timezone: schedule.cron_timezone,
        nextFireAt: schedule.next_fire_at,
        now,
        catchUpPolicy: schedule.catch_up_policy,
      });

      let occurrences = plan.scheduledFor;
      if (schedule.daily_run_budget !== null) {
        const counts = await client.query<{ total: string }>(
          `select (
             select count(*) from workflow_run
             where workflow_id = $1 and created_at >= $2
           ) + (
             select count(*) from workflow_schedule_firing
             where workflow_id = $1
               and created_at >= $2
               and workflow_run_id is null
               and status not in ('completed', 'cancelled')
           ) as total`,
          [schedule.id, utcDayStart],
        );
        const budgeted = applyWorkflowDailyBudget({
          occurrences,
          dailyRunBudget: schedule.daily_run_budget,
          alreadyConsumed: Number(counts.rows[0]?.total ?? 0),
        });
        occurrences = budgeted.selected;
        result.budgetSkipped += budgeted.skipped;
      }

      let lastMaterialized: Date | null = null;
      for (const scheduledFor of occurrences) {
        const inserted = await client.query(
          `insert into workflow_schedule_firing (
             org_id, workflow_id, scheduled_for, status, available_at,
             created_at, updated_at
           ) values ($1, $2, $3, 'pending', $4, $4, $4)
           on conflict (workflow_id, scheduled_for) do nothing
           returning id`,
          [schedule.org_id, schedule.id, scheduledFor, now],
        );
        if ((inserted.rowCount ?? 0) > 0) {
          result.materialized++;
          lastMaterialized = scheduledFor;
        }
      }
      result.coalescedOccurrences += plan.coalescedOccurrences;

      await client.query(
        `update workflow_schedule_state
         set next_fire_at = $2,
             last_materialized_at = coalesce($3, last_materialized_at),
             updated_at = $4
         where workflow_id = $1`,
        [schedule.id, plan.nextFireAt, lastMaterialized, now],
      );
    }

    await client.query(
      `update workflow_scheduler_health
       set last_succeeded_at = $2,
           last_error = null,
           last_materialized_count = $3,
           updated_at = $2
       where id = $1`,
      [WORKFLOW_SCHEDULER_HEALTH_ID, now, result.materialized],
    );
    await client.query("COMMIT");
    return result;
  } catch (error) {
    await client.query("ROLLBACK").catch(() => undefined);
    throw error;
  } finally {
    client.release();
  }
}

export type LeasedWorkflowFiring = {
  id: string;
  orgId: string;
  workflowId: string;
  scheduledFor: Date;
  attempts: number;
};

export async function leasePendingWorkflowFirings(
  input: {
    now?: Date;
    limit?: number;
    leaseMs?: number;
    orgId?: string;
  } = {},
): Promise<LeasedWorkflowFiring[]> {
  const now = input.now ?? new Date();
  const leaseUntil = new Date(
    now.getTime() + (input.leaseMs ?? DEFAULT_DISPATCH_LEASE_MS),
  );
  const limit = Math.max(1, Math.min(input.limit ?? 50, 200));
  const result = await pool().query<{
    id: string;
    org_id: string;
    workflow_id: string;
    scheduled_for: Date;
    attempts: number;
  }>(
    `with candidates as (
       select id
       from workflow_schedule_firing
       where (
         (status = 'pending' and available_at <= $1)
         or (status = 'dispatching' and lease_until <= $1)
       )
       and ($4::text is null or org_id = $4)
       order by available_at, created_at
       for update skip locked
       limit $2
     )
     update workflow_schedule_firing firing
     set status = 'dispatching',
         attempts = firing.attempts + 1,
         lease_until = $3,
         updated_at = $1
     from candidates
     where firing.id = candidates.id
     returning firing.id, firing.org_id, firing.workflow_id,
               firing.scheduled_for, firing.attempts`,
    [now, limit, leaseUntil, input.orgId ?? null],
  );
  return result.rows.map((row) => ({
    id: row.id,
    orgId: row.org_id,
    workflowId: row.workflow_id,
    scheduledFor: row.scheduled_for,
    attempts: row.attempts,
  }));
}

export async function markWorkflowFiringEnqueued(
  firingId: string,
  queueJobId: string,
  now = new Date(),
): Promise<void> {
  await pool().query(
    `update workflow_schedule_firing
     set status = 'enqueued', queue_job_id = $2, lease_until = null,
         dispatched_at = $3, last_error = null, updated_at = $3
     where id = $1 and status = 'dispatching'`,
    [firingId, queueJobId, now],
  );
}

export async function releaseWorkflowFiringDispatch(
  firingId: string,
  error: unknown,
  now = new Date(),
): Promise<void> {
  const delaySeconds = 15;
  await pool().query(
    `update workflow_schedule_firing
     set status = 'pending', lease_until = null,
         available_at = $3 + ($4 * interval '1 second'),
         last_error = $2, updated_at = $3
     where id = $1 and status = 'dispatching'`,
    [
      firingId,
      error instanceof Error ? error.message : String(error),
      now,
      delaySeconds,
    ],
  );
}

export async function claimWorkflowScheduleFiring(input: {
  firingId: string;
  orgId: string;
  workflowId: string;
  now?: Date;
  leaseMs?: number;
}): Promise<boolean> {
  const now = input.now ?? new Date();
  const leaseUntil = new Date(
    now.getTime() + (input.leaseMs ?? DEFAULT_RUN_LEASE_MS),
  );
  const claimed = await pool().query(
    `update workflow_schedule_firing
     set status = 'running', lease_until = $4, updated_at = $3
     where id = $1 and org_id = $2 and workflow_id = $5
       and status in ('pending', 'dispatching', 'enqueued')
     returning id`,
    [input.firingId, input.orgId, now, leaseUntil, input.workflowId],
  );
  return (claimed.rowCount ?? 0) === 1;
}

export async function linkWorkflowScheduleFiringRun(
  firingId: string,
  workflowRunId: string,
  now = new Date(),
): Promise<void> {
  await pool().query(
    `update workflow_schedule_firing
     set workflow_run_id = $2, updated_at = $3
     where id = $1 and status = 'running'`,
    [firingId, workflowRunId, now],
  );
}

export async function completeWorkflowScheduleFiring(
  firingId: string,
  now = new Date(),
): Promise<void> {
  await pool().query(
    `update workflow_schedule_firing
     set status = 'completed', lease_until = null, completed_at = $2,
         last_error = null, updated_at = $2
     where id = $1`,
    [firingId, now],
  );
}

export async function releaseWorkflowScheduleFiringRun(
  firingId: string,
  error: unknown,
  now = new Date(),
): Promise<void> {
  await pool().query(
    `update workflow_schedule_firing
     set status = 'pending', workflow_run_id = null, lease_until = null,
         available_at = $3 + interval '15 seconds', last_error = $2,
         updated_at = $3
     where id = $1 and status = 'running'`,
    [firingId, error instanceof Error ? error.message : String(error), now],
  );
}

export async function cancelWorkflowScheduleFiring(
  firingId: string,
  reason: string,
  now = new Date(),
): Promise<void> {
  await pool().query(
    `update workflow_schedule_firing
     set status = 'cancelled', workflow_run_id = null, lease_until = null,
         completed_at = $3, last_error = $2, updated_at = $3
     where id = $1 and status = 'running'`,
    [firingId, reason, now],
  );
}

export async function recoverStaleWorkflowScheduleFirings(
  now = new Date(),
  options: { orgId?: string } = {},
): Promise<number> {
  const finalized = await pool().query(
    `update workflow_schedule_firing firing
     set status = 'completed', lease_until = null, completed_at = $1,
         last_error = null, updated_at = $1
     where firing.status = 'running'
       and ($2::text is null or firing.org_id = $2)
       and exists (
         select 1 from workflow_run run
         where run.id = firing.workflow_run_id
           and run.status in ('completed', 'failed', 'needs_input')
       )
     returning firing.id`,
    [now, options.orgId ?? null],
  );
  const recovered = await pool().query(
    `update workflow_schedule_firing firing
     set status = 'pending', workflow_run_id = null, lease_until = null,
         available_at = $1, updated_at = $1,
         last_error = coalesce(last_error, 'recovered stale scheduler lease')
     where (
       (
         firing.status = 'enqueued'
         and firing.workflow_run_id is null
         and firing.updated_at < $1::timestamptz - interval '30 minutes'
       ) or (
         firing.status = 'running'
         and (
           (firing.workflow_run_id is null and firing.lease_until < $1)
           or exists (
             select 1 from workflow_run run
             where run.id = firing.workflow_run_id
               and run.status = 'cancelled'
           )
         )
       )
     ) and ($2::text is null or firing.org_id = $2)
     returning firing.id`,
    [now, options.orgId ?? null],
  );
  return (finalized.rowCount ?? 0) + (recovered.rowCount ?? 0);
}

export async function recordWorkflowSchedulerSuccess(input: {
  dispatched: number;
  now?: Date;
}): Promise<void> {
  const now = input.now ?? new Date();
  await pool().query(
    `insert into workflow_scheduler_health (
       id, last_succeeded_at, last_dispatched_count, updated_at
     ) values ($1, $2, $3, $2)
     on conflict (id) do update
     set last_succeeded_at = excluded.last_succeeded_at,
         last_dispatched_count = excluded.last_dispatched_count,
         last_error = null,
         updated_at = excluded.updated_at`,
    [WORKFLOW_SCHEDULER_HEALTH_ID, now, input.dispatched],
  );
}

export async function recordWorkflowSchedulerFailure(
  error: unknown,
  now = new Date(),
): Promise<void> {
  await pool().query(
    `insert into workflow_scheduler_health (
       id, last_error_at, last_error, updated_at
     ) values ($1, $2, $3, $2)
     on conflict (id) do update
     set last_error_at = excluded.last_error_at,
         last_error = excluded.last_error,
         updated_at = excluded.updated_at`,
    [
      WORKFLOW_SCHEDULER_HEALTH_ID,
      now,
      error instanceof Error ? error.message : String(error),
    ],
  );
}
