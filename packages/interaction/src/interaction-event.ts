export type Mood = "good" | "watch" | "act";

export type RiskLevel = "low" | "medium" | "high";

export type AskKind = "approval" | "choice" | "freeform";

export type ResolveStatus = "succeeded" | "failed" | "rejected" | "partially_applied" | "reconcile_required";

export type SeriesKind = "kpi" | "line" | "bar" | "area" | "donut";

export interface Metric {
  label: string;
  value: string;
  /** Optional one-line comparison or context, e.g. "down from 53%". */
  sub?: string;
}

export interface SeriesPoint {
  d: string;
  v: number;
  t?: number;
}

export interface Series {
  kind: SeriesKind;
  points: SeriesPoint[];
}

export interface Evidence {
  label: string;
  detail?: string;
  ref?: string;
}

export interface Freshness {
  observedAt: string;
}

export interface Choice {
  id: string;
  label: string;
}

/**
 * A surface message blob (A2UI v0.9 or any future structured payload).
 * Structural by design — the waist never imports a renderer's catalog.
 */
export type SurfaceMessage = { version: string; [key: string]: unknown };

/**
 * Optional, additive. A rich visual channel reads it; a thin or eyes-free
 * channel ignores it and is still guaranteed a complete experience from the
 * modality-free core of the event. Enrichment is never the payload.
 */
export interface RichEnrichment {
  surfaces?: SurfaceMessage[];
  imageUrl?: string;
}

export type InteractionEvent =
  | { kind: "converse"; id: string; role: "assistant"; text: string; enrichment?: RichEnrichment }
  | { kind: "progress"; id: string; label: string; phase: "start" | "end" }
  | {
      kind: "inform";
      id: string;
      mood: Mood;
      title: string;
      body: string;
      evidence?: Evidence[];
      metric?: Metric;
      series?: Series;
      freshness?: Freshness;
      enrichment?: RichEnrichment;
    }
  | {
      kind: "ask";
      id: string;
      ask: AskKind;
      prompt: string;
      decisionRef: string;
      options?: Choice[];
      risk?: RiskLevel;
    }
  | { kind: "resolve"; id: string; ref: string; status: ResolveStatus; summary: string }
  | { kind: "offer"; id: string; label: string; artifactRef: string; mime: string }
  // The headline figures that carry an answer — modality-free content. Each
  // channel renders them its own way: the web as metric tiles, an eyes-free
  // channel by reading them aloud, a thin channel as plain lines.
  | { kind: "highlight"; id: string; metrics: Metric[] };

export type InteractionEventKind = InteractionEvent["kind"];
