import type { CapabilityProfile, SurfaceMessage } from "@neko/interaction";
import { escapeHtml, inlineBlock, mdToTelegramHtml } from "./markdown";

/**
 * A2UI surface → Telegram HTML. The agent's rich answer (Answer/Briefing root
 * with Markdown, MetricCard, Table, Callout, Section, Choice, Divider) rides on
 * `inform.enrichment.surfaces`; this renders as much of it as Telegram can
 * carry. Telegram has no card/column layout, so the tree flattens to one
 * formatted message: tables become aligned monospace blocks, callouts mood
 * blockquotes, sections bold-headed groups, choices a suggested-next list
 * (Telegram callback_data can't carry a full follow-up prompt).
 */

type Comp = { id: string; component: string; [key: string]: unknown };

export interface SurfaceRender {
  html: string;
  /** Choice option labels — surfaced as suggested follow-ups, not buttons. */
  followups: string[];
}

const MOOD_ICON: Record<string, string> = { good: "✅", watch: "👀", act: "🚨" };
const str = (v: unknown): string => (typeof v === "string" ? v : v == null ? "" : String(v));

// Flatten createSurface/updateComponents into an ordered id→component map,
// hoisting nested child objects and normalizing `children` to id strings
// (mirrors the web surface engine).
const collect = (messages: SurfaceMessage[]): Map<string, Comp> => {
  const map = new Map<string, Comp>();
  const add = (input: Comp) => {
    let comp = input;
    const raw = (comp as { children?: unknown }).children;
    if (Array.isArray(raw)) {
      const ids: string[] = [];
      for (const child of raw) {
        if (typeof child === "string") ids.push(child);
        else if (child && typeof child === "object" && "id" in child) {
          const childComp = child as Comp;
          ids.push(childComp.id);
          add(childComp);
        }
      }
      comp = { ...comp, children: ids };
    }
    if (comp.id) map.set(comp.id, comp);
  };
  for (const message of messages) {
    const uc = (message as { updateComponents?: { components?: unknown } }).updateComponents;
    if (uc && Array.isArray(uc.components)) {
      for (const comp of uc.components as Comp[]) add(comp);
    }
  }
  return map;
};

const childIdsOf = (comp: Comp, map: Map<string, Comp>): string[] => {
  const declared = Array.isArray(comp.children) ? (comp.children as string[]) : [];
  if (declared.length > 0 || comp.id !== "root") return declared;
  return [...map.keys()].filter((id) => id !== comp.id); // fallback: all non-root in doc order
};

const renderTable = (comp: Comp): string => {
  const columns = (Array.isArray(comp.columns) ? comp.columns : []) as Array<Record<string, unknown>>;
  const rows = (Array.isArray(comp.rows) ? comp.rows : []) as Array<Record<string, unknown>>;
  if (!columns.length) return "";
  const headers = columns.map((c) => str(c.label));
  const aligns = columns.map((c) => str(c.align));
  const cells = rows.map((r) => columns.map((c) => str(r[str(c.key)])));
  const widths = headers.map((h, i) => Math.max(h.length, ...cells.map((row) => (row[i] ?? "").length), 1));
  const pad = (s: string, i: number) =>
    aligns[i] === "right" ? s.padStart(widths[i] ?? 0) : s.padEnd(widths[i] ?? 0);
  const fmt = (row: string[]) => row.map((c, i) => pad(c, i)).join("  ");
  const rule = widths.map((w) => "─".repeat(w)).join("  ");
  const grid = [fmt(headers), rule, ...cells.map(fmt)].join("\n");
  const caption = str(comp.caption);
  return `${caption ? `${escapeHtml(caption)}\n` : ""}<pre>${escapeHtml(grid)}</pre>`;
};

const renderMetricCard = (comp: Comp): string => {
  const icon = MOOD_ICON[str(comp.mood)] ?? "";
  const value = str(comp.metric);
  const label = str(comp.label);
  const head = `${icon ? `${icon} ` : ""}<b>${escapeHtml(value)}</b>${label ? ` — ${escapeHtml(label)}` : ""}`;
  const detail = str(comp.detail);
  return detail ? `${head}\n${inlineBlock(detail)}` : head;
};

const renderCallout = (comp: Comp): string => {
  const icon = MOOD_ICON[str(comp.mood)] ?? "";
  const title = str(comp.title);
  const head = `${icon ? `${icon} ` : ""}${title ? `<b>${escapeHtml(title)}</b>` : ""}`.trim();
  const body = inlineBlock(str(comp.text)); // inline-only: safe inside <blockquote>
  const inner = [head, body].filter(Boolean).join("\n");
  return `<blockquote>${inner}</blockquote>`;
};

export const renderSurface = (
  surfaces: SurfaceMessage[],
  _profile: CapabilityProfile,
): SurfaceRender => {
  const map = collect(surfaces);
  const followups: string[] = [];

  const renderOne = (id: string): string => {
    const comp = map.get(id);
    if (!comp) return "";
    switch (comp.component) {
      case "Answer":
      case "Briefing": {
        const eyebrow = str(comp.eyebrow);
        const title = str(comp.title) || str(comp.greeting);
        const subtitle = str(comp.subtitle);
        const header = [
          eyebrow ? `<i>${escapeHtml(eyebrow)}</i>` : "",
          title ? `<b>${escapeHtml(title)}</b>` : "",
          subtitle ? escapeHtml(subtitle) : "",
        ]
          .filter(Boolean)
          .join("\n");
        const body = renderMany(childIdsOf(comp, map));
        return [header, body].filter(Boolean).join("\n\n");
      }
      case "Markdown":
        return mdToTelegramHtml(str(comp.text));
      case "MetricCard":
      case "BriefingCard":
        return renderMetricCard(comp);
      case "Table":
        return renderTable(comp);
      case "Callout":
        return renderCallout(comp);
      case "Section": {
        const title = str(comp.title);
        const body = renderMany(childIdsOf(comp, map));
        return [title ? `<b>${escapeHtml(title)}</b>` : "", body].filter(Boolean).join("\n");
      }
      case "Confirmation": {
        const label = str(comp.label);
        const title = str(comp.title);
        const body = renderMany(childIdsOf(comp, map));
        return [
          `✅ ${label ? `<b>${escapeHtml(label)}</b>` : ""}`.trim(),
          title ? escapeHtml(title) : "",
          body,
        ]
          .filter(Boolean)
          .join("\n");
      }
      case "Choice": {
        const options = Array.isArray(comp.options) ? (comp.options as Array<Record<string, unknown>>) : [];
        for (const opt of options) {
          const label = str(opt.label) || str(opt.prompt);
          if (label) followups.push(label);
        }
        return "";
      }
      case "Divider": {
        const label = str(comp.label);
        return label ? `— ${escapeHtml(label)} —` : "———";
      }
      default:
        return "";
    }
  };

  const renderMany = (ids: string[]): string =>
    ids.map(renderOne).filter(Boolean).join("\n\n");

  const root = map.get("root");
  const html = root ? renderOne("root") : renderMany([...map.keys()]);
  return { html, followups };
};
