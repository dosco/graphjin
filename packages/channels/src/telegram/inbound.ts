import type { IntentEvent } from "@neko/interaction";
import type { ChannelRecipient, ChannelSender } from "../channel-adapter";
import { intentFromButtonId } from "../inbound/button-id";


type Obj = Record<string, unknown>;
const asObj = (v: unknown): Obj | null =>
  typeof v === "object" && v !== null ? (v as Obj) : null;
const asStr = (v: unknown): string | null => (typeof v === "string" ? v : null);

/** The sender's chat as a delivery recipient — lets the worker auto-bind on first contact. */
export const recipientFromTelegramUpdate = (raw: unknown): ChannelRecipient | undefined => {
  const update = asObj(raw);
  if (!update) return undefined;
  const cq = asObj(update.callback_query);
  const message =
    asObj(update.message) ??
    asObj(update.edited_message) ??
    asObj(update.channel_post) ??
    (cq ? asObj(cq.message) : null);
  const chat = message ? asObj(message.chat) : null;
  const chatId = chat?.id;
  if (typeof chatId === "number" || typeof chatId === "string") {
    return { kind: "telegram", chatId };
  }
  return undefined;
};

/**
 * CH1: the sending user (`from`) of the update — message, edited message,
 * or callback query. Channel posts have no `from`; those return undefined.
 */
export const senderFromTelegramUpdate = (raw: unknown): ChannelSender | undefined => {
  const update = asObj(raw);
  if (!update) return undefined;
  const cq = asObj(update.callback_query);
  const from =
    (cq ? asObj(cq.from) : null) ??
    asObj(asObj(update.message)?.from) ??
    asObj(asObj(update.edited_message)?.from);
  const id = from?.id;
  if (typeof id !== "number" && typeof id !== "string") return undefined;
  const first = asStr(from?.first_name);
  const last = asStr(from?.last_name);
  const username = asStr(from?.username);
  const displayName =
    [first, last].filter(Boolean).join(" ") || username || undefined;
  return {
    id: String(id),
    ...(displayName ? { displayName } : {}),
  };
};

const intentFromUpdate = (update: Obj): IntentEvent | null => {
  const cq = asObj(update.callback_query);
  if (cq) {
    const data = asStr(cq.data);
    return data ? intentFromButtonId(data) : null;
  }
  const message =
    asObj(update.message) ?? asObj(update.edited_message) ?? asObj(update.channel_post);
  if (message) {
    const text = asStr(message.text);
    if (!text) return null;
    const chat = asObj(message.chat);
    const chatId = chat?.id;
    const threadRef =
      typeof chatId === "number" || typeof chatId === "string" ? String(chatId) : undefined;
    if (text.startsWith("/")) {
      const segs = text.slice(1).split(/\s+/);
      const cmd = segs[0];
      if (cmd) {
        const rest = segs.slice(1).join(" ");
        return { kind: "invoke", command: cmd, ...(rest ? { args: { text: rest } } : {}) };
      }
    }
    return { kind: "utterance", text, ...(threadRef ? { threadRef } : {}) };
  }
  return null;
};

/**
 * Telegram Update(s) → IntentEvent[]. Accepts a single webhook Update, a
 * getUpdates envelope ({ result: Update[] }), or a bare array of Updates.
 */
export const parseTelegramInbound = (raw: unknown): IntentEvent[] => {
  const obj = asObj(raw);
  const updates: unknown[] = Array.isArray(raw)
    ? raw
    : obj && Array.isArray(obj.result)
      ? (obj.result as unknown[])
      : raw != null
        ? [raw]
        : [];
  const intents: IntentEvent[] = [];
  for (const u of updates) {
    const update = asObj(u);
    const intent = update ? intentFromUpdate(update) : null;
    if (intent) intents.push(intent);
  }
  return intents;
};
