import { pool } from "@neko/db";
import { raiseSpendAlert } from "./alerts";
import { loadSpendLimits, microsToUsd, type SpendQueryable } from "./limits";

export type SpendSource =
  | "chat"
  | "channel"
  | "cron"
  | "trigger"
  | "api"
  | "webhook"
  | "metric"
  | "system";

export type SpendBudget = "org_hourly" | "org_daily" | "workflow_hourly" | "workflow_daily";

export class SpendBudgetExceeded extends Error {
  readonly code = "spend_budget_exhausted";
  constructor(
    readonly budget: SpendBudget,
    readonly limitUsd: number,
    readonly committedUsd: number,
    readonly reservationUsd: number,
    readonly retryAfterSeconds: number,
    readonly resetsAt: Date,
    readonly alert: { orgId: string; workflowId: string | null; windowStart: Date; source: SpendSource; committedMicros: number; limitMicros: number } | null = null,
  ) {
    super(spendBudgetMessage(budget, limitUsd, resetsAt));
    this.name = "SpendBudgetExceeded";
  }
}

function spendBudgetMessage(budget: SpendBudget, limitUsd: number, resetsAt: Date): string {
  const scope = budget.startsWith("org") ? "this organization" : "this workflow";
  const window = budget.endsWith("hourly") ? "hourly" : "daily";
  const time = resetsAt.toISOString().slice(11, 16);
  return `The ${window} spending limit of $${limitUsd.toFixed(2)} for ${scope} is reached. Try again after ${time} UTC.`;
}

export type SpendAdmission = {
  reservationId: string;
  reservedMicros: number;
};

export function spendWindows(now: Date) {
  const hourStart = new Date(Math.floor(now.getTime() / 3_600_000) * 3_600_000);
  const dayStart = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()));
  return {
    hourStart,
    hourEnd: new Date(hourStart.getTime() + 3_600_000),
    dayStart,
    dayEnd: new Date(dayStart.getTime() + 86_400_000),
  };
}

/**
 * Ledger spend in the window, plus the unused part of every reservation whose
 * run is still queued or running. An open reservation counts whatever its age.
 */
export async function committedSpendMicros(
  client: SpendQueryable,
  input: { orgId: string; workflowId?: string | null; since: Date },
): Promise<number> {
  const { spentMicros, heldMicros } = await spendBreakdownMicros(client, input);
  return spentMicros + heldMicros;
}

export async function spendBreakdownMicros(
  client: SpendQueryable,
  input: { orgId: string; workflowId?: string | null; since: Date },
): Promise<{ spentMicros: number; heldMicros: number }> {
  const { rows } = await client.query<{ spent: string; held: string }>(
    `with open_reservation as (
       select r.id, r.reserved_micros
         from spend_reservation r
         left join work_run w on w.id = r.work_run_id
        where r.org_id = $1
          and ($2::uuid is null or r.workflow_id = $2::uuid)
          and r.released_at is null
          and (r.work_run_id is null or w.status in ('queued', 'running'))
     ), used as (
       select l.reservation_id, sum(l.cost_micros) as used
         from spend_ledger l
         join open_reservation o on o.id = l.reservation_id
        group by l.reservation_id
     )
     select
       coalesce((select sum(cost_micros) from spend_ledger
                  where org_id = $1
                    and ($2::uuid is null or workflow_id = $2::uuid)
                    and created_at >= $3), 0) as spent,
       coalesce((select sum(greatest(o.reserved_micros - coalesce(u.used, 0), 0))
                   from open_reservation o
                   left join used u on u.reservation_id = o.id), 0) as held`,
    [input.orgId, input.workflowId ?? null, input.since],
  );
  return { spentMicros: Number(rows[0]?.spent ?? 0), heldMicros: Number(rows[0]?.held ?? 0) };
}

/**
 * Check the four budgets and reserve the per-run cap. The caller must hold an
 * open transaction; the advisory lock serializes admissions in one org.
 */
