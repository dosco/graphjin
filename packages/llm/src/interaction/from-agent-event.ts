import type { InteractionEvent, Mood, SeriesKind, SeriesPoint } from "@neko/interaction";
import type { AgentEvent, AgentSurfaceMessage } from "../agent-backend";

type IdGen = () => string;

const MOODS: readonly Mood[] = ["good", "watch", "act"];
const SERIES_KINDS: readonly SeriesKind[] = ["kpi", "line", "bar", "area", "donut"];

const asObj = (value: unknown): Record<string, unknown> | null =>
  typeof value === "object" && value !== null ? (value as Record<string, unknown>) : null;

const componentsOf = (messages: AgentSurfaceMessage[]): Record<string, unknown>[] => {
  const components: Record<string, unknown>[] = [];
  for (const message of messages) {
    const update = asObj(message.updateComponents);
    if (update && Array.isArray(update.components)) {
      for (const component of update.components) {
        const obj = asObj(component);
        if (obj) components.push(obj);
      }
    }
  }
  return components;
};

const str = (value: unknown, fallback = ""): string => (typeof value === "string" ? value : fallback);

const seriesFrom = (card: Record<string, unknown>): { kind: SeriesKind; points: SeriesPoint[] } | undefined => {
  if (!Array.isArray(card.chartData) || card.chartData.length === 0) return undefined;
  const kind = SERIES_KINDS.find((k) => k === card.chartType) ?? "kpi";
  const points = card.chartData.flatMap((point) => {
    const obj = asObj(point);
    return obj && typeof obj.v === "number" ? [{ d: str(obj.d), v: obj.v, ...(typeof obj.t === "number" ? { t: obj.t } : {}) }] : [];
  });
  return { kind, points };
};

/** Best-effort core extracted from an A2UI surface, with the surface kept as additive enrichment. */
const informFromSurface = (messages: AgentSurfaceMessage[], id: string): InteractionEvent => {
  const components = componentsOf(messages);
  const card = components.find((c) => c.component === "BriefingCard");
  const markdown = components.find((c) => c.component === "Markdown");
  const mood = MOODS.find((m) => m === card?.mood) ?? "watch";
  const metricValue = str(card?.metric);
  return {
    kind: "inform",
    id,
    mood,
    title: str(card?.text) || str(markdown?.text, "Update"),
    body: str(card?.detail) || str(markdown?.text),
    ...(card && metricValue ? { metric: { label: str(card.label), value: metricValue } } : {}),
    ...(card ? { series: seriesFrom(card) } : {}),
    enrichment: { surfaces: messages },
  };
};

const mapOne = (event: AgentEvent, gen: IdGen): InteractionEvent[] => {
  switch (event.type) {
    case "message":
      return event.role === "assistant" ? [{ kind: "converse", id: gen(), role: "assistant", text: event.content }] : [];
    case "tool_start":
      return [{ kind: "progress", id: event.id, label: event.name, phase: "start" }];
    case "tool_end":
      return [{ kind: "progress", id: event.id, label: event.id, phase: "end" }];
    case "status":
      return [{ kind: "progress", id: gen(), label: event.message, phase: "start" }];
    case "progress":
      return [{ kind: "progress", id: event.id, label: event.content, phase: "start" }];
    case "surface":
      return [informFromSurface(event.messages, gen())];
    case "artifact":
      return [{ kind: "offer", id: gen(), label: event.artifact.label, artifactRef: event.artifact.path, mime: event.artifact.mimeType ?? "application/octet-stream" }];
    case "vitals":
      return event.items.length
        ? [{ kind: "highlight", id: gen(), metrics: event.items.map((m) => ({ label: m.label, value: m.value, ...(m.sub ? { sub: m.sub } : {}) })) }]
        : [];
    case "action_request_emit":
      return event.decision === "pending_approval"
        ? [{ kind: "ask", id: event.action_request_id, ask: "approval", prompt: event.intent ?? event.summary ?? `Approve ${event.kind}?`, decisionRef: event.action_request_id, ...(isRisk(event.risk_level) ? { risk: event.risk_level } : {}) }]
        : [];
    case "action_request_result":
      return [{ kind: "resolve", id: event.action_request_id, ref: event.action_request_id, status: event.status, summary: resultSummary(event) }];
    case "needs_input": {
      const id = gen();
      const first = event.questions?.[0];
      const optionLabels = first?.options?.map((option) => option.label) ?? event.options;
      return [{
        kind: "ask",
        id,
        ask: optionLabels?.length ? "choice" : "freeform",
        prompt: first?.question ?? event.question,
        decisionRef: id,
        ...(optionLabels?.length
          ? { options: optionLabels.map((label, i) => ({ id: `opt-${i}`, label })) }
          : {}),
      }];
    }
    case "capability_denied":
      return [{
        kind: "inform",
        id: gen(),
        mood: "watch",
        title: `Network access blocked: ${event.host}`,
        body: "Live data was not retrieved. Use an approved integration or contact an administrator.",
        evidence: [{
          label: "Denied by sandbox policy",
          detail: `${event.method ?? "REQUEST"} ${event.host}${event.port ? `:${event.port}` : ""}${event.path ?? "/"}`,
        }],
      }];
    case "error":
      return [{ kind: "inform", id: gen(), mood: "act", title: "Something went wrong", body: event.message }];
    default:
      return [];
  }
};

const isRisk = (value: unknown): value is "low" | "medium" | "high" =>
  value === "low" || value === "medium" || value === "high";

const resultSummary = (event: Extract<AgentEvent, { type: "action_request_result" }>): string => {
  if (event.status === "rejected") return event.rejection_reason ?? `${event.kind} rejected`;
  if (event.status === "failed") return event.error ?? `${event.kind} failed`;
  if (event.status === "partially_applied") return `${event.kind} partly applied; review the action details`;
  if (event.status === "reconcile_required") return `${event.kind} needs reconciliation before retry`;
  return event.outcome?.externalRef ? `${event.kind} → ${event.outcome.externalRef}` : `${event.kind} done`;
};

/** A stateful mapper so id-less events (message/status/error) get stable, ordered ids. */
export const createAgentEventMapper = (): ((event: AgentEvent) => InteractionEvent[]) => {
  let n = 0;
  const gen: IdGen = () => `ie-${++n}`;
  return (event) => mapOne(event, gen);
};

export const toInteractionEvents = (events: readonly AgentEvent[]): InteractionEvent[] => {
  const map = createAgentEventMapper();
  return events.flatMap((event) => map(event));
};
