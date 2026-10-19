import { fileURLToPath } from "node:url";
import {
  ChannelRegistry,
  createSlackChannel,
  createVoiceChannel,
  createWebChannel,
  type ChannelDelivery,
  type DeliveryReport,
} from "@neko/channels";
import type { InteractionEvent, IntentEvent } from "@neko/interaction";
import type { AgentEvent } from "@neko/llm";
import { toInteractionEvents } from "@neko/llm/interaction";

/** A representative OUDA chat turn, exactly as a backend emits it today. */
const agentStream: AgentEvent[] = [
  { type: "status", message: "Reading the sales data source" },
  { type: "tool_start", id: "t1", name: "graphjin_query" },
  { type: "tool_end", id: "t1" },
  {
    type: "surface",
    messages: [
      { version: "v0.9", createSurface: { surfaceId: "s1", catalogId: "urn:app:catalog:briefing:v1" } },
      {
        version: "v0.9",
        updateComponents: {
          surfaceId: "s1",
          components: [
            { id: "intro", component: "Markdown", text: "Q3 is tracking ahead of plan." },
            {
              id: "card1",
              component: "BriefingCard",
              mood: "good",
              text: "Q3 revenue landed at $4.7M",
              metric: "$4.7M",
              label: "Revenue MTD",
              detail: "Up 12% MoM, driven by enterprise expansion. Q4 pipeline coverage sits at 3.1x.",
              chartType: "line",
              chartData: [{ d: "Jul", v: 3.9 }, { d: "Aug", v: 4.2 }, { d: "Sep", v: 4.7 }],
            },
          ],
        },
      },
    ],
  },
  { type: "message", role: "assistant", content: "Want me to post the Q3 summary to #exec?" },
  {
    type: "action_request_emit",
    action_request_id: "ar-501",
    kind: "send_slack_message",
    scope: "external",
    risk_level: "medium",
    intent: "Post the Q3 revenue summary to #exec",
    decision: "pending_approval",
  },
];

const slackApproveEnvelope = (decisionRef: string) => ({
  type: "block_actions",
  actions: [{ action_id: "approve", value: decisionRef }],
});

export interface DemoResult {
  interactionEvents: InteractionEvent[];
  outbound: DeliveryReport;
  inbound: { slack: IntentEvent[] };
}

/** One agent stream → the modality-free waist → every bound membrane, then back. */
export const runMultiChannelDemo = async (): Promise<DemoResult> => {
  const interactionEvents = toInteractionEvents(agentStream);

  const registry = new ChannelRegistry();
  registry.register(createWebChannel()); // built-in, always-on
  registry.register(createSlackChannel());
  registry.register(createVoiceChannel());

  registry.bind({ audience: "coo", channelPlugin: "slack", recipient: { kind: "slack", channel: "#exec" } });
  registry.bind({ audience: "coo", channelPlugin: "voice", recipient: { kind: "voice", to: "+15551111" } });

  const outbound = await registry.deliver("coo", interactionEvents);

  return {
    interactionEvents,
    outbound,
    inbound: {
      slack: registry.parseInbound("slack", slackApproveEnvelope("ar-501")),
    },
  };
};

const rule = (label: string): string => `\n${"─".repeat(4)} ${label} ${"─".repeat(Math.max(0, 64 - label.length))}`;

const routeOf = (intent: IntentEvent): string => {
  if (intent.kind === "decision") return `→ ${intent.choice}ActionRequest(orgId, "${intent.decisionRef}")`;
  if (intent.kind === "utterance") return `→ runChatTurn({ message: "${intent.text}" })`;
  if (intent.kind === "select") return `→ resolve select "${intent.optionId}" for "${intent.ref}"`;
  return `→ invoke "${intent.command}"`;
};

const printDelivery = (delivery: ChannelDelivery): void => {
  console.log(rule(`${delivery.providerLabel}  (${delivery.channelPlugin})`));
  console.log(JSON.stringify(delivery.native, null, 2));
};

const printDemo = (result: DemoResult): void => {
  console.log(rule("THE WAIST — one modality-free InteractionEvent[]"));
  console.log(JSON.stringify(result.interactionEvents, null, 2));

  console.log(rule("OUTBOUND — same events projected per profile"));
  console.log(`audience "coo" reaches: ${result.outbound.deliveries.map((d) => d.providerLabel).join(", ")}`);
  for (const delivery of result.outbound.deliveries) printDelivery(delivery);

  console.log(rule("INBOUND — native payloads normalized back to intent"));
  for (const intent of [...result.inbound.slack]) {
    console.log(`${JSON.stringify(intent)}\n  ${routeOf(intent)}`);
  }
  console.log("");
};

const main = async (): Promise<void> => {
  printDemo(await runMultiChannelDemo());
};

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  await main();
}
