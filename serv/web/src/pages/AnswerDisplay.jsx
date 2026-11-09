import React from "react";

import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

// Answer displays draw the engine's display hints under an answer. A hint
// already says which field names the rows, which is a measure and which is a
// time, and suggests a form; nothing here reads the answer's prose. A value
// hint draws nothing: the answer already states the number.

const SERIES = ["var(--viz-1)", "var(--viz-2)", "var(--viz-3)"];
const MAX_HINTS = 3;
const TABLE_PREVIEW_ROWS = 8;

export function AnswerDisplays({ hints }) {
  const shown = (Array.isArray(hints) ? hints : []).filter((hint) => displayable(hint)).slice(0, MAX_HINTS);
  if (shown.length === 0) {
    return null;
  }
  return (
    <div className="grid w-full min-w-0 grid-cols-[minmax(0,1fr)] gap-6">
      {shown.map((hint, index) => <AnswerDisplay key={`${hint.root}-${index}`} hint={hint} labelled={shown.length > 1} />)}
    </div>
  );
}

function displayable(hint) {
  return hint && Array.isArray(hint.rows) && hint.rows.length > 0 && Array.isArray(hint.columns) && hint.kind !== "value";
}

export function AnswerDisplay({ hint, labelled = false }) {
  const plan = React.useMemo(() => chartPlan(hint), [hint]);
  const [showTable, setShowTable] = React.useState(false);
  const caption = (labelled && hint.question) || plan.caption;
  const rowNote = hint.truncated ? `first ${hint.rows.length} of ${formatNumber(hint.row_count)} rows` : `${formatNumber(hint.rows.length)} ${hint.rows.length === 1 ? "row" : "rows"}`;

  if (!plan.chart) {
    return (
      <figure className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-2">
        <figcaption className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
          <span className="min-w-0 truncate">{caption}</span>
          <Button type="button" variant="ghost" size="sm" onClick={() => setShowTable((value) => !value)} aria-expanded={showTable}>
            {showTable ? "Hide table" : `Show table · ${rowNote}`}
          </Button>
        </figcaption>
        {showTable && <DisplayTable hint={hint} />}
      </figure>
    );
  }
  return (
    <figure className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-2">
      <figcaption className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
        <span className="min-w-0 truncate">{caption}</span>
        <Button type="button" variant="ghost" size="sm" onClick={() => setShowTable((value) => !value)} aria-pressed={showTable}>
          {showTable ? "Chart" : "Table"}
        </Button>
      </figcaption>
      {showTable ? <DisplayTable hint={hint} /> : plan.chart === "bar" ? <BarDisplay plan={plan} /> : <LineDisplay plan={plan} />}
      {hint.truncated && <p className="text-xs text-muted-foreground">Showing the {rowNote}.</p>}
    </figure>
  );
}

// chartPlan reads the hint's columns into what a form needs. A hint whose
// values don't fit the form it suggests (a negative bar, an unreadable time)
// falls back to a table rather than drawing something misleading.
export function chartPlan(hint) {
  const columns = hint?.columns || [];
  const rows = hint?.rows || [];
  const measures = columns.filter((column) => column.role === "measure");
  const labels = columns.filter((column) => column.role === "text" || column.role === "reference");
  const time = columns.find((column) => column.role === "time");
  const table = { chart: "", caption: humanize(hint?.root || "result") };

  if (hint?.kind === "bar" && labels.length === 1 && measures.length === 1) {
    const label = labels[0];
    const measure = measures[0];
    const bars = rows.map((row) => ({ label: textValue(valueAt(row, label.name)), value: numberValue(valueAt(row, measure.name)) }));
    if (bars.some((bar) => bar.value == null || bar.value < 0)) {
      return table;
    }
    return { chart: "bar", caption: `${columnTitle(measure)} by ${columnTitle(label).toLowerCase()}`, measure, bars, max: Math.max(...bars.map((bar) => bar.value)) };
  }

  if (hint?.kind === "line" && time && measures.length >= 1) {
    const series = measures.slice(0, SERIES.length);
    const points = rows
      .map((row) => ({ x: timeValue(valueAt(row, time.name)), raw: valueAt(row, time.name), values: series.map((measure) => numberValue(valueAt(row, measure.name))) }))
      .filter((point) => point.x != null);
    if (points.length < 2 || points.some((point) => point.values.some((value) => value == null))) {
      return table;
    }
    points.sort((a, b) => a.x - b.x);
    return { chart: "line", caption: `${series.map(columnTitle).join(", ")} over ${columnTitle(time).toLowerCase()}`, series, points, time };
  }
  return table;
}

