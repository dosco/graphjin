import { stripNekoFences } from "@/components/RuleChatBubble";
import type { A2UIMessage } from "@/a2ui/types";
import type { AnswerVital } from "./work-types";

export function surfaceComponents(messages: A2UIMessage[]) {
  const components: Array<Record<string, unknown>> = [];
  for (const message of messages) {
    if ("createSurface" in message && Array.isArray(message.createSurface.components)) {
      components.push(
        ...(message.createSurface.components as Array<Record<string, unknown>>),
      );
    }
    if ("updateComponents" in message) {
      components.push(
        ...(message.updateComponents.components as Array<Record<string, unknown>>),
      );
    }
  }
  const latest = new Map<string, Record<string, unknown>>();
  for (const component of components) {
    if (typeof component.id === "string") latest.set(component.id, component);
  }
  return latest;
}

export function surfaceIdOf(messages: A2UIMessage[]): string | null {
  for (const message of messages) {
    if ("createSurface" in message) return message.createSurface.surfaceId;
    if ("updateComponents" in message) return message.updateComponents.surfaceId;
    if ("updateDataModel" in message) return message.updateDataModel.surfaceId;
    if ("deleteSurface" in message) return message.deleteSurface.surfaceId;
  }
  return null;
}

export function canonicalAnswerCreateId(message: A2UIMessage): string | null {
  if (!("createSurface" in message)) return null;
  return isCanonicalAnswerSurface([message])
    ? message.createSurface.surfaceId
    : null;
}

function isCanonicalAnswerSurface(messages: A2UIMessage[]): boolean {
  const components = surfaceComponents(messages);
  const hasAnswer = [...components.values()].some(
    (component) => component.component === "Answer",
  );
  const carriesAnswer = [...components.values()].some((component) =>
    ["Markdown", "Table", "Chart", "KeyFigures", "MetricCard", "Callout"].includes(
      String(component.component),
    ),
  );
  return hasAnswer && carriesAnswer;
}

function withVitalProvenance(
  items: AnswerVital[],
  sources: string[],
  deniedNetwork: boolean,
): AnswerVital[] {
  return items.map((item) => ({
    ...item,
    basis:
      item.basis ??
      (deniedNetwork && sources.length === 0 ? "estimated" : "calculated"),
    source: item.source ?? sources[0],
  }));
}

/**
 * Existing answers used a Row of MetricCards. Replay them as the new ruled
 * KeyFigures component, and add missing per-run vitals to otherwise complete
 * Answer surfaces. Persisted protocol messages stay immutable.
 */
export function ownVitalsBySurface(
  messages: A2UIMessage[],
  vitals: AnswerVital[],
  sources: string[],
  deniedNetwork: boolean,
): A2UIMessage[] {
  if (!isCanonicalAnswerSurface(messages)) return messages;
  const components = surfaceComponents(messages);
  const figures = withVitalProvenance(vitals, sources, deniedNetwork);
  const metricCards = [...components.values()].filter(
    (component) => component.component === "MetricCard",
  );
  const fallbackFigures: AnswerVital[] = metricCards.map((card) => ({
    label: typeof card.label === "string" ? card.label : "Figure",
    value: typeof card.metric === "string" ? card.metric : "—",
    sub: typeof card.text === "string" ? card.text : undefined,
    basis: deniedNetwork ? "estimated" : "calculated",
    source: sources[0],
  }));
  const ownedFigures = figures.length > 0 ? figures : fallbackFigures;
  if (ownedFigures.length === 0) return messages;

  const existing = [...components.values()].find(
    (component) => component.component === "KeyFigures",
  );
  const metricRow = [...components.values()].find((component) => {
    if (component.component !== "Row" || !Array.isArray(component.children)) {
      return false;
    }
    return (
      component.children.length > 0 &&
      component.children.every(
        (id) => components.get(String(id))?.component === "MetricCard",
      )
    );
  });
  const answer = [...components.values()].find(
    (component) => component.component === "Answer",
  );
  const surfaceId = surfaceIdOf(messages);
  if (!surfaceId || !answer) return messages;

  if (existing || metricRow) {
    const target = existing ?? metricRow!;
    return [
      ...messages,
      {
        version: "v1.0",
        updateComponents: {
          surfaceId,
          components: [
            {
              id: String(target.id),
              component: "KeyFigures",
              items: ownedFigures,
            },
          ],
        },
      },
    ];
  }

  const children = Array.isArray(answer.children)
    ? answer.children.map(String)
    : [];
  return [
    ...messages,
    {
      version: "v1.0",
      updateComponents: {
        surfaceId,
          components: [
            {
              ...answer,
              id: String(answer.id),
              component: "Answer",
              children: [...children, "__run_key_figures"],
            },
          {
            id: "__run_key_figures",
            component: "KeyFigures",
            items: ownedFigures,
          },
        ],
      },
    },
  ];
}

export function fallbackAnswerSurface(
  runId: string,
  raw: string,
  vitals: AnswerVital[],
  sources: string[],
  deniedNetwork: boolean,
): A2UIMessage[] {
  const text = stripNekoFences(raw).trim();
  const figures = withVitalProvenance(vitals, sources, deniedNetwork);
  const children = [
    ...(text ? ["__fallback_markdown"] : []),
    ...(figures.length > 0 ? ["__fallback_figures"] : []),
  ];
  if (children.length === 0) return [];
  return [
    {
      version: "v1.0",
      createSurface: {
        surfaceId: `answer-${runId}`,
        catalogId: "urn:openneko:catalog:work:v2",
        components: [
          {
            id: "root",
            component: "Answer",
            title: "",
            children,
          },
          ...(text
            ? [
                {
                  id: "__fallback_markdown",
                  component: "Markdown",
                  text,
                },
              ]
            : []),
          ...(figures.length > 0
            ? [
                {
                  id: "__fallback_figures",
                  component: "KeyFigures",
                  items: figures,
                },
              ]
            : []),
        ],
        dataModel: {},
      },
    },
  ];
}
