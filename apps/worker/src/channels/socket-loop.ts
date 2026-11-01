// Slack Socket Mode transport, worker-side. The worker owns the persistent
// connection and hands each inbound payload to processInboundUpdate, which
// parses it with the built-in Slack channel.
// Dependency-free on purpose: Node's global WebSocket + fetch.
import { channelEnv } from "./runtime.js";
import { processInboundUpdate } from "./delivery.js";
import { pollBackoffMs } from "./poll-backoff.js";

const SLACK_API_BASE = "https://slack.com/api";
const msg = (e: unknown): string => (e instanceof Error ? e.message : String(e));
const sleep = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));

type MinimalWs = {
  send(data: string): void;
  close(): void;
  addEventListener(type: "message", cb: (ev: { data: unknown }) => void): void;
  addEventListener(type: "close" | "error", cb: () => void): void;
};
const WebSocketCtor = (globalThis as { WebSocket?: new (url: string) => MinimalWs })
  .WebSocket;

type SocketFrame = { type?: string; envelope_id?: string; payload?: unknown };

async function openConnection(appToken: string): Promise<string> {
  const res = await fetch(`${SLACK_API_BASE}/apps.connections.open`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${appToken}`,
      "Content-Type": "application/x-www-form-urlencoded",
    },
  });
  const body = (await res.json()) as { ok?: boolean; url?: string; error?: string };
  if (!body.ok || !body.url) {
    throw new Error(`apps.connections.open failed: ${body.error ?? "no url returned"}`);
  }
  return body.url;
}

export async function handleFrame(
  ws: MinimalWs,
  data: unknown,
  orgId: string,
  pluginName: string,
): Promise<void> {
  let frame: SocketFrame;
  try {
    frame = JSON.parse(typeof data === "string" ? data : String(data)) as SocketFrame;
  } catch {
    return;
  }
  if (frame.type === "hello") return;
  if (frame.type === "disconnect") {
    ws.close();
    return;
  }
  // Ack before processing — Slack requires it within 3s.
  if (frame.envelope_id) ws.send(JSON.stringify({ envelope_id: frame.envelope_id }));
  if (
    frame.payload &&
    (frame.type === "events_api" ||
      frame.type === "slash_commands" ||
      frame.type === "interactive")
  ) {
    await processInboundUpdate(orgId, pluginName, frame.payload);
  }
}

function pumpConnection(
  wssUrl: string,
  orgId: string,
  pluginName: string,
  isStopped: () => boolean,
): Promise<void> {
  return new Promise<void>((resolve) => {
    const ws = new WebSocketCtor!(wssUrl);
    let settled = false;
    const finish = (): void => {
      if (settled) return;
      settled = true;
      clearInterval(stopTimer);
      ws.close();
      resolve();
    };
    const stopTimer = setInterval(() => {
      if (isStopped()) finish();
    }, 1000);
    stopTimer.unref?.();
    ws.addEventListener("message", (ev) => {
      void handleFrame(ws, ev.data, orgId, pluginName).catch((e) =>
        console.warn(`[channel-inbound] ${pluginName} socket frame error: ${msg(e)}`),
      );
    });
    ws.addEventListener("close", finish);
    ws.addEventListener("error", finish);
  });
}

/**
 * Slack Socket Mode loop: connect, pump, reconnect with
 * backoff. No-op (warns once) when the channel has no SLACK_APP_TOKEN.
 */
export async function runSlackSocketLoop(opts: {
  orgId: string;
  pluginName: string;
  isStopped: () => boolean;
}): Promise<void> {
  const { orgId, pluginName, isStopped } = opts;
  if (!WebSocketCtor) {
    console.warn(
      `[channel-inbound] ${pluginName}: global WebSocket unavailable — Slack inbound disabled.`,
    );
    return;
  }
  let warnedNoToken = false;
  let failureStreak = 0;
  while (!isStopped()) {
    try {
      const appToken = (await channelEnv(pluginName)).SLACK_APP_TOKEN;
      if (!appToken) {
        if (!warnedNoToken) {
          console.warn(
            `[channel-inbound] ${pluginName}: no SLACK_APP_TOKEN — Slack inbound disabled. Enable Socket Mode and set an xapp- app-level token (connections:write).`,
          );
          warnedNoToken = true;
        }
        return;
      }
      warnedNoToken = false;
      const wssUrl = await openConnection(appToken);
      console.log(`[channel-inbound] ${pluginName}: Socket Mode connected`);
      await pumpConnection(wssUrl, orgId, pluginName, isStopped);
      failureStreak = 0;
    } catch (err) {
      failureStreak++;
      console.warn(
        `[channel-inbound] ${pluginName} socket error (attempt ${failureStreak}): ${msg(err)}`,
      );
    }
    if (!isStopped()) await sleep(pollBackoffMs(failureStreak));
  }
}
