import { pool } from "@neko/db";
import { recordAuditEvent } from "../workflows/audit-chain";
import { spendBreakdownMicros, spendWindows } from "./admission";
import { listOpenSpendAlerts, type SpendAlert } from "./alerts";
import {
  loadSpendLimits,
  METRIC_REFRESH_VALUES,
  metricRefreshFromColumn,
  microsToUsd,
  spendCeilingsMicros,
  usdToMicros,
  type MetricRefresh,
} from "./limits";

export type SpendLimitsUsd = {
  runCapUsd: number;
  orgHourlyUsd: number;
  orgDailyUsd: number;
  workflowHourlyUsd: number;
  workflowDailyUsd: number;
  warnPercent: number;
};

export type SpendWindowUsage = {
  spentUsd: number;
  heldUsd: number;
  limitUsd: number;
  resetsAt: string;
};

export type WorkflowSpendRow = {
  workflowId: string;
  name: string;
  enabled: boolean;
  hourlyOverrideUsd: number | null;
  dailyOverrideUsd: number | null;
  hourlyLimitUsd: number;
  dailyLimitUsd: number;
  spentTodayUsd: number;
};

export type SpendSettings = {
  limits: SpendLimitsUsd;
  metricRefresh: MetricRefresh;
  ceilings: Omit<SpendLimitsUsd, "warnPercent">;
  org: { hour: SpendWindowUsage; day: SpendWindowUsage };
  workflows: WorkflowSpendRow[];
  alerts: SpendAlert[];
};

export class SpendSettingsError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "SpendSettingsError";
  }
}

export async function getSpendSettings(orgId: string, now = new Date()): Promise<SpendSettings> {
  const db = pool();
  const limits = await loadSpendLimits(db, orgId);
  const ceilings = spendCeilingsMicros();
  const windows = spendWindows(now);
  const [hour, day, alerts, refresh] = await Promise.all([
    spendBreakdownMicros(db, { orgId, since: windows.hourStart }),
    spendBreakdownMicros(db, { orgId, since: windows.dayStart }),
    listOpenSpendAlerts(orgId),
    db.query<{ metric_refresh: string | null }>(
      "select metric_refresh from spend_limit where org_id = $1 and workflow_id is null",
      [orgId],
    ),
  ]);
  const { rows } = await db.query<{
    id: string;
    name: string;
    enabled: boolean;
    workflow_hourly_micros: string | null;
    workflow_daily_micros: string | null;
    spent_today: string;
  }>(
    `select w.id, w.name, w.enabled, l.workflow_hourly_micros, l.workflow_daily_micros,
            coalesce((select sum(s.cost_micros) from spend_ledger s
                       where s.org_id = w.org_id and s.workflow_id = w.id and s.created_at >= $2), 0) as spent_today
       from workflow_definition w
       left join spend_limit l on l.org_id = w.org_id and l.workflow_id = w.id
      where w.org_id = $1
      order by spent_today desc, w.name`,
    [orgId, windows.dayStart],
  );
  const override = (value: string | null) => (value === null ? null : microsToUsd(Number(value)));
  return {
    limits: {
      runCapUsd: microsToUsd(limits.runCapMicros),
      orgHourlyUsd: microsToUsd(limits.orgHourlyMicros),
      orgDailyUsd: microsToUsd(limits.orgDailyMicros),
      workflowHourlyUsd: microsToUsd(limits.workflowHourlyMicros),
      workflowDailyUsd: microsToUsd(limits.workflowDailyMicros),
      warnPercent: limits.warnPercent,
    },
    metricRefresh: metricRefreshFromColumn(refresh.rows[0]?.metric_refresh),
    ceilings: {
      runCapUsd: microsToUsd(ceilings.runCapMicros),
      orgHourlyUsd: microsToUsd(ceilings.orgHourlyMicros),
      orgDailyUsd: microsToUsd(ceilings.orgDailyMicros),
      workflowHourlyUsd: microsToUsd(ceilings.workflowHourlyMicros),
      workflowDailyUsd: microsToUsd(ceilings.workflowDailyMicros),
    },
    org: {
      hour: {
        spentUsd: microsToUsd(hour.spentMicros),
        heldUsd: microsToUsd(hour.heldMicros),
        limitUsd: microsToUsd(limits.orgHourlyMicros),
        resetsAt: windows.hourEnd.toISOString(),
      },
      day: {
        spentUsd: microsToUsd(day.spentMicros),
        heldUsd: microsToUsd(day.heldMicros),
        limitUsd: microsToUsd(limits.orgDailyMicros),
        resetsAt: windows.dayEnd.toISOString(),
      },
    },
    workflows: rows.map((row) => ({
      workflowId: row.id,
      name: row.name,
      enabled: row.enabled,
      hourlyOverrideUsd: override(row.workflow_hourly_micros),
      dailyOverrideUsd: override(row.workflow_daily_micros),
      hourlyLimitUsd: override(row.workflow_hourly_micros) ?? microsToUsd(limits.workflowHourlyMicros),
      dailyLimitUsd: override(row.workflow_daily_micros) ?? microsToUsd(limits.workflowDailyMicros),
      spentTodayUsd: microsToUsd(Number(row.spent_today)),
    })),
    alerts,
  };
}

