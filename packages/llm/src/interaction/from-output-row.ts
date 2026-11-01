import type {
  InteractionEvent,
  Mood,
  SeriesKind,
  SeriesPoint,
  SurfaceMessage,
} from "@neko/interaction";

/** The projection-relevant subset of a persisted workflow_output / observation row. */
export interface OutputRow {
  id: string;
  mood?: Mood;
  title: string;
  body: string;
  metric?: { label: string; value: string };
  series?: { kind: SeriesKind; points: SeriesPoint[] };
  observedAt?: string;
  surfaces?: SurfaceMessage[];
}

/**
 * Generalizes today's /api/briefing row→A2UI projector to row→InteractionEvent,
 * so the OUDA loop's persisted outputs reach any channel, not just the web.
 */
export const outputRowToInteractionEvent = (row: OutputRow): InteractionEvent => ({
  kind: "inform",
  id: row.id,
  mood: row.mood ?? "watch",
  title: row.title,
  body: row.body,
  ...(row.metric ? { metric: row.metric } : {}),
  ...(row.series ? { series: row.series } : {}),
  ...(row.observedAt ? { freshness: { observedAt: row.observedAt } } : {}),
  ...(row.surfaces ? { enrichment: { surfaces: row.surfaces } } : {}),
});
