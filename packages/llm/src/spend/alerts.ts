import { pool } from "@neko/db";
import { spendBreakdownMicros, spendWindows, type SpendBudget } from "./admission";
import { loadSpendLimits, microsToUsd, SpendLimitsMissing, type SpendQueryable } from "./limits";

export type SpendAlertKind = "spend.budget_warning" | "spend.budget_blocked" | "spend.price_unknown";

export type SpendAlert = {
  id: string;
  kind: SpendAlertKind;
  subject: string;
  message: string;
  createdAt: string;
};

const cents = (micros: number) => Math.min(2_147_483_647, Math.round(micros / 10_000));

const BUDGET_LABEL: Record<SpendBudget, string> = {
  org_hourly: "hourly budget for this organization",
  org_daily: "daily budget for this organization",
  workflow_hourly: "hourly budget for the workflow",
  workflow_daily: "daily budget for the workflow",
};

export function budgetWarningMessage(input: {
  budget: SpendBudget;
  percent: number;
  committedUsd: number;
  limitUsd: number;
  resetsAt: Date;
  workflowName?: string | null;
}): string {
  const label = input.workflowName
    ? BUDGET_LABEL[input.budget].replace("the workflow", input.workflowName)
    : BUDGET_LABEL[input.budget];
  return (
    `Spend is at ${Math.floor(input.percent)}% of the $${input.limitUsd.toFixed(2)} ${label} ` +
    `($${input.committedUsd.toFixed(2)} so far). New runs stop at $${input.limitUsd.toFixed(2)}. ` +
    `The budget resets at ${input.resetsAt.toISOString().slice(11, 16)} UTC.`
  );
}

/** Insert once per kind, subject and window; returns false when the alert already exists. */
export async function raiseSpendAlert(input: {
  orgId: string;
  kind: SpendAlertKind;
  subject: string;
  observedMicros: number;
  thresholdMicros: number;
  windowSeconds: number;
  windowStart: Date;
  message: string;
  details?: Record<string, unknown>;
}): Promise<boolean> {
  const details = {
    ...input.details,
    message: input.message,
    windowStart: input.windowStart.toISOString(),
    observedMicros: input.observedMicros,
    thresholdMicros: input.thresholdMicros,
  };
  const { rowCount } = await pool().query(
    `insert into behavior_alert (org_id, kind, subject, observed, threshold, window_seconds, details)
     values ($1, $2, $3, $4, $5, $6, $7::jsonb)
     on conflict (org_id, kind, subject, (details->>'windowStart')) where kind like 'spend.%' do nothing`,
    [
      input.orgId,
      input.kind,
      input.subject,
      cents(input.observedMicros),
      cents(input.thresholdMicros),
      input.windowSeconds,
      JSON.stringify(details),
    ],
  );
  if (!rowCount) return false;
  try {
    const { dispatchExternalEvent } = await import("../workflows/external-events");
    await dispatchExternalEvent({
      orgId: input.orgId,
      event: {
        name: input.kind,
        source: "spend-governance",
        payload: { subject: input.subject, ...details },
        dedupeKey: `${input.kind}:${input.subject}:${details.windowStart}`,
      },
    });
  } catch (error) {
    console.warn(`[spend] alert dispatch failed: ${error instanceof Error ? error.message : error}`);
  }
  return true;
}

/** Warn once per window when committed spend first reaches the warning share of a budget. */
export async function checkSpendWarnings(
  client: SpendQueryable,
  input: { orgId: string; workflowId?: string | null; now?: Date },
): Promise<number> {
  const now = input.now ?? new Date();
  let limits;
  try {
    limits = await loadSpendLimits(client, input.orgId, input.workflowId);
  } catch (error) {
    if (error instanceof SpendLimitsMissing) return 0;
    throw error;
  }
  const windows = spendWindows(now);
  let workflowName: string | null = null;
  if (input.workflowId) {
    const { rows } = await client.query<{ name: string }>(
      "select name from workflow_definition where org_id = $1 and id = $2::uuid",
      [input.orgId, input.workflowId],
    );
    workflowName = rows[0]?.name ?? null;
  }
  const budgets: Array<{ budget: SpendBudget; limit: number; since: Date; resetsAt: Date; seconds: number; workflowId: string | null }> = [
    { budget: "org_hourly", limit: limits.orgHourlyMicros, since: windows.hourStart, resetsAt: windows.hourEnd, seconds: 3_600, workflowId: null },
    { budget: "org_daily", limit: limits.orgDailyMicros, since: windows.dayStart, resetsAt: windows.dayEnd, seconds: 86_400, workflowId: null },
  ];
  if (input.workflowId) {
    budgets.push(
      { budget: "workflow_hourly", limit: limits.workflowHourlyMicros, since: windows.hourStart, resetsAt: windows.hourEnd, seconds: 3_600, workflowId: input.workflowId },
      { budget: "workflow_daily", limit: limits.workflowDailyMicros, since: windows.dayStart, resetsAt: windows.dayEnd, seconds: 86_400, workflowId: input.workflowId },
    );
  }
  let raised = 0;
  for (const budget of budgets) {
    const { spentMicros, heldMicros } = await spendBreakdownMicros(client, {
      orgId: input.orgId,
      workflowId: budget.workflowId,
      since: budget.since,
    });
    const committed = spentMicros + heldMicros;
    const threshold = Math.ceil((budget.limit * limits.warnPercent) / 100);
    if (committed < threshold) continue;
    const created = await raiseSpendAlert({
      orgId: input.orgId,
      kind: "spend.budget_warning",
      subject: budget.workflowId ? `workflow:${budget.workflowId}` : "org",
      observedMicros: committed,
      thresholdMicros: threshold,
      windowSeconds: budget.seconds,
      windowStart: budget.since,
      message: budgetWarningMessage({
        budget: budget.budget,
        percent: (committed / budget.limit) * 100,
        committedUsd: microsToUsd(committed),
        limitUsd: microsToUsd(budget.limit),
        resetsAt: budget.resetsAt,
        workflowName,
      }),
      details: { budget: budget.budget, limitMicros: budget.limit, warnPercent: limits.warnPercent },
    });
    if (created) raised += 1;
  }
  return raised;
}

export async function listOpenSpendAlerts(orgId: string, limit = 20): Promise<SpendAlert[]> {
  const { rows } = await pool().query<{
    id: string;
    kind: SpendAlertKind;
    subject: string;
    message: string | null;
    created_at: Date;
  }>(
    `select id, kind, subject, details->>'message' as message, created_at
       from behavior_alert
      where org_id = $1 and kind like 'spend.%' and acknowledged_at is null
      order by created_at desc
      limit $2`,
    [orgId, limit],
  );
  return rows.map((row) => ({
    id: row.id,
    kind: row.kind,
    subject: row.subject,
    message: row.message ?? row.kind,
    createdAt: row.created_at.toISOString(),
  }));
}

export async function acknowledgeSpendAlert(orgId: string, alertId: string, actorUserId: string | null): Promise<boolean> {
  const { rowCount } = await pool().query(
    `update behavior_alert set acknowledged_at = now(), acknowledged_by = $3
      where org_id = $1 and id = $2::uuid and kind like 'spend.%' and acknowledged_at is null`,
    [orgId, alertId, actorUserId],
  );
  return (rowCount ?? 0) > 0;
}