function amount(value: unknown, label: string, ceilingMicros: number): number {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) {
    throw new SpendSettingsError(`${label} must be a positive dollar amount.`);
  }
  const micros = usdToMicros(value);
  if (micros > ceilingMicros) {
    throw new SpendSettingsError(`${label} cannot exceed $${microsToUsd(ceilingMicros).toFixed(2)}.`);
  }
  return micros;
}

export async function saveSpendLimits(
  orgId: string,
  actorUserId: string | null,
  draft: Partial<SpendLimitsUsd> & { metricRefresh?: MetricRefresh },
): Promise<SpendSettings> {
  const ceilings = spendCeilingsMicros();
  const next = {
    runCap: amount(draft.runCapUsd, "Per-run cap", ceilings.runCapMicros),
    orgHourly: amount(draft.orgHourlyUsd, "Organization hourly budget", ceilings.orgHourlyMicros),
    orgDaily: amount(draft.orgDailyUsd, "Organization daily budget", ceilings.orgDailyMicros),
    workflowHourly: amount(draft.workflowHourlyUsd, "Workflow hourly budget", ceilings.workflowHourlyMicros),
    workflowDaily: amount(draft.workflowDailyUsd, "Workflow daily budget", ceilings.workflowDailyMicros),
  };
  const warn = draft.warnPercent;
  if (typeof warn !== "number" || !Number.isInteger(warn) || warn < 50 || warn > 99) {
    throw new SpendSettingsError("Warning threshold must be a whole number from 50 to 99.");
  }
  if (next.orgHourly > next.orgDaily) {
    throw new SpendSettingsError("Organization hourly budget cannot exceed the daily budget.");
  }
  if (next.workflowHourly > next.workflowDaily) {
    throw new SpendSettingsError("Workflow hourly budget cannot exceed the daily budget.");
  }
  if (next.runCap > next.orgHourly || next.runCap > next.workflowHourly) {
    throw new SpendSettingsError("Per-run cap cannot exceed an hourly budget, or no run could start.");
  }
  if (draft.metricRefresh !== undefined && !METRIC_REFRESH_VALUES.includes(draft.metricRefresh)) {
    throw new SpendSettingsError("Metric refresh must be card, daily, weekly or off.");
  }
  const previous = await getSpendSettings(orgId);
  const metricRefresh = draft.metricRefresh ?? previous.metricRefresh;
  const { rowCount } = await pool().query(
    `update spend_limit
        set run_cap_micros = $2, org_hourly_micros = $3, org_daily_micros = $4,
            workflow_hourly_micros = $5, workflow_daily_micros = $6, warn_percent = $7,
            metric_refresh = $9, updated_by_user_id = $8, updated_at = now()
      where org_id = $1 and workflow_id is null`,
    [
      orgId, next.runCap, next.orgHourly, next.orgDaily, next.workflowHourly, next.workflowDaily, warn, actorUserId,
      metricRefresh === "card" ? null : metricRefresh,
    ],
  );
  if (!rowCount) throw new SpendSettingsError("This organization has no spend limits to update.");
  const saved = await getSpendSettings(orgId);
  await recordAuditEvent({
    orgId,
    entityKind: "spend_limit",
    entityId: orgId,
    event: "spend:limit_changed",
    payload: {
      actorUserId,
      previous: { ...previous.limits, metricRefresh: previous.metricRefresh },
      next: { ...saved.limits, metricRefresh: saved.metricRefresh },
    },
  });
  return saved;
}

export async function saveWorkflowSpendOverride(
  orgId: string,
  actorUserId: string | null,
  workflowId: string,
  draft: { hourlyUsd: number | null; dailyUsd: number | null },
): Promise<SpendSettings> {
  const ceilings = spendCeilingsMicros();
  const workflow = await pool().query("select 1 from workflow_definition where org_id = $1 and id = $2::uuid", [
    orgId,
    workflowId,
  ]);
  if (!workflow.rowCount) throw new SpendSettingsError("Workflow not found.");
  const hourly = draft.hourlyUsd === null ? null : amount(draft.hourlyUsd, "Workflow hourly budget", ceilings.workflowHourlyMicros);
  const daily = draft.dailyUsd === null ? null : amount(draft.dailyUsd, "Workflow daily budget", ceilings.workflowDailyMicros);
  if (hourly !== null && daily !== null && hourly > daily) {
    throw new SpendSettingsError("Workflow hourly budget cannot exceed the daily budget.");
  }
  if (hourly === null && daily === null) {
    await pool().query("delete from spend_limit where org_id = $1 and workflow_id = $2::uuid", [orgId, workflowId]);
  } else {
    await pool().query(
      `insert into spend_limit (org_id, workflow_id, workflow_hourly_micros, workflow_daily_micros, updated_by_user_id)
       values ($1, $2::uuid, $3, $4, $5)
       on conflict (org_id, workflow_id) where workflow_id is not null
       do update set workflow_hourly_micros = excluded.workflow_hourly_micros,
                     workflow_daily_micros = excluded.workflow_daily_micros,
                     updated_by_user_id = excluded.updated_by_user_id,
                     updated_at = now()`,
      [orgId, workflowId, hourly, daily, actorUserId],
    );
  }
  await recordAuditEvent({
    orgId,
    entityKind: "spend_limit",
    entityId: workflowId,
    event: "spend:limit_changed",
    payload: {
      actorUserId,
      workflowId,
      next: { hourlyUsd: hourly === null ? null : microsToUsd(hourly), dailyUsd: daily === null ? null : microsToUsd(daily) },
    },
  });
  return getSpendSettings(orgId);
}
