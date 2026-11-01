import { randomUUID } from "node:crypto";
import type { CapabilityProfile, InteractionEvent } from "@neko/interaction";
import type { ChannelEnv, ChannelRecipient, ChannelSubstrate, DeliverResult, ParsedInbound } from "../channel-adapter";
import { projectEvent } from "./projection";
import { signBody, signatureIsValid } from "./security";

/**
 * The bidirectional channel between OpenNeko and an installed visual demo
 * pack. Deliveries go to the pack's bridge as a signed, versioned HUD
 * envelope; signed operator decisions and utterances come back as intents.
 * Domain-neutral: the selected pack owns the map, data and terminology.
 */
export const HUD_PROFILE: CapabilityProfile = {
  modalities: ["text", "visual"],
  richMedia: { markdown: true, cards: true, charts: true, images: true, interactiveControls: true },
  interaction: { turnTaking: "async", canApproveInline: true, quickReplies: true },
  constraints: { latencyClass: "interactive", attentionModel: "push" },
  fidelity: "full",
};

/** Every demo pack exposes its bridge under this alias on the shared Docker network. */
const DEFAULT_CHANNEL_URL = "http://demo-channel:443";

export interface HudDeliverOptions {
  fetch?: typeof fetch;
  now?: () => Date;
  randomId?: () => string;
  channelUrl?: string;
}

function requiredSecret(env: ChannelEnv): string {
  const value = env.HUD_CHANNEL_SECRET?.trim();
  if (!value) throw new Error("HUD_CHANNEL_SECRET is required");
  return value;
}

const workspace = (env: ChannelEnv): string => env.HUD_CHANNEL_WORKSPACE?.trim() || "demo";

export async function deliverHud(
  input: { recipient: ChannelRecipient; events: InteractionEvent[]; env: ChannelEnv },
  options: HudDeliverOptions = {},
): Promise<DeliverResult> {
  if (input.events.length === 0) return { delivered: false };
  const deliveryId = (options.randomId || randomUUID)();
  const envelope = {
    schema: "openneko.hud.delivery.v1",
    deliveryId,
    deliveredAt: (options.now || (() => new Date()))().toISOString(),
    workspace: workspace(input.env),
    recipient: input.recipient,
    projections: input.events.map((event) => projectEvent(event, input.recipient)),
  };
  const body = JSON.stringify(envelope);
  const response = await (options.fetch || fetch)(`${options.channelUrl || DEFAULT_CHANNEL_URL}/api/deliver`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      "x-openneko-hud-delivery-id": deliveryId,
      "x-openneko-hud-signature": `sha256=${signBody(body, requiredSecret(input.env))}`,
    },
    body,
    signal: AbortSignal.timeout(15_000),
  });
  if (!response.ok) {
    const detail = await response.text();
    throw new Error(`HUD bridge returned ${response.status}: ${detail.slice(0, 240)}`);
  }
  return { delivered: true, ref: deliveryId };
}

const record = (value: unknown): Record<string, unknown> =>
  value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};

const string = (value: unknown): string => (typeof value === "string" ? value : "");

export function parseHudUpdate(raw: unknown, env: ChannelEnv): ParsedInbound {
  const body = record(raw);
  const kind = string(body.kind);
  const pack = workspace(env);
  const recipient = { kind: "hud", workspace: pack };
  const sender = {
    id: string(body.operatorId) || `${pack}-operator`,
    ...(string(body.operatorName) ? { displayName: string(body.operatorName) } : {}),
    workspaceId: pack,
  };
  if (kind === "decision") {
    const decisionRef = string(body.decisionRef);
    const choice = string(body.choice);
    if (!decisionRef) throw new Error("decisionRef is required");
    if (choice !== "approve" && choice !== "reject") throw new Error("choice must be approve or reject");
    return {
      intents: [{ kind: "decision", decisionRef, choice, ...(string(body.reason) ? { reason: string(body.reason) } : {}) }],
      recipient,
      sender,
    };
  }
  if (kind === "utterance") {
    const text = string(body.text).trim();
    if (!text) throw new Error("text is required");
    return { intents: [{ kind: "utterance", text }], recipient, sender };
  }
  throw new Error(`unsupported inbound event kind: ${kind || "missing"}`);
}

export function verifyHudRequest(headers: Record<string, string>, body: string, env: ChannelEnv): boolean {
  const lower = Object.fromEntries(Object.entries(headers).map(([key, value]) => [key.toLowerCase(), value]));
  const secret = env.HUD_CHANNEL_SECRET?.trim();
  if (!secret) return false;
  return signatureIsValid(lower["x-openneko-hud-signature"], body, secret);
}

export const HUD_CHANNEL: ChannelSubstrate = {
  id: "hud",
  providerLabel: "HUD surface",
  profile: HUD_PROFILE,
  directions: ["outbound", "inbound"],
  ingress: "webhook",
  settings: [
    {
      key: "HUD_CHANNEL_SECRET",
      required: true,
      secret: true,
      autogenerate: true,
      description: "HMAC secret shared with the selected demo pack's HUD bridge. OpenNeko generates it.",
    },
    {
      key: "HUD_CHANNEL_WORKSPACE",
      required: false,
      secret: false,
      description: "Stable pack identifier used for inbound sender and recipient scoping. Set to the pack that turned the channel on.",
    },
  ],
  deliver: (input) => deliverHud(input),
  parseInbound: parseHudUpdate,
  verifyInbound: verifyHudRequest,
};
