export type SpendQueryable = {
  query<T = Record<string, unknown>>(
    text: string,
    values?: unknown[],
  ): Promise<{ rows: T[] }>;
};

export type SpendLimits = {
  runCapMicros: number;
  orgHourlyMicros: number;
  orgDailyMicros: number;
  workflowHourlyMicros: number;
  workflowDailyMicros: number;
  warnPercent: number;
};

/** How often scheduled metric refreshes may run. "card" keeps each card's own cadence. */
export type MetricRefresh = "card" | "daily" | "weekly" | "off";

export const METRIC_REFRESH_VALUES: readonly MetricRefresh[] = ["card", "daily", "weekly", "off"];

export function metricRefreshFromColumn(value: string | null | undefined): MetricRefresh {
  return value === "daily" || value === "weekly" || value === "off" ? value : "card";
}

/** Each organization's metric refresh setting, keyed by organization ID. */
export async function loadMetricRefreshSettings(client: SpendQueryable): Promise<Map<string, MetricRefresh>> {
  const { rows } = await client.query<{ org_id: string; metric_refresh: string | null }>(
    "select org_id, metric_refresh from spend_limit where workflow_id is null",
  );
  return new Map(rows.map((row) => [row.org_id, metricRefreshFromColumn(row.metric_refresh)]));
}

type CeilingKey = Exclude<keyof SpendLimits, "warnPercent">;

const DEFAULT_CEILINGS_USD: Record<CeilingKey, number> = {
  runCapMicros: 50,
  orgHourlyMicros: 500,
  orgDailyMicros: 2_000,
  workflowHourlyMicros: 100,
  workflowDailyMicros: 500,
};

const CEILING_ENV: Record<CeilingKey, string> = {
  runCapMicros: "OPENNEKO_SPEND_CEILING_RUN_CAP_USD",
  orgHourlyMicros: "OPENNEKO_SPEND_CEILING_ORG_HOURLY_USD",
  orgDailyMicros: "OPENNEKO_SPEND_CEILING_ORG_DAILY_USD",
  workflowHourlyMicros: "OPENNEKO_SPEND_CEILING_WORKFLOW_HOURLY_USD",
  workflowDailyMicros: "OPENNEKO_SPEND_CEILING_WORKFLOW_DAILY_USD",
};

export function usdToMicros(usd: number): number {
  return Math.max(0, Math.ceil(usd * 1_000_000));
}

export function microsToUsd(micros: number): number {
  return Math.round(micros) / 1_000_000;
}

/** Operator-level ceilings. An org limit above its ceiling is clamped. */
export function spendCeilingsMicros(
  env: Record<string, string | undefined> = process.env,
): Record<CeilingKey, number> {
  const out = {} as Record<CeilingKey, number>;
  for (const key of Object.keys(DEFAULT_CEILINGS_USD) as CeilingKey[]) {
    const raw = Number(env[CEILING_ENV[key]]);
    const usd = Number.isFinite(raw) && raw > DEFAULT_CEILINGS_USD[key] ? raw : DEFAULT_CEILINGS_USD[key];
    out[key] = usdToMicros(usd);
  }
  return out;
}

export class SpendLimitsMissing extends Error {
  constructor(orgId: string) {
    super(`No spend limits are configured for organization ${orgId}.`);
    this.name = "SpendLimitsMissing";
  }
}

type LimitRow = {
  workflow_id: string | null;
  run_cap_micros: string | null;
  org_hourly_micros: string | null;
  org_daily_micros: string | null;
  workflow_hourly_micros: string | null;
  workflow_daily_micros: string | null;
  warn_percent: number | null;
};

export async function loadSpendLimits(
  client: SpendQueryable,
  orgId: string,
  workflowId?: string | null,
): Promise<SpendLimits> {
  const { rows } = await client.query<LimitRow>(
    `select workflow_id, run_cap_micros, org_hourly_micros, org_daily_micros,
            workflow_hourly_micros, workflow_daily_micros, warn_percent
       from spend_limit
      where org_id = $1 and (workflow_id is null or workflow_id = $2::uuid)`,
    [orgId, workflowId ?? null],
  );
  const org = rows.find((row) => row.workflow_id === null);
  if (!org) throw new SpendLimitsMissing(orgId);
  const override = rows.find((row) => row.workflow_id !== null);
  const ceilings = spendCeilingsMicros();
  const pick = (key: CeilingKey, value: string | null | undefined, fallback: string | null) =>
    Math.min(Number(value ?? fallback), ceilings[key]);
  return {
    runCapMicros: pick("runCapMicros", org.run_cap_micros, null),
    orgHourlyMicros: pick("orgHourlyMicros", org.org_hourly_micros, null),
    orgDailyMicros: pick("orgDailyMicros", org.org_daily_micros, null),
    workflowHourlyMicros: pick("workflowHourlyMicros", override?.workflow_hourly_micros, org.workflow_hourly_micros),
    workflowDailyMicros: pick("workflowDailyMicros", override?.workflow_daily_micros, org.workflow_daily_micros),
    warnPercent: org.warn_percent ?? 80,
  };
}
