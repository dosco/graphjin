import type { MetricRefresh } from "@neko/llm/spend";

const HOUR_MS = 60 * 60 * 1_000;

export function metricCadenceMs(cadence: string): number {
  switch (cadence.trim().toLowerCase()) {
    case "hourly":
      return HOUR_MS;
    case "weekly":
      return 7 * 24 * HOUR_MS;
    case "daily":
    default:
      // Unknown legacy values fall back to the prior daily behavior instead
      // of creating an accidental high-frequency refresh loop.
      return 24 * HOUR_MS;
  }
}

/** The organization's metric refresh setting slows a card, and never speeds one up. */
export function metricRefreshIsDue(input: {
  cadence: string;
  lastRefreshStatus: string | null;
  updatedAt: Date;
  orgRefresh?: MetricRefresh;
  now?: Date;
}): boolean {
  if (input.lastRefreshStatus === "pending" || input.orgRefresh === "off") return false;
  const now = input.now ?? new Date();
  const orgMs = input.orgRefresh === "daily" || input.orgRefresh === "weekly" ? metricCadenceMs(input.orgRefresh) : 0;
  return input.updatedAt.getTime() + Math.max(metricCadenceMs(input.cadence), orgMs) <= now.getTime();
}
