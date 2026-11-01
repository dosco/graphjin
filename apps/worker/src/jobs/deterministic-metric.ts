import { resolveWatcherVariables } from "@neko/llm/workflows";
import type { MetricAgentResult } from "@neko/llm";

type MetricDefinition = {
  title: string;
  description?: string;
  calculationNote?: string;
  chartHint?: string;
  unit?: string;
  directionGood?: "up" | "down" | "neutral";
  execution: {
    mode: "saved_query";
    source: string;
    query: string;
    document: string;
    result: Record<string, unknown>;
    freshnessSeconds: number;
    variables?: Record<string, unknown>;
    runtime?: {
      storeIds?: number[];
      windowDays?: number;
      staleAfterSeconds?: number;
      agedAfterSeconds?: number;
      stockThreshold?: number;
      currency?: string;
      timezone?: string;
    };
  };
};

export type DeterministicMetricOutput = {
  result: MetricAgentResult;
  value: number | null;
  valueJson: unknown;
  baseline: number | null;
  deltaPct: number | null;
  sourceFreshnessAt: Date | null;
};

function record(value: unknown, label: string): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${label} must be an object`);
  }
  return value as Record<string, unknown>;
}

export function parseMetricDefinition(value: unknown): MetricDefinition {
  const definition = record(value, "saved-query metric definition");
  const execution = record(definition.execution, "saved-query metric execution");
  if (execution.mode !== "saved_query") throw new Error("saved-query metric execution.mode is invalid");
  for (const key of ["source", "query", "document"] as const) {
    if (typeof execution[key] !== "string" || !String(execution[key]).trim()) {
      throw new Error(`saved-query metric execution.${key} is required`);
    }
  }
  const freshness = Number(execution.freshnessSeconds);
  if (!Number.isInteger(freshness) || freshness <= 0) {
    throw new Error("saved-query metric freshnessSeconds must be a positive integer");
  }
  const result = record(execution.result, "saved-query metric result mapping");
  if (typeof result.kind !== "string") throw new Error("saved-query metric result.kind is required");
  if (typeof definition.title !== "string" || !definition.title.trim()) {
    throw new Error("saved-query metric title is required");
  }
  return definition as unknown as MetricDefinition;
}

function iso(date: Date): string {
  return date.toISOString();
}

export function buildSavedQueryVariables(
  definitionValue: unknown,
  now = new Date(),
): Record<string, unknown> {
  const definition = parseMetricDefinition(definitionValue);
  if (definition.execution.variables) return resolveWatcherVariables(definition.execution.variables, now);
  const runtime = definition.execution.runtime ?? {};
  const windowDays = positive(runtime.windowDays, 30);
  const staleAfterSeconds = positive(runtime.staleAfterSeconds, 2 * 60 * 60);
  const agedAfterSeconds = positive(runtime.agedAfterSeconds, 2 * 24 * 60 * 60);
  return {
    from: iso(new Date(now.getTime() - windowDays * 86_400_000)),
    to: iso(now),
    now: iso(now),
    staleBefore: iso(new Date(now.getTime() - staleAfterSeconds * 1_000)),
    olderThan: iso(new Date(now.getTime() - agedAfterSeconds * 1_000)),
    storeIds: runtime.storeIds?.length ? runtime.storeIds : [1],
    threshold: Number.isFinite(runtime.stockThreshold) ? runtime.stockThreshold : 5,
  };
}

function positive(value: unknown, fallback: number): number {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? number : fallback;
}

function atPath(root: unknown, path: string): unknown {
  let current = root;
  for (const segment of path.split(".")) {
    if (Array.isArray(current) && /^\d+$/.test(segment)) {
      current = current[Number(segment)];
    } else if (current && typeof current === "object" && segment in current) {
      current = (current as Record<string, unknown>)[segment];
    } else {
      return undefined;
    }
  }
  return current;
}

function numberAt(root: unknown, path: string): number {
  const value = atPath(root, path);
  const parsed = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(parsed)) throw new Error(`saved-query result path ${path} is not numeric`);
  return parsed;
}

function evaluateExpression(root: unknown, expression: unknown): number | null {
  if (typeof expression === "number") return expression;
  if (typeof expression === "string") return numberAt(root, expression);
  const node = record(expression, "saved-query result expression");
  const entries = Object.entries(node);
  if (entries.length !== 1) throw new Error("saved-query expression must contain one operator");
  const [operator, operands] = entries[0]!;
  if (!Array.isArray(operands) || operands.length < 2) {
    throw new Error(`saved-query expression ${operator} requires at least two operands`);
  }
  const values = operands.map((operand) => evaluateExpression(root, operand));
  if (values.some((value) => value === null)) return null;
  const numbers = values as number[];
  switch (operator) {
    case "add":
      return numbers.reduce((sum, value) => sum + value, 0);
    case "subtract":
      return numbers.slice(1).reduce((value, operand) => value - operand, numbers[0]!);
    case "multiply":
      return numbers.reduce((value, operand) => value * operand, 1);
    case "divide": {
      const denominator = numbers.slice(1).reduce((value, operand) => value * operand, 1);
      return denominator === 0 ? null : numbers[0]! / denominator;
    }
    default:
      throw new Error(`saved-query result expression operator ${operator} is not allowed`);
  }
}

function rowsAt(root: unknown, path: string): Record<string, unknown>[] {
  const value = atPath(root, path);
  if (!Array.isArray(value)) throw new Error(`saved-query result path ${path} is not an array`);
  return value.map((row, index) => record(row, `${path}.${index}`));
}

function median(values: number[]): number {
  if (values.length === 0) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 0
    ? (sorted[middle - 1]! + sorted[middle]!) / 2
    : sorted[middle]!;
}

function latestTimestamp(value: unknown): Date | null {
  let latest: Date | null = null;
  const visit = (candidate: unknown) => {
    if (typeof candidate === "string" && /^\d{4}-\d{2}-\d{2}[T ]/.test(candidate)) {
      const parsed = new Date(candidate);
      if (!Number.isNaN(parsed.getTime()) && (!latest || parsed > latest)) latest = parsed;
      return;
    }
    if (Array.isArray(candidate)) {
      for (const item of candidate) visit(item);
      return;
    }
    if (candidate && typeof candidate === "object") {
      for (const item of Object.values(candidate as Record<string, unknown>)) visit(item);
    }
  };
  visit(value);
  return latest;
}

function rowLabel(row: Record<string, unknown>, index: number): string {
  for (const key of ["sku", "indexer_id", "source_code", "store_id", "name", "status"]) {
    if (row[key] !== undefined && row[key] !== null) return String(row[key]);
  }
  return String(index + 1);
}

function rowValue(row: Record<string, unknown>): number {
  for (const key of ["sum_base_row_invoiced", "net_revenue", "quantity", "sum_qty_invoiced"]) {
    const value = Number(row[key]);
    if (Number.isFinite(value)) return value;
  }
  return 1;
}

function mapValue(
  mapping: Record<string, unknown>,
  data: Record<string, unknown>,
  now: Date,
): { value: number | null; valueJson: unknown; chartData: Array<{ d: string; v: number }> } {
  switch (mapping.kind) {
    case "scalar": {
      const value = mapping.expression !== undefined
        ? evaluateExpression(data, mapping.expression)
        : numberAt(data, String(mapping.path));
      return { value, valueJson: value, chartData: [{ d: "Current", v: value ?? 0 }] };
    }
    case "object": {
      const paths = Array.isArray(mapping.paths) ? mapping.paths.map(String) : [];
      const valueJson = Object.fromEntries(paths.map((path) => [path, atPath(data, path) ?? null]));
      const chartData = paths.flatMap((path) => {
        const value = Number(atPath(data, path));
        return Number.isFinite(value) ? [{ d: path.split(".").at(-2) ?? path, v: value }] : [];
      });
      return {
        value: chartData.reduce((sum, item) => sum + item.v, 0),
        valueJson,
        chartData: chartData.length ? chartData : [{ d: "Current", v: 0 }],
      };
    }
    case "timestamps": {
      const paths = Array.isArray(mapping.paths) ? mapping.paths.map(String) : [];
      const ages = paths.map((path) => {
        const date = new Date(String(atPath(data, path) ?? ""));
        return {
          d: path.split(".").at(-1) ?? path,
          v: Number.isNaN(date.getTime()) ? null : Math.max(0, (now.getTime() - date.getTime()) / 1_000),
        };
      });
      const valid = ages.filter((item): item is { d: string; v: number } => item.v !== null);
      return {
        value: valid.length ? Math.max(...valid.map((item) => item.v)) : null,
        valueJson: Object.fromEntries(paths.map((path) => [path, atPath(data, path) ?? null])),
        chartData: valid.length ? valid : [{ d: "Not yet available", v: 0 }],
      };
    }
    case "timestamp_age_summary": {
      const rows = rowsAt(data, String(mapping.rowsPath));
      const path = String(mapping.timestampPath);
      const ages = rows.map((row) => {
        const date = new Date(String(atPath(row, path) ?? ""));
        if (Number.isNaN(date.getTime())) throw new Error(`saved-query timestamp ${path} is invalid`);
        return Math.max(0, (now.getTime() - date.getTime()) / 1_000);
      });
      const med = median(ages);
      const max = ages.length ? Math.max(...ages) : 0;
      return { value: med, valueJson: { median: med, max, count: rows.length }, chartData: [{ d: "Median", v: med }, { d: "Oldest", v: max }] };
    }
    case "rows": {
      const rows = rowsAt(data, String(mapping.path));
      return { value: rows.length, valueJson: rows, chartData: rows.slice(0, 20).map((row, index) => ({ d: rowLabel(row, index), v: rowValue(row) })).concat(rows.length ? [] : [{ d: "None", v: 0 }]) };
    }
    case "keyed_subtract": {
      const left = record(mapping.left, "keyed_subtract.left");
      const right = record(mapping.right, "keyed_subtract.right");
      const totals = new Map<string, number>();
      for (const row of rowsAt(data, String(left.rowsPath))) {
        const key = String(atPath(row, String(left.keyPath)));
        totals.set(key, (totals.get(key) ?? 0) + numberAt(row, String(left.valuePath)));
      }
      for (const row of rowsAt(data, String(right.rowsPath))) {
        const key = String(atPath(row, String(right.keyPath)));
        totals.set(key, (totals.get(key) ?? 0) - numberAt(row, String(right.valuePath)));
      }
      const chartData = [...totals].sort(([a], [b]) => a.localeCompare(b)).map(([d, v]) => ({ d, v }));
      return { value: chartData.reduce((sum, item) => sum + item.v, 0), valueJson: Object.fromEntries(totals), chartData: chartData.length ? chartData : [{ d: "None", v: 0 }] };
    }
    default:
      throw new Error(`saved-query result kind ${String(mapping.kind)} is not supported`);
  }
}

function chartType(hint: string | undefined, points: number): MetricAgentResult["chartType"] {
  if (hint === "bar" || hint === "table" || hint === "distribution" || points > 1) return "bar";
  return "kpi";
}

function displayValue(value: number | null, unit: string | undefined, currency: string): string {
  if (value === null) return "Not yet available";
  const safe = value;
  if (unit === "currency") {
    return new Intl.NumberFormat("en-US", { style: "currency", currency, maximumFractionDigits: 2 }).format(safe);
  }
  if (unit === "percent") return `${new Intl.NumberFormat("en-US", { maximumFractionDigits: 2 }).format(safe * 100)}%`;
  if (unit === "duration_seconds") {
    if (safe >= 86_400) return `${(safe / 86_400).toFixed(1)}d`;
    if (safe >= 3_600) return `${(safe / 3_600).toFixed(1)}h`;
    return `${Math.round(safe)}s`;
  }
  return new Intl.NumberFormat("en-US", { maximumFractionDigits: 2 }).format(safe);
}

function mood(value: number | null, baseline: number | null, direction: string | undefined): MetricAgentResult["mood"] {
  if (value === null || baseline === null || direction === "neutral") return "watch";
  if (value === baseline) return "watch";
  if (direction === "up") return value > baseline ? "good" : "bad";
  if (direction === "down") return value < baseline ? "good" : "bad";
  return "watch";
}

export function mapSavedQueryMetric(input: {
  definition: unknown;
  data: unknown;
  baseline?: number | null;
  now?: Date;
}): DeterministicMetricOutput {
  const definition = parseMetricDefinition(input.definition);
  const data = record(input.data, "saved-query response data");
  const now = input.now ?? new Date();
  const mapped = mapValue(definition.execution.result, data, now);
  const baseline = input.baseline ?? null;
  const deltaPct = mapped.value !== null && baseline !== null && baseline !== 0
    ? ((mapped.value - baseline) / Math.abs(baseline)) * 100
    : null;
  const currency = definition.execution.runtime?.currency ?? "USD";
  const type = chartType(definition.chartHint, mapped.chartData.length);
  const chartData = mapped.chartData.map((point) => ({
    ...point,
    ...(type === "kpi" ? { t: baseline ?? mapped.value ?? 0 } : {}),
  }));
  const headline = displayValue(mapped.value, definition.unit, currency);
  return {
    value: mapped.value,
    valueJson: mapped.valueJson,
    baseline,
    deltaPct,
    sourceFreshnessAt: latestTimestamp(data),
    result: {
      reasoning: "Computed deterministically from a reviewed saved query; no language model selected or transformed the value.",
      headlineMetric: headline,
      headlineLabel: definition.title,
      insightText: baseline === null
        ? `${definition.title} is ${headline}; a comparison will appear after the next refresh.`
        : `${definition.title} is ${headline}, ${deltaPct === null ? "with no comparable percentage change" : `${Math.abs(deltaPct).toFixed(1)}% ${deltaPct >= 0 ? "above" : "below"} the prior refresh`}.`,
      detailText: definition.calculationNote ?? definition.description ?? definition.title,
      mood: mood(mapped.value, baseline, definition.directionGood),
      chartType: type,
      chartData,
      baselineValue: baseline,
      timeWindow: {
        grain: "month",
        start: new Date(now.getTime() - positive(definition.execution.runtime?.windowDays, 30) * 86_400_000).toISOString().slice(0, 10),
        end: now.toISOString().slice(0, 10),
        label: `Last ${positive(definition.execution.runtime?.windowDays, 30)} days`,
      },
    },
  };
}