function BarDisplay({ plan }) {
  const max = plan.max || 1;
  return (
    <ol className="grid gap-1.5" aria-label={plan.caption}>
      {plan.bars.map((bar, index) => (
        <li key={`${bar.label}-${index}`} className="grid grid-cols-[minmax(0,9rem)_minmax(0,1fr)_auto] items-center gap-3 text-sm sm:grid-cols-[minmax(0,14rem)_minmax(0,1fr)_auto]">
          <span className="truncate" title={bar.label}>{bar.label}</span>
          <span className="flex min-w-0 items-center" aria-hidden="true">
            <span className="h-3.5 rounded-r-[4px]" style={{ width: `${Math.max((bar.value / max) * 100, bar.value > 0 ? 1 : 0)}%`, background: SERIES[0] }} />
          </span>
          <span className="text-right text-xs tabular-nums text-muted-foreground">{formatNumber(bar.value)}</span>
        </li>
      ))}
    </ol>
  );
}

const LINE_HEIGHT = 180;
const MARGIN = { top: 12, right: 16, bottom: 26, left: 48 };

function LineDisplay({ plan }) {
  const [ref, width] = useWidth(560);
  const [active, setActive] = React.useState(-1);
  const { points, series } = plan;
  const innerWidth = Math.max(width - MARGIN.left - MARGIN.right, 40);
  const innerHeight = LINE_HEIGHT - MARGIN.top - MARGIN.bottom;
  const values = points.flatMap((point) => point.values);
  const ticks = niceTicks(Math.min(0, ...values), Math.max(...values));
  const yMin = ticks[0];
  const yMax = ticks[ticks.length - 1];
  const x0 = points[0].x;
  const xSpan = points[points.length - 1].x - x0 || 1;
  const xAt = (point) => MARGIN.left + ((point.x - x0) / xSpan) * innerWidth;
  const yAt = (value) => MARGIN.top + innerHeight - ((value - yMin) / (yMax - yMin || 1)) * innerHeight;
  const compact = Math.max(Math.abs(yMin), Math.abs(yMax)) >= 10000;
  const dateOptions = dateFormatOptions(points);

  const pick = (clientX) => {
    const box = ref.current?.getBoundingClientRect();
    if (!box) {
      return;
    }
    const x = clientX - box.left;
    let nearest = 0;
    points.forEach((point, index) => {
      if (Math.abs(xAt(point) - x) < Math.abs(xAt(points[nearest]) - x)) {
        nearest = index;
      }
    });
    setActive(nearest);
  };
  const onKeyDown = (event) => {
    if (event.key === "ArrowRight" || event.key === "ArrowLeft") {
      event.preventDefault();
      const step = event.key === "ArrowRight" ? 1 : -1;
      setActive((current) => Math.min(points.length - 1, Math.max(0, (current < 0 ? (step > 0 ? -1 : points.length) : current) + step)));
    }
  };
  const last = points[points.length - 1];
  const point = active >= 0 ? points[active] : null;

  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-2">
      {series.length > 1 && (
        <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-label="Legend">
          {series.map((measure, index) => (
            <li key={measure.name} className="flex items-center gap-1.5">
              <span className="h-0.5 w-3 rounded-full" style={{ background: SERIES[index] }} aria-hidden="true" />
              {columnTitle(measure)}
            </li>
          ))}
        </ul>
      )}
      <div
        ref={ref}
        className="relative w-full min-w-0 outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
        tabIndex={0}
        role="img"
        aria-label={`${plan.caption}, ${points.length} points from ${formatDate(points[0].raw, dateOptions)} to ${formatDate(last.raw, dateOptions)}`}
        onPointerMove={(event) => pick(event.clientX)}
        onPointerLeave={() => setActive(-1)}
        onKeyDown={onKeyDown}
        onBlur={() => setActive(-1)}
      >
        <svg width={width} height={LINE_HEIGHT} viewBox={`0 0 ${width} ${LINE_HEIGHT}`} className="block max-w-full overflow-visible" aria-hidden="true">
          {ticks.map((tick) => (
            <g key={tick}>
              <line x1={MARGIN.left} x2={MARGIN.left + innerWidth} y1={yAt(tick)} y2={yAt(tick)} stroke="var(--border)" strokeWidth="1" />
              <text x={MARGIN.left - 8} y={yAt(tick)} dy="0.32em" textAnchor="end" className="fill-muted-foreground text-[0.6875rem] tabular-nums">{formatNumber(tick, compact)}</text>
            </g>
          ))}
          <text x={MARGIN.left} y={LINE_HEIGHT - 6} className="fill-muted-foreground text-[0.6875rem]">{formatDate(points[0].raw, dateOptions)}</text>
          <text x={MARGIN.left + innerWidth} y={LINE_HEIGHT - 6} textAnchor="end" className="fill-muted-foreground text-[0.6875rem]">{formatDate(last.raw, dateOptions)}</text>
          {series.length === 1 && (
            <path
              d={`M${xAt(points[0])},${yAt(Math.max(yMin, 0))} ${points.map((p) => `L${xAt(p)},${yAt(p.values[0])}`).join(" ")} L${xAt(last)},${yAt(Math.max(yMin, 0))}Z`}
              fill={SERIES[0]}
              opacity="0.1"
            />
          )}
          {series.map((measure, index) => (
            <path
              key={measure.name}
              d={points.map((p, i) => `${i === 0 ? "M" : "L"}${xAt(p)},${yAt(p.values[index])}`).join(" ")}
              fill="none"
              stroke={SERIES[index]}
              strokeWidth="2"
              strokeLinejoin="round"
              strokeLinecap="round"
            />
          ))}
          {point && <line x1={xAt(point)} x2={xAt(point)} y1={MARGIN.top} y2={MARGIN.top + innerHeight} stroke="var(--muted-foreground)" strokeWidth="1" opacity="0.5" />}
          {(point ? [point] : [last]).map((p) => series.map((measure, index) => (
            <circle key={`${measure.name}-${p.x}`} cx={xAt(p)} cy={yAt(p.values[index])} r="4" fill={SERIES[index]} stroke="var(--background)" strokeWidth="2" />
          )))}
        </svg>
        {point && (
          <div
            className="pointer-events-none absolute top-0 z-10 grid gap-0.5 rounded-md bg-popover px-2.5 py-1.5 text-xs text-popover-foreground shadow-md"
            style={xAt(point) > width / 2 ? { right: width - xAt(point) + 10 } : { left: xAt(point) + 10 }}
            role="status"
          >
            <span className="text-muted-foreground">{formatDate(point.raw, dateOptions)}</span>
            {series.map((measure, index) => (
              <span key={measure.name} className="flex items-center gap-1.5 tabular-nums">
                {series.length > 1 && <span className="size-2 rounded-full" style={{ background: SERIES[index] }} aria-hidden="true" />}
                {series.length > 1 && <span className="text-muted-foreground">{columnTitle(measure)}</span>}
                <span className="font-medium">{formatNumber(point.values[index])}</span>
              </span>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function DisplayTable({ hint }) {
  const [expanded, setExpanded] = React.useState(false);
  const keys = hint.columns.filter((column) => column.role === "key");
  const columns = keys.length === hint.columns.length ? hint.columns : hint.columns.filter((column) => column.role !== "key");
  const rows = expanded ? hint.rows : hint.rows.slice(0, TABLE_PREVIEW_ROWS);
  return (
    <div className="grid gap-1">
      <Table>
        <TableHeader>
          <TableRow>
            {columns.map((column) => <TableHead key={column.name} className={cn(column.role === "measure" && "text-right")}>{columnTitle(column)}</TableHead>)}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row, index) => (
            <TableRow key={index}>
              {columns.map((column) => <TableCell key={column.name} className={cn(column.role === "measure" && "text-right")}>{cellText(valueAt(row, column.name), column)}</TableCell>)}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {hint.rows.length > TABLE_PREVIEW_ROWS && (
        <div>
          <Button type="button" variant="ghost" size="sm" onClick={() => setExpanded((value) => !value)}>
            {expanded ? "Show fewer" : `Show all ${hint.rows.length} rows`}
          </Button>
        </div>
      )}
    </div>
  );
}

function useWidth(fallback) {
  const ref = React.useRef(null);
  const [width, setWidth] = React.useState(fallback);
  React.useLayoutEffect(() => {
    const node = ref.current;
    if (!node) {
      return undefined;
    }
    const measure = () => {
      if (node.clientWidth > 0) {
        setWidth(node.clientWidth);
      }
    };
    measure();
    if (typeof ResizeObserver === "undefined") {
      return undefined;
    }
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  return [ref, width];
}

// valueAt reads a column by its path inside a row ("owner.name").
export function valueAt(row, path) {
  return String(path || "").split(".").reduce((value, key) => (value && typeof value === "object" ? value[key] : undefined), row);
}

function numberValue(value) {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string" && value.trim() !== "" && Number.isFinite(Number(value))) {
    return Number(value);
  }
  return null;
}

function timeValue(value) {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string") {
    const parsed = Date.parse(value);
    return Number.isNaN(parsed) ? null : parsed;
  }
  return null;
}

function textValue(value) {
  if (value == null || value === "") {
    return "(none)";
  }
  return typeof value === "object" ? JSON.stringify(value) : String(value);
}

function cellText(value, column) {
  if (value == null) {
    return "";
  }
  if (column.role === "measure") {
    const number = numberValue(value);
    return number == null ? textValue(value) : formatNumber(number);
  }
  return textValue(value);
}

export function niceTicks(min, max, count = 4) {
  if (min === max) {
    max = min + 1;
  }
  const rough = (max - min) / count;
  const magnitude = 10 ** Math.floor(Math.log10(rough));
  const step = [1, 2, 2.5, 5, 10].map((factor) => factor * magnitude).find((candidate) => candidate >= rough) || rough;
  const ticks = [];
  // The last tick is the first one at or above max, so no value sits above
  // the axis.
  let tick = Math.floor(min / step) * step;
  for (;;) {
    ticks.push(Number(tick.toFixed(10)));
    if (tick >= max) {
      break;
    }
    tick += step;
  }
  return ticks;
}

function formatNumber(value, compact = false) {
  return new Intl.NumberFormat(undefined, compact ? { notation: "compact", maximumFractionDigits: 1 } : { maximumFractionDigits: 2 }).format(value);
}

function dateFormatOptions(points) {
  if (typeof points[0]?.raw === "number") {
    return null;
  }
  const first = new Date(points[0].x);
  const last = new Date(points[points.length - 1].x);
  const spansDays = points.some((point) => typeof point.raw === "string" && /T\d\d:\d\d/.test(point.raw) && !/T00:00(:00)?/.test(point.raw));
  if (spansDays && last - first < 3 * 86400000) {
    return { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" };
  }
  return first.getUTCFullYear() === last.getUTCFullYear() ? { month: "short", day: "numeric", timeZone: "UTC" } : { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" };
}

function formatDate(raw, options) {
  if (options == null) {
    return String(raw);
  }
  const parsed = Date.parse(raw);
  return Number.isNaN(parsed) ? String(raw) : new Intl.DateTimeFormat(undefined, options).format(new Date(parsed));
}

function columnTitle(column) {
  const name = String(column?.name || "").split(".").pop();
  if (column?.aggregate) {
    const field = name.startsWith(`${column.aggregate}_`) ? name.slice(column.aggregate.length + 1) : name;
    if (column.aggregate === "count") {
      return field && field !== "id" && field !== name ? `Count of ${humanize(field).toLowerCase()}` : "Count";
    }
    return `${humanize(column.aggregate)} ${humanize(field).toLowerCase()}`;
  }
  return humanize(name);
}

function humanize(value) {
  const text = String(value || "").replace(/_/g, " ").trim();
  return text ? text[0].toUpperCase() + text.slice(1) : "";
}
