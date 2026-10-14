"use client";

import { useId } from "react";

import {
  AreaChart,
  Area,
  BarChart,
  Bar,
  LineChart,
  Line,
  PieChart,
  Pie,
  Cell,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts";
import { ChartContainer } from "@/components/ui/chart";
import type { ChartDataPoint } from "@/a2ui/catalog";
export type { ChartDataPoint } from "@/a2ui/catalog";
import { formatCompact } from "@/lib/format-number";

const AXIS_FILL = "var(--text3)";
const GRID_STROKE = "var(--border)";
const CARD_FILL = "var(--card)";
const MUTED_FILL = "var(--text2)";
const BASELINE_STROKE = "var(--text)";
const DEFAULT_ACCENT = "var(--accent)";

const axisProps = {
  tick: { fontSize: "var(--type-label)", fill: AXIS_FILL },
  axisLine: false,
  tickLine: false,
} as const;
const tooltipStyle = {
  contentStyle: {
    borderRadius: 10,
    border: "none",
    boxShadow: "var(--shadow-h)",
    fontSize: "var(--type-body-sm)",
  },
  itemStyle: { color: MUTED_FILL },
};

function donutFills(accent: string) {
  return [
    accent,
    `color-mix(in srgb, ${accent} 78%, white)`,
    `color-mix(in srgb, ${accent} 62%, white)`,
    `color-mix(in srgb, ${accent} 50%, var(--text2))`,
    `color-mix(in srgb, ${accent} 38%, var(--text3))`,
    `color-mix(in srgb, ${accent} 28%, var(--border))`,
  ];
}

export default function Chart({
  type,
  accent = DEFAULT_ACCENT,
  h = 130,
  data,
  centerLabel,
  valueLabel,
  baselineLabel,
  maxBarSize = 16,
  onSelect,
}: {
  type: string;
  accent?: string;
  h?: number;
  data: ChartDataPoint[];
  centerLabel?: string;
  valueLabel?: string;
  baselineLabel?: string;
  maxBarSize?: number;
  /** Called with a point's label when the reader points at it or taps it. */
  onSelect?: (label: string) => void;
}) {
  const chartId = useId();
  if (!data || data.length === 0) return null;
  const pick = (state: { activeLabel?: string | number } | null) => {
    if (state?.activeLabel !== undefined) onSelect?.(String(state.activeLabel));
  };
  const selectable = onSelect
    ? { onClick: pick, onMouseMove: pick, style: { cursor: "pointer" } }
    : {};

  // Defensive: agents sometimes mis-classify chartType. Coerce to a sensible
  // shape based on the actual data.
  // - KPI demands exactly 1 item; multi-item data is a category breakdown.
  // - Non-kpi types with a single point are degenerate; treat as kpi.
  let resolvedType = type;
  if (type === "kpi" && data.length > 1) {
    resolvedType = data.length <= 6 ? "donut" : "bar";
  } else if (type !== "kpi" && data.length === 1) {
    resolvedType = "kpi";
  }

  if (resolvedType === "kpi") {
    // kpi data isn't a visualization — it's a headline number with a
    // baseline. Use <KpiHeadline> for that. Chart returns null here so
    // any caller that accidentally passes kpi to Chart silently no-ops
    // instead of rendering empty axes; lint/tests should catch the
    // misuse but this is a defensive fallback.
    return null;
  }

  if (resolvedType === "donut") {
    const colors = donutFills(accent);
    const total = data.reduce((s, d) => s + d.v, 0);
    const donutFormatter = (
      value: number | string | undefined,
      name: string | number | undefined,
    ) =>
      [
        `${((Number(value ?? 0) / total) * 100).toFixed(0)}%`,
        String(name ?? ""),
      ] as [string, string];
    return (
      <ChartContainer
        config={{}}
        className="aspect-auto w-full"
        style={{ height: h }}
        initialDimension={{ width: 320, height: h }}
      >
        <PieChart>
          <Pie
            data={data}
            dataKey="v"
            nameKey="d"
            cx="50%"
            cy="50%"
            innerRadius={h * 0.25}
            outerRadius={h * 0.4}
            paddingAngle={2}
            strokeWidth={0}
            {...(onSelect
              ? {
                  onClick: (entry: { d?: string }) => entry?.d && onSelect(entry.d),
                  onMouseEnter: (entry: { d?: string }) => entry?.d && onSelect(entry.d),
                  style: { cursor: "pointer" },
                }
              : {})}
          >
            {data.map((_, i) => (
              <Cell key={i} fill={colors[i % colors.length]} />
            ))}
          </Pie>
          <Tooltip {...tooltipStyle} formatter={donutFormatter as never} />
          <text
            x="50%"
            y="50%"
            textAnchor="middle"
            dominantBaseline="middle"
            style={{ fontSize: "var(--type-body-sm)", fill: MUTED_FILL }}
          >
            {centerLabel ?? formatCompact(total)}
          </text>
        </PieChart>
      </ChartContainer>
    );
  }

  const seriesFormatter = (
    value: number | string | undefined,
    name: string | number | undefined,
  ) =>
    [
      formatCompact(Number(value ?? 0)),
      name === "v"
        ? (valueLabel ?? "Value")
        : name === "t"
          ? (baselineLabel ?? "Prior")
          : String(name),
    ] as [string, string];
  const yTickFormatter = (v: number) => formatCompact(v);

  if (resolvedType === "area") {
    const gradientId = `chart-area-fill-${chartId.replace(/:/g, "")}`;
    return (
      <ChartContainer
        config={{}}
        className="aspect-auto w-full"
        style={{ height: h }}
        initialDimension={{ width: 320, height: h }}
      >
        <AreaChart data={data} {...selectable}>
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={accent} stopOpacity={0.25} />
              <stop offset="100%" stopColor={accent} stopOpacity={0.01} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke={GRID_STROKE} />
          <XAxis dataKey="d" {...axisProps} />
          <YAxis {...axisProps} width={32} tickFormatter={yTickFormatter} />
          <Tooltip {...tooltipStyle} formatter={seriesFormatter as never} />
          <Area
            type="monotone"
            dataKey="v"
            name={valueLabel ?? "Value"}
            stroke={accent}
            strokeWidth={2}
            fill={`url(#${gradientId})`}
            dot={{ r: 3, fill: CARD_FILL, stroke: accent, strokeWidth: 1.5 }}
          />
        </AreaChart>
      </ChartContainer>
    );
  }

  if (resolvedType === "bar") {
    return (
      <ChartContainer
        config={{}}
        className="aspect-auto w-full"
        style={{ height: h }}
        initialDimension={{ width: 320, height: h }}
      >
        <BarChart data={data} maxBarSize={maxBarSize} barGap={3} {...selectable}>
          <CartesianGrid strokeDasharray="3 3" stroke={GRID_STROKE} />
          <XAxis dataKey="d" {...axisProps} />
          <YAxis {...axisProps} width={32} tickFormatter={yTickFormatter} />
          <Tooltip {...tooltipStyle} formatter={seriesFormatter as never} />
          <Bar
            dataKey="v"
            name={valueLabel ?? "Value"}
            fill={accent}
            radius={[5, 5, 0, 0]}
          />
          <Bar
            dataKey="t"
            name={baselineLabel ?? "Prior"}
            fill={`color-mix(in srgb, ${accent} 33%, transparent)`}
            radius={[5, 5, 0, 0]}
          />
        </BarChart>
      </ChartContainer>
    );
  }

  return (
    <ChartContainer
      config={{}}
      className="aspect-auto w-full"
      style={{ height: h }}
      initialDimension={{ width: 320, height: h }}
    >
      <LineChart data={data} {...selectable}>
        <CartesianGrid strokeDasharray="3 3" stroke={GRID_STROKE} />
        <XAxis dataKey="d" {...axisProps} />
        <YAxis {...axisProps} width={32} tickFormatter={yTickFormatter} />
        <Tooltip {...tooltipStyle} formatter={seriesFormatter as never} />
        <Line
          type="monotone"
          dataKey="v"
          name={valueLabel ?? "Value"}
          stroke={accent}
          strokeWidth={2.5}
          dot={{ r: 3, fill: CARD_FILL, stroke: accent, strokeWidth: 2 }}
        />
        <Line
          type="monotone"
          dataKey="t"
          name={baselineLabel ?? "Prior"}
          stroke={BASELINE_STROKE}
          strokeOpacity={0.55}
          strokeWidth={1.75}
          strokeDasharray="5 4"
          dot={false}
        />
      </LineChart>
    </ChartContainer>
  );
}
