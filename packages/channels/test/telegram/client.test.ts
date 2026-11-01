import { describe, it, expect, vi } from "vitest";
import { createTelegramClient, TelegramApiError } from "../../src/telegram/client";

const jsonResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

describe("createTelegramClient", () => {
  it("POSTs JSON to /bot<token>/<method> and returns the result", async () => {
    const fetchImpl = vi.fn(async () => jsonResponse({ ok: true, result: { message_id: 99 } }));
    const client = createTelegramClient({
      token: "T",
      fetchImpl: fetchImpl as unknown as typeof fetch,
    });
    const res = await client.call<{ message_id: number }>("sendMessage", { chat_id: 1, text: "hi" });
    expect(res.message_id).toBe(99);
    const call = fetchImpl.mock.calls[0]!;
    expect(call[0]).toBe("https://api.telegram.org/botT/sendMessage");
    const init = call[1]!;
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ chat_id: 1, text: "hi" });
  });

  it("throws TelegramApiError on an ok:false envelope", async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse({ ok: false, error_code: 400, description: "Bad Request: chat not found" }),
    );
    const client = createTelegramClient({
      token: "T",
      fetchImpl: fetchImpl as unknown as typeof fetch,
    });
    await expect(client.call("sendMessage", {})).rejects.toBeInstanceOf(TelegramApiError);
  });

  it("surfaces the underlying OS code from an opaque fetch failure", async () => {
    const failed = Object.assign(new TypeError("fetch failed"), {
      cause: Object.assign(new Error("connect ECONNREFUSED"), { code: "ECONNREFUSED" }),
    });
    const fetchImpl = vi.fn(async () => {
      throw failed;
    });
    const client = createTelegramClient({
      token: "T",
      fetchImpl: fetchImpl as unknown as typeof fetch,
    });
    await expect(client.call("getUpdates", {})).rejects.toThrow(
      /network error: fetch failed \(ECONNREFUSED\)/,
    );
  });

  it("collects every code from a Happy-Eyeballs aggregate failure", async () => {
    const failed = Object.assign(new TypeError("fetch failed"), {
      cause: new AggregateError(
        [
          Object.assign(new Error("v6"), { code: "ENETUNREACH" }),
          Object.assign(new Error("v4"), { code: "ECONNREFUSED" }),
        ],
        "all attempts failed",
      ),
    });
    const fetchImpl = vi.fn(async () => {
      throw failed;
    });
    const client = createTelegramClient({
      token: "T",
      fetchImpl: fetchImpl as unknown as typeof fetch,
    });
    await expect(client.call("getUpdates", {})).rejects.toThrow(
      /fetch failed \(ENETUNREACH, ECONNREFUSED\)/,
    );
  });
});
