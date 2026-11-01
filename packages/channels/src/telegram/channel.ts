import { timingSafeEqual } from "node:crypto";
import { TELEGRAM_PROFILE, type CapabilityProfile, type InteractionEvent } from "@neko/interaction";
import type { ChannelEnv, ChannelRecipient, ChannelSubstrate, DeliverResult, ParsedInbound } from "../channel-adapter";
import { createTelegramClient, type TelegramClient } from "./client";
import { parseTelegramInbound, recipientFromTelegramUpdate, senderFromTelegramUpdate } from "./inbound";
import { projectTelegram } from "./projection";

/** Test seam: inject a fake TelegramClient instead of the real one. */
export interface TelegramOptions {
  createClient?: (token: string) => TelegramClient;
}

const clientFor = (token: string, options: TelegramOptions): TelegramClient =>
  (options.createClient ?? ((t: string) => createTelegramClient({ token: t })))(token);

const resolveChatId = (recipient: ChannelRecipient): string | number => {
  const chatId = (recipient as { chatId?: unknown }).chatId;
  if (typeof chatId === "string" || typeof chatId === "number") return chatId;
  throw new Error("recipient.chatId is required (the Telegram chat or user id to deliver to)");
};

export async function deliverTelegram(
  input: { recipient: ChannelRecipient; events: InteractionEvent[]; profile?: CapabilityProfile; env: ChannelEnv },
  options: TelegramOptions = {},
): Promise<DeliverResult> {
  const messages = projectTelegram(input.events, input.profile ?? TELEGRAM_PROFILE);
  if (messages.length === 0) return { delivered: false };

  const chatId = resolveChatId(input.recipient);
  const token = input.env.TELEGRAM_BOT_TOKEN;
  if (!token) {
    console.warn(`[channel-telegram dry-run] chat=${chatId} ${JSON.stringify(messages)}`);
    return { delivered: false, ref: `dry-run:${messages.length}` };
  }

  const client = clientFor(token, options);
  let lastId: string | undefined;
  for (const message of messages) {
    const sent = await client.call<{ message_id?: number }>("sendMessage", { chat_id: chatId, ...message });
    if (typeof sent.message_id === "number") lastId = String(sent.message_id);
  }
  return { delivered: true, ...(lastId ? { ref: lastId } : {}) };
}

export function parseTelegramUpdate(raw: unknown): ParsedInbound {
  const recipient = recipientFromTelegramUpdate(raw);
  const sender = senderFromTelegramUpdate(raw);
  return {
    intents: parseTelegramInbound(raw),
    ...(recipient ? { recipient } : {}),
    ...(sender ? { sender } : {}),
  };
}

/** Constant-time match of Telegram's secret_token header; fails closed without a configured secret. */
export function verifyTelegramRequest(headers: Record<string, string>, _body: string, env: ChannelEnv): boolean {
  const secret = env.TELEGRAM_WEBHOOK_SECRET;
  if (!secret) return false;
  const lower: Record<string, string> = {};
  for (const [k, v] of Object.entries(headers)) lower[k.toLowerCase()] = v;
  const header = lower["x-telegram-bot-api-secret-token"];
  if (!header) return false;
  const a = Buffer.from(header);
  const b = Buffer.from(secret);
  if (a.length !== b.length) return false;
  return timingSafeEqual(a, b);
}

/**
 * Pull transport: fetch the next batch via getUpdates and return the individual
 * updates. `timeout: 0` returns immediately — the worker owns the loop cadence.
 * Without a token it returns an empty batch.
 */
export async function pollTelegram(
  cursor: string | undefined,
  env: ChannelEnv,
  options: TelegramOptions = {},
): Promise<{ updates: unknown[]; cursor?: string }> {
  const token = env.TELEGRAM_BOT_TOKEN;
  if (!token) return { updates: [] };
  const offset = cursor ? Number(cursor) : undefined;
  const updates = await clientFor(token, options).call<Array<{ update_id?: number }>>("getUpdates", {
    timeout: 0,
    ...(offset !== undefined && Number.isFinite(offset) ? { offset } : {}),
  });
  const list = Array.isArray(updates) ? updates : [];
  let maxId = -1;
  for (const u of list) {
    if (typeof u.update_id === "number" && u.update_id > maxId) maxId = u.update_id;
  }
  const next = maxId >= 0 ? String(maxId + 1) : cursor;
  return { updates: list, ...(next ? { cursor: next } : {}) };
}

/** Telegram: HTML messages and inline keyboards out; messages and button taps in. */
export const TELEGRAM_CHANNEL: ChannelSubstrate = {
  id: "telegram",
  providerLabel: "Telegram",
  profile: TELEGRAM_PROFILE,
  directions: ["outbound", "inbound"],
  ingress: "webhook",
  settings: [
    {
      key: "TELEGRAM_BOT_TOKEN",
      required: true,
      secret: true,
      description: "Bot token from @BotFather (e.g. 123456789:AAH…). Without it, delivery runs dry and sends nothing.",
    },
    {
      key: "TELEGRAM_WEBHOOK_SECRET",
      required: false,
      secret: true,
      description: "The secret_token you set with setWebhook. Webhook requests must carry it. Not needed for long polling.",
    },
  ],
  deliver: (input) => deliverTelegram(input),
  parseInbound: (raw) => parseTelegramUpdate(raw),
  verifyInbound: verifyTelegramRequest,
  pollInbound: (cursor, env) => pollTelegram(cursor, env),
};
