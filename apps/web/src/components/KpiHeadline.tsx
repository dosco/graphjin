"use client";

import type { ChartDataPoint } from "./Chart";

/**
 * Renders a single KPI metric: headline number + label + period-over-period
 * delta arrow.
 *
 * Why this exists as its own component: chartType="kpi" doesn't map to a
 * traditional chart visualization — it's a single number with a baseline.
 * Rather than overload `<Chart type="kpi" />` (and have it return null,
 * silently dropping the metric for any caller that doesn't know to render
 * the headline themselves), the kpi presentation lives here. Both
 * BriefingCard and ChatBubble use it, so kpi data renders consistently
 * everywhere instead of being re-implemented per surface.
 */
export interface KpiHeadlineProps {
  /** Pre-formatted display string, e.g. "$4.7M", "100%", "31,465". */
  metric: string;
  /** Short label under the metric, e.g. "Revenue MTD". */
  label?: string;
  /** Single data point: v = current, t = baseline (prior period). */
  data?: ChartDataPoint[];
  /** Visual size — chat bubbles want it bigger; card meta-rows want it small. */
  size?: "card" | "chat";
  /** Card mood — drives the delta pill color so a rising "bad" metric reads red. */
  mood?: string;
}

export default function KpiHeadline({ metric, label, data, size = "card", mood }: KpiHeadlineProps) {
  const delta = computeDelta(data);
  const deltaClass = delta ? deltaToneClass(delta, mood) : "";
  return (
    <div className={`kpi kpi-${size}`}>
      <span className="kpi-metric">{metric}</span>
      {label && <span className="kpi-label">{label}</span>}
      {delta && (
        <span className={`kpi-delta ${deltaClass}`}>
          <span aria-hidden="true">{delta.isUp ? "↑" : "↓"}</span>
          {formatDeltaPct(delta.pct)}
        </span>
      )}
    </div>
  );
}

export function formatDeltaPct(pct: number): string {
  const abs = Math.abs(pct);
  if (abs >= 10) return `${Math.round(abs)}%`;
  return `${abs.toFixed(1)}%`;
}

export function deltaToneClass(
  delta: { isUp: boolean; isBigDrop: boolean },
  mood: string | undefined,
): string {
  if (mood === "act" || mood === "bad") return "kpi-delta-crash";
  if (mood === "watch") return "kpi-delta-down";
  if (delta.isBigDrop) return "kpi-delta-crash";
  return delta.isUp ? "kpi-delta-up" : "kpi-delta-down";
}

function computeDelta(data?: ChartDataPoint[]) {
  if (!data || data.length !== 1) return null;
  const item = data[0];
  if (item.t == null || item.t === 0) return null;
  const pct = ((item.v - item.t) / item.t) * 100;
  if (Math.abs(pct) < 0.05) return null;
  return { pct, isUp: pct >= 0, isBigDrop: pct <= -20 };
}
