import { describe, expect, it } from "vitest";
import {
  createSlackClient,
  SLACK_API_BASE,
  SlackApiError,
} from "../../src/slack/client";

function fakeFetch(
  status: number,
  body: unknown,
  capture?: { url?: string; init?: RequestInit },
): typeof fetch {
  return (async (url: unknown, init: unknown) => {
    if (capture) {
      capture.url = String(url);
      capture.init = init as RequestInit;
    }
    return new Response(typeof body === "string" ? body : JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  }) as unknown as typeof fetch;
}

describe("createSlackClient", () => {
  it("postJson sets Bearer auth + JSON body + correct URL", async () => {
    const capture: { url?: string; init?: RequestInit } = {};
    const client = createSlackClient({
      token: "xoxb-test",
      fetchImpl: fakeFetch(200, { ok: true, ts: "1.0" }, capture),
    });
    const envelope = await client.postJson("chat.postMessage", {
      channel: "C1",
      text: "hi",
    });
    expect(envelope.ok).toBe(true);
    expect(capture.url).toBe(`${SLACK_API_BASE}/chat.postMessage`);
    expect((capture.init?.headers as Record<string, string>)["Authorization"]).toBe(
      "Bearer xoxb-test",
    );
    expect(capture.init?.body).toContain('"channel":"C1"');
  });

  it("postForm encodes URLSearchParams body", async () => {
    const capture: { url?: string; init?: RequestInit } = {};
    const client = createSlackClient({
      token: "t",
      fetchImpl: fakeFetch(200, { ok: true }, capture),
    });
    await client.postForm("reactions.add", {
      channel: "C1",
      timestamp: "1.0",
      name: "thumbsup",
    });
    expect((capture.init?.headers as Record<string, string>)["Content-Type"]).toMatch(
      /application\/x-www-form-urlencoded/,
    );
    expect(capture.init?.body).toContain("channel=C1");
    expect(capture.init?.body).toContain("name=thumbsup");
  });

  it("get appends query params to the URL", async () => {
    const capture: { url?: string; init?: RequestInit } = {};
    const client = createSlackClient({
      token: "t",
      fetchImpl: fakeFetch(200, { ok: true }, capture),
    });
    await client.get("users.lookupByEmail", { email: "a@b.com" });
    expect(capture.url).toMatch(/email=a%40b\.com/);
    expect(capture.init?.method).toBe("GET");
  });

  it("throws SlackApiError on ok=false with the slack error code attached", async () => {
    const client = createSlackClient({
      token: "t",
      fetchImpl: fakeFetch(200, { ok: false, error: "channel_not_found" }),
    });
    await expect(
      client.postJson("chat.postMessage", { channel: "X", text: "x" }),
    ).rejects.toMatchObject({
      name: "SlackApiError",
      slackError: "channel_not_found",
    });
  });

  it("throws SlackApiError on non-JSON response", async () => {
    const client = createSlackClient({
      token: "t",
      fetchImpl: fakeFetch(500, "<html>error</html>"),
    });
    await expect(client.postJson("chat.postMessage", {})).rejects.toBeInstanceOf(
      SlackApiError,
    );
  });
});
