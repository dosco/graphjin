import { describe, it, expect } from "vitest";
import { TELEGRAM_PROFILE, type InteractionEvent } from "@neko/interaction";
import { TELEGRAM_CHANNEL, deliverTelegram, pollTelegram, type TelegramClient } from "../../src/index.js";

describe("Telegram channel", () => {
  it("declares the channel contract", () => {
    expect(TELEGRAM_CHANNEL.id).toBe("telegram");
    expect(TELEGRAM_CHANNEL.providerLabel).toBe("Telegram");
    expect(TELEGRAM_CHANNEL.directions).toEqual(["outbound", "inbound"]);
    expect(TELEGRAM_CHANNEL.profile).toEqual(TELEGRAM_PROFILE);
    expect(TELEGRAM_CHANNEL.settings.find((s) => s.key === "TELEGRAM_BOT_TOKEN")?.required).toBe(true);
  });

  it("deliver dry-runs without a token and reports not delivered", async () => {
    const events: InteractionEvent[] = [{ kind: "inform", id: "i1", mood: "good", title: "All clear", body: "Nothing to do." }];
    const result = await TELEGRAM_CHANNEL.deliver({ recipient: { kind: "telegram", chatId: 555 }, events, env: {} });
    expect(result.delivered).toBe(false);
    expect(result.ref).toMatch(/^dry-run:/);
  });

  it("deliver sends every projected message and reports the last message id", async () => {
    const calls: Array<{ method: string; body: Record<string, unknown> }> = [];
    const client = {
      call: async (method: string, body: Record<string, unknown>) => {
        calls.push({ method, body });
        return { message_id: 41 };
      },
    } as unknown as TelegramClient;
    const result = await deliverTelegram(
      {
        recipient: { kind: "telegram", chatId: 9 },
        events: [{ kind: "converse", id: "c1", role: "assistant", text: "**Done.**" }],
        env: { TELEGRAM_BOT_TOKEN: "t" },
      },
      { createClient: () => client },
    );
    expect(result).toEqual({ delivered: true, ref: "41" });
    expect(calls[0]).toMatchObject({ method: "sendMessage", body: { chat_id: 9, parse_mode: "HTML" } });
  });

  it("parseInbound normalizes a button tap to a decision", () => {
    const result = TELEGRAM_CHANNEL.parseInbound({ callback_query: { data: "approve:dr-7" } }, {});
    expect(result.intents).toEqual([{ kind: "decision", decisionRef: "dr-7", choice: "approve" }]);
  });

  it("parseInbound surfaces the sender recipient (for auto-bind)", () => {
    const result = TELEGRAM_CHANNEL.parseInbound({ message: { text: "hi", chat: { id: 8102762294 } } }, {});
    expect(result.recipient).toEqual({ kind: "telegram", chatId: 8102762294 });
  });

  it("verifyInbound matches the configured secret and rejects mismatch / missing", () => {
    const env = { TELEGRAM_WEBHOOK_SECRET: "s3cret" };
    const verify = TELEGRAM_CHANNEL.verifyInbound!;
    expect(verify({ "X-Telegram-Bot-Api-Secret-Token": "s3cret" }, "{}", env)).toBe(true);
    expect(verify({ "x-telegram-bot-api-secret-token": "nope" }, "{}", env)).toBe(false);
    expect(verify({}, "{}", env)).toBe(false);
    expect(verify({ "x-telegram-bot-api-secret-token": "s3cret" }, "{}", {})).toBe(false);
  });

  it("pollInbound returns an empty batch without a token", async () => {
    expect((await TELEGRAM_CHANNEL.pollInbound!(undefined, {})).updates).toEqual([]);
  });

  it("pollInbound fetches getUpdates, splits the batch, and advances the cursor", async () => {
    const calls: Array<{ method: string; body: Record<string, unknown> }> = [];
    const fakeClient = {
      call: async (method: string, body: Record<string, unknown>) => {
        calls.push({ method, body });
        return [
          { update_id: 10, message: { text: "hi", chat: { id: 9 } } },
          { update_id: 11, callback_query: { data: "approve:x", message: { chat: { id: 9 } } } },
        ];
      },
    } as unknown as TelegramClient;
    const res = await pollTelegram("10", { TELEGRAM_BOT_TOKEN: "t" }, { createClient: () => fakeClient });
    expect(calls[0]?.method).toBe("getUpdates");
    expect(calls[0]?.body).toMatchObject({ timeout: 0, offset: 10 });
    expect(res.updates).toHaveLength(2);
    expect(res.cursor).toBe("12");
  });
});
