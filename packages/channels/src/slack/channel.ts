import { SLACK_PROFILE, type InteractionEvent } from "@neko/interaction";
import type { ChannelEnv, ChannelRecipient, ChannelSubstrate, DeliverResult, ParsedInbound } from "../channel-adapter";
import { parseSlackInbound, recipientFromSlackPayload, senderFromSlackPayload } from "../inbound/slack";
import { slackProjection } from "../projections/slack";
import { createSlackClient, type SlackClient } from "./client";
import { verifySlackSignature } from "./verify";

/** Test seam: inject a fake SlackClient instead of the real one. */
export interface SlackDeliverOptions {
  createClient?: (token: string) => SlackClient;
}

const resolveChannel = (recipient: ChannelRecipient): string => {
  const channel = (recipient as { channel?: unknown }).channel;
  if (typeof channel === "string" && channel) return channel;
  throw new Error("recipient.channel is required (the Slack channel or DM id to deliver to)");
};

export async function deliverSlack(
  input: { recipient: ChannelRecipient; events: InteractionEvent[]; profile?: typeof SLACK_PROFILE; env: ChannelEnv },
  options: SlackDeliverOptions = {},
): Promise<DeliverResult> {
  const { blocks, text } = slackProjection(input.events, input.profile ?? SLACK_PROFILE);
  if (blocks.length === 0) return { delivered: false };
  const channel = resolveChannel(input.recipient);
  const recipient = input.recipient as { thread_ts?: unknown; user?: unknown; ephemeral?: unknown };
  const thread_ts = typeof recipient.thread_ts === "string" ? recipient.thread_ts : undefined;
  const user = typeof recipient.user === "string" ? recipient.user : undefined;
  // Slash-command replies are ephemeral (private to the invoker); DMs and
  // mention-threads post normally, threaded when a thread_ts is present.
  const ephemeral = recipient.ephemeral === true && Boolean(user);

  const token = input.env.SLACK_BOT_TOKEN;
  if (!token) {
    console.warn(`[channel-slack dry-run] channel=${channel} ${JSON.stringify({ blocks, text, thread_ts, ephemeral })}`);
    return { delivered: false, ref: `dry-run:${blocks.length}` };
  }

  const client = (options.createClient ?? ((t) => createSlackClient({ token: t })))(token);
  const envelope = await client.postJson(ephemeral ? "chat.postEphemeral" : "chat.postMessage", {
    channel,
    blocks,
    text,
    ...(thread_ts ? { thread_ts } : {}),
    ...(ephemeral ? { user } : {}),
  });
  const ts = (envelope as { ts?: unknown }).ts;
  return { delivered: true, ...(typeof ts === "string" ? { ref: ts } : {}) };
}

export function parseSlackUpdate(raw: unknown): ParsedInbound {
  const recipient = recipientFromSlackPayload(raw);
  const sender = senderFromSlackPayload(raw);
  return {
    intents: parseSlackInbound(raw),
    ...(recipient ? { recipient } : {}),
    ...(sender ? { sender } : {}),
  };
}

/** Fails closed when no signing secret is configured. */
export function verifySlackRequest(headers: Record<string, string>, body: string, env: ChannelEnv): boolean {
  const secret = env.SLACK_SIGNING_SECRET;
  if (!secret) return false;
  return verifySlackSignature(secret, headers, body);
}

/** Slack: Block Kit out; DMs, @-mentions, slash commands and button taps in over Socket Mode. */
export const SLACK_CHANNEL: ChannelSubstrate = {
  id: "slack",
  providerLabel: "Slack",
  profile: SLACK_PROFILE,
  directions: ["outbound", "inbound"],
  ingress: "socket",
  settings: [
    {
      key: "SLACK_BOT_TOKEN",
      required: true,
      secret: true,
      description: "xoxb-... bot token from your Slack app. Scopes: chat:write, im:write, im:history (DMs), app_mentions:read (mentions).",
    },
    {
      key: "SLACK_APP_TOKEN",
      required: false,
      secret: true,
      description: "xapp-... app-level token with connections:write. Enables inbound (DMs, @-mentions, slash commands) over Socket Mode. Subscribe bot events message.im + app_mention and add a /openneko slash command in the Slack app.",
    },
    {
      key: "SLACK_SIGNING_SECRET",
      required: false,
      secret: true,
      description: "Slack app signing secret. Verifies inbound webhook requests; only needed for webhook ingress, not Socket Mode.",
    },
  ],
  deliver: (input) => deliverSlack(input),
  parseInbound: (raw) => parseSlackUpdate(raw),
  verifyInbound: verifySlackRequest,
};
