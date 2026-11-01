import type { CapabilityProfile, InteractionEvent, Projection } from "@neko/interaction";
import { summarizeBody } from "./degrade";

export type SlackBlock = Record<string, unknown>;

export interface SlackProjectionResult {
  blocks: SlackBlock[];
  text: string;
}

const section = (markdown: string, fields?: SlackBlock[]): SlackBlock => ({
  type: "section",
  text: { type: "mrkdwn", text: markdown },
  ...(fields ? { fields } : {}),
});

const approvalButtons = (decisionRef: string): SlackBlock => ({
  type: "actions",
  elements: [
    { type: "button", action_id: "approve", style: "primary", value: decisionRef, text: { type: "plain_text", text: "Approve" } },
    { type: "button", action_id: "reject", style: "danger", value: decisionRef, text: { type: "plain_text", text: "Reject" } },
  ],
});

const informBlocks = (
  event: Extract<InteractionEvent, { kind: "inform" }>,
  profile: CapabilityProfile,
): SlackBlock[] => {
  const body = summarizeBody(event.body, profile.fidelity);
  const fields = event.metric
    ? [{ type: "mrkdwn", text: `*${event.metric.label}*\n${event.metric.value}` }]
    : undefined;
  const blocks: SlackBlock[] = [section(`*${event.title}*${body ? `\n${body}` : ""}`, fields)];
  if (event.series && !profile.richMedia.charts) {
    blocks.push({ type: "context", elements: [{ type: "mrkdwn", text: "📊 Chart available in the web dashboard." }] });
  }
  return blocks;
};

const askBlocks = (
  event: Extract<InteractionEvent, { kind: "ask" }>,
  profile: CapabilityProfile,
): SlackBlock[] => {
  const blocks: SlackBlock[] = [section(event.prompt)];
  if (!profile.interaction.canApproveInline) {
    blocks.push({ type: "context", elements: [{ type: "mrkdwn", text: "Open the web dashboard to respond." }] });
    return blocks;
  }
  if (event.ask === "choice" && event.options?.length) {
    blocks.push({
      type: "actions",
      elements: event.options.map((option) => ({
        type: "button",
        action_id: `select:${option.id}`,
        value: `${event.decisionRef}:${option.id}`,
        text: { type: "plain_text", text: option.label },
      })),
    });
    return blocks;
  }
  if (event.ask === "approval") blocks.push(approvalButtons(event.decisionRef));
  return blocks;
};

/** Block Kit. Slack carries cards + buttons but not charts (per its profile). */
export const slackProjection: Projection<SlackProjectionResult> = (events, profile) => {
  const blocks: SlackBlock[] = [];
  const lines: string[] = [];
  for (const event of events) {
    if (event.kind === "converse") {
      blocks.push(section(event.text));
      lines.push(event.text);
    } else if (event.kind === "inform") {
      blocks.push(...informBlocks(event, profile));
      lines.push(event.title);
    } else if (event.kind === "ask") {
      blocks.push(...askBlocks(event, profile));
      lines.push(event.prompt);
    } else if (event.kind === "resolve") {
      const mark = event.status === "succeeded" ? "✓" : event.status === "rejected" ? "⊘" : "✗";
      blocks.push({ type: "context", elements: [{ type: "mrkdwn", text: `${mark} ${event.summary}` }] });
    } else if (event.kind === "offer") {
      blocks.push(section(`📎 ${event.label}`));
    } else if (event.kind === "highlight") {
      const fields = event.metrics.map((m) => ({
        type: "mrkdwn",
        text: `*${m.label}*\n${m.value}${m.sub ? ` _(${m.sub})_` : ""}`,
      }));
      blocks.push(section("*Key figures*", fields));
      lines.push(event.metrics.map((m) => `${m.label} ${m.value}`).join(" · "));
    }
  }
  return { blocks, text: lines.join(" · ") };
};