export async function admitRunSpend(
  client: SpendQueryable,
  input: {
    orgId: string;
    workflowId?: string | null;
    workRunId?: string | null;
    source: SpendSource;
    now?: Date;
  },
): Promise<SpendAdmission> {
  const now = input.now ?? new Date();
  await client.query("select pg_advisory_xact_lock(hashtext('openneko:spend:' || $1))", [input.orgId]);
  const limits = await loadSpendLimits(client, input.orgId, input.workflowId);
  const windows = spendWindows(now);
  const reservation = limits.runCapMicros;
  const checks: Array<{ budget: SpendBudget; limit: number; since: Date; resetsAt: Date; workflowId: string | null }> = [
    { budget: "org_hourly", limit: limits.orgHourlyMicros, since: windows.hourStart, resetsAt: windows.hourEnd, workflowId: null },
    { budget: "org_daily", limit: limits.orgDailyMicros, since: windows.dayStart, resetsAt: windows.dayEnd, workflowId: null },
  ];
  if (input.workflowId) {
    checks.push(
      { budget: "workflow_hourly", limit: limits.workflowHourlyMicros, since: windows.hourStart, resetsAt: windows.hourEnd, workflowId: input.workflowId },
      { budget: "workflow_daily", limit: limits.workflowDailyMicros, since: windows.dayStart, resetsAt: windows.dayEnd, workflowId: input.workflowId },
    );
  }
  for (const check of checks) {
    const committed = await committedSpendMicros(client, {
      orgId: input.orgId,
      workflowId: check.workflowId,
      since: check.since,
    });
    if (committed + reservation > check.limit) {
      throw new SpendBudgetExceeded(
        check.budget,
        microsToUsd(check.limit),
        microsToUsd(committed),
        microsToUsd(reservation),
        Math.max(1, Math.ceil((check.resetsAt.getTime() - now.getTime()) / 1_000)),
        check.resetsAt,
        {
          orgId: input.orgId,
          workflowId: check.workflowId,
          windowStart: check.since,
          source: input.source,
          committedMicros: committed,
          limitMicros: check.limit,
        },
      );
    }
  }
  const { rows } = await client.query<{ id: string }>(
    `insert into spend_reservation (org_id, work_run_id, workflow_id, source, reserved_micros, created_at)
     values ($1, $2::uuid, $3::uuid, $4, $5, $6)
     returning id`,
    [input.orgId, input.workRunId ?? null, input.workflowId ?? null, input.source, reservation, now],
  );
  return { reservationId: rows[0]!.id, reservedMicros: reservation };
}

/** Admit and reserve in a transaction of its own, for spend outside a work run. */
export async function reserveSpend(input: {
  orgId: string;
  workflowId?: string | null;
  source: SpendSource;
  now?: Date;
}): Promise<SpendAdmission> {
  const client = await pool().connect();
  let released = false;
  try {
    await client.query("begin");
    const admission = await admitRunSpend(client, input);
    await client.query("commit");
    return admission;
  } catch (error) {
    await client.query("rollback").catch(() => undefined);
    client.release();
    released = true;
    await recordBudgetBlocked(error);
    throw error;
  } finally {
    if (!released) client.release();
  }
}

/** Record a blocked admission. Call it after the admission transaction has ended. */
export async function recordBudgetBlocked(error: unknown): Promise<void> {
  if (!(error instanceof SpendBudgetExceeded) || !error.alert) return;
  const { alert } = error;
  await raiseSpendAlert({
    orgId: alert.orgId,
    kind: "spend.budget_blocked",
    subject: alert.workflowId ? `workflow:${alert.workflowId}` : "org",
    observedMicros: alert.committedMicros,
    thresholdMicros: alert.limitMicros,
    windowSeconds: error.budget.endsWith("hourly") ? 3_600 : 86_400,
    windowStart: alert.windowStart,
    message: `A run was blocked. ${error.message}`,
    details: { budget: error.budget, source: alert.source },
  }).catch((cause) => {
    console.warn(`[spend] could not record blocked alert: ${cause instanceof Error ? cause.message : cause}`);
  });
}

export async function releaseSpendReservation(reservationId: string, now = new Date()): Promise<void> {
  await pool().query(
    "update spend_reservation set released_at = $2 where id = $1 and released_at is null",
    [reservationId, now],
  );
}
