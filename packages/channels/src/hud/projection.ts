import { randomUUID } from "node:crypto";
import type { InteractionEvent } from "@neko/interaction";
import type { ChannelRecipient } from "../channel-adapter";

export type HudAnchor = {
  kind: string;
  id?: string;
  longitude?: number;
  latitude?: number;
};

export type HudProjection = {
  id: string;
  kind: InteractionEvent["kind"];
  type: "message" | "progress" | "finding" | "figures" | "decision" | "resolution" | "artifact";
  anchor: HudAnchor;
  title: string;
  body?: string;
  [key: string]: unknown;
};

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function string(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function coordinate(value: unknown): number | undefined {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

export function inferAnchor(event: InteractionEvent, recipient: ChannelRecipient): HudAnchor {
  const source = record(event);
  const enrichment = record(source.enrichment);
  const candidate = record(source.spatial ?? enrichment.spatial);
  const longitude = coordinate(candidate.longitude);
  const latitude = coordinate(candidate.latitude);
  if (longitude !== undefined && latitude !== undefined) {
    return {
      kind: string(candidate.kind) || "coordinate",
      ...(string(candidate.id) ? { id: string(candidate.id) } : {}),
      longitude,
      latitude,
    };
  }

  const evidence = Array.isArray(source.evidence) ? source.evidence : [];
  for (const item of evidence) {
    const ref = string(record(item).ref);
    const separator = ref.indexOf(":");
    if (separator > 0 && separator < ref.length - 1) {
      return { kind: ref.slice(0, separator), id: ref.slice(separator + 1) };
    }
  }

  const defaultAnchor = string(record(recipient).defaultAnchor);
  if (defaultAnchor) {
    const separator = defaultAnchor.indexOf(":");
    return separator > 0
      ? { kind: defaultAnchor.slice(0, separator), id: defaultAnchor.slice(separator + 1) }
      : { kind: "workspace", id: defaultAnchor };
  }
  return { kind: "workspace", id: string(record(recipient).workspace) || "demo" };
}

export function projectEvent(
  event: InteractionEvent,
  recipient: ChannelRecipient,
): HudProjection {
  const base = {
    id: event.id || randomUUID(),
    kind: event.kind,
    anchor: inferAnchor(event, recipient),
  };

  switch (event.kind) {
    case "converse":
      return { ...base, type: "message", title: "OpenNeko", body: event.text };
    case "progress":
      return {
        ...base,
        type: "progress",
        title: event.label,
        phase: event.phase,
      };
    case "ask":
      return {
        ...base,
        type: "decision",
        title: "Operator decision",
        body: event.prompt,
        ask: event.ask,
        decisionRef: event.decisionRef,
        risk: event.risk || "medium",
        options: event.options || [],
      };
    case "resolve":
      return {
        ...base,
        type: "resolution",
        title: event.status,
        body: event.summary,
        decisionRef: event.ref,
        status: event.status,
      };
    case "offer":
      return {
        ...base,
        type: "artifact",
        title: event.label,
        artifactRef: event.artifactRef,
        mime: event.mime,
      };
    case "highlight":
      return { ...base, type: "figures", title: "Key figures", metrics: event.metrics };
    case "inform":
      return {
        ...base,
        type: "finding",
        title: event.title,
        body: event.body,
        mood: event.mood,
        evidence: event.evidence || [],
        metric: event.metric,
        series: event.series,
        freshness: event.freshness,
      };
  }
}
