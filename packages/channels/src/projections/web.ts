import type {
  Choice,
  InteractionEvent,
  Projection,
  RiskLevel,
  SurfaceMessage,
} from "@neko/interaction";

const CATALOG_ID = "urn:app:catalog:briefing:v1";

export interface WebAsk {
  decisionRef: string;
  ask: string;
  prompt: string;
  risk?: RiskLevel;
  options?: Choice[];
}

export interface WebProjectionResult {
  surfaces: SurfaceMessage[];
  pendingAsks: WebAsk[];
}

type Component = Record<string, unknown>;

const toComponent = (event: InteractionEvent): Component | null => {
  if (event.kind === "converse") {
    return { id: `c-${event.id}`, component: "Markdown", text: event.text };
  }
  if (event.kind === "inform") {
    return {
      id: `card-${event.id}`,
      component: "MetricCard",
      metricId: event.id,
      source: "chat",
      state: "ok",
      mood: event.mood,
      text: event.title,
      metric: event.metric?.value ?? "",
      label: event.metric?.label ?? "",
      detail: event.body,
      chartType: event.series?.kind ?? "kpi",
      chartData: event.series?.points ?? [],
    };
  }
  if (event.kind === "resolve") {
    const mark = event.status === "succeeded" ? "✓" : event.status === "rejected" ? "⊘" : "✗";
    return { id: `r-${event.id}`, component: "Markdown", text: `${mark} ${event.summary}` };
  }
  if (event.kind === "offer") {
    return { id: `o-${event.id}`, component: "Markdown", text: `[${event.label}](${event.artifactRef})` };
  }
  if (event.kind === "highlight") {
    const lines = event.metrics
      .map((m) => `**${m.value}** ${m.label}${m.sub ? ` — ${m.sub}` : ""}`)
      .join("  \n");
    return { id: `h-${event.id}`, component: "Markdown", text: lines };
  }
  return null;
};

/**
 * The reference web projection. Owns the A2UI catalog that used to live in the
 * agent prompt. `ask` events become web-native approval affordances, not A2UI
 * components.
 */
export const webProjection: Projection<WebProjectionResult> = (events) => {
  const components: Component[] = [];
  const pendingAsks: WebAsk[] = [];
  for (const event of events) {
    if (event.kind === "ask") {
      pendingAsks.push({
        decisionRef: event.decisionRef,
        ask: event.ask,
        prompt: event.prompt,
        risk: event.risk,
        options: event.options,
      });
      continue;
    }
    const component = toComponent(event);
    if (component) components.push(component);
  }
  const surfaces: SurfaceMessage[] = [
    { version: "v1.0", createSurface: { surfaceId: "s1", catalogId: CATALOG_ID } },
    { version: "v1.0", updateComponents: { surfaceId: "s1", components } },
  ];
  return { surfaces, pendingAsks };
};
