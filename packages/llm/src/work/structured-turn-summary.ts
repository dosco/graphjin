import type { AgentSurfaceMessage } from "../agent-backend";
import type { VitalsPayload } from "../workflows/fence-schemas";

const MAX_CONTEXT_CHARS = 6_000;

function surfaceFacts(messages: readonly AgentSurfaceMessage[]): string[] {
  const facts: string[] = [];
  const seen = new Set<string>();
  const add = (value: unknown) => {
    if (typeof value !== "string") return;
    const text = value.trim();
    if (!text || seen.has(text)) return;
    seen.add(text);
    facts.push(text);
  };
  const resolveData = (value: unknown, dataModel: unknown): unknown => {
    if (!value || typeof value !== "object" || Array.isArray(value)) return value;
    const path = (value as Record<string, unknown>).path;
    if (typeof path !== "string" || !path.startsWith("/")) return value;
    return path.slice(1).split("/").reduce<unknown>((node, part) => {
      if (!node || typeof node !== "object") return undefined;
      return (node as Record<string, unknown>)[part.replace(/~1/g, "/").replace(/~0/g, "~")];
    }, dataModel);
  };
  const visit = (node: unknown, dataModel?: unknown): void => {
    if (Array.isArray(node)) {
      for (const item of node) visit(item, dataModel);
      return;
    }
    if (!node || typeof node !== "object") return;
    const value = node as Record<string, unknown>;
    if (value.createSurface && typeof value.createSurface === "object") {
      const surface = value.createSurface as Record<string, unknown>;
      visit(surface.components, surface.dataModel);
      return;
    }
    const component = value.component;
    if (typeof component === "string") {
      for (const key of ["title", "label", "metric", "text", "detail", "subtitle"] as const) add(value[key]);
      if (component === "KeyFigures" && Array.isArray(value.items)) {
        for (const item of value.items.slice(0, 20)) {
          if (!item || typeof item !== "object" || Array.isArray(item)) continue;
          const figure = item as Record<string, unknown>;
          if (typeof figure.label !== "string") continue;
          if (typeof figure.value !== "string" && typeof figure.value !== "number") continue;
          const qualifiers = [figure.sub, figure.basis, figure.asOf, figure.source]
            .filter((part): part is string => typeof part === "string" && Boolean(part.trim()));
          add(`${figure.label}: ${figure.value}${qualifiers.length ? ` (${qualifiers.join("; ")})` : ""}`);
        }
      }
      const rows = resolveData(value.rows, dataModel);
      if (component === "Table" && Array.isArray(rows)) {
        const columns = Array.isArray(value.columns) ? value.columns : [];
        const keys = columns
          .map(column => column && typeof column === "object" ? (column as Record<string, unknown>).key : null)
          .filter((key): key is string => typeof key === "string");
        for (const row of rows.slice(0, 20)) {
          if (!row || typeof row !== "object" || Array.isArray(row)) continue;
          const cells = (keys.length ? keys : Object.keys(row)).slice(0, 12)
            .map(key => {
              const cell = (row as Record<string, unknown>)[key];
              return typeof cell === "string" || typeof cell === "number"
                ? `${key}: ${cell}` : null;
            })
            .filter(Boolean);
          if (cells.length) add(cells.join(", "));
        }
      }
      const chartData = resolveData(value.data, dataModel);
      if (component === "Chart" && Array.isArray(chartData)) {
        const label = typeof value.valueLabel === "string" ? value.valueLabel.trim() : "value";
        for (const point of chartData.slice(0, 60)) {
          if (!point || typeof point !== "object" || Array.isArray(point)) continue;
          const { d, v } = point as Record<string, unknown>;
          if (typeof d === "string" && typeof v === "number" && Number.isFinite(v)) {
            add(`${d}: ${v} ${label}`);
          }
        }
      }
    }
    for (const child of Object.values(value)) visit(child, dataModel);
  };
  for (const message of messages) visit(message);
  return facts;
}

/** A display-safe transcript anchor when the agent returned no final text. */
export function structuredTurnSummary(
  vitals: VitalsPayload | null,
  surfaces: readonly AgentSurfaceMessage[] = [],
  notes: readonly string[] = [],
): string {
  const facts = vitals?.vitals.map(({ label, value, sub, basis, asOf, source }) => {
    const qualifiers = [sub, basis, asOf && `as of ${asOf}`, source && `source: ${source}`]
      .filter(Boolean)
      .join("; ");
    return `- ${label}: ${value}${qualifiers ? ` (${qualifiers})` : ""}`;
  }) ?? [];
  const content = [...new Set(notes.map(note => note.trim()).filter(Boolean)), ...surfaceFacts(surfaces)];
  if (!content.length && !facts.length) return "";
  const summary = [
    "Structured result from my previous turn:",
    ...content.map(value => `- ${value}`),
    ...(facts.length ? ["Headline figures:", ...facts] : []),
  ].join("\n");
  return summary.length <= MAX_CONTEXT_CHARS
    ? summary
    : `${summary.slice(0, MAX_CONTEXT_CHARS).trimEnd()}\n[Structured result truncated]`;
}
