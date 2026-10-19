// Inbound transport for channels on hosts without a public webhook URL. For every
// turned-on inbound-capable channel, the worker loops the channel's pollInbound
// (provider-agnostic — no Telegram-specific code here), then hands each raw
// update to processInboundUpdate, which dedupes and dispatches it to the same
// agent entry points the web uses.
//
// Reliability across restarts: the poll cursor is persisted per (org, channel),
// so a restart resumes from the last acknowledged offset; the cursor only
// advances when the whole batch dispatched cleanly, and the dedup ledger makes
// re-polling a partially-processed batch safe (exactly-once dispatch).
//
// No env flag: a channel that a pack turned on and that declares an inbound
// direction is polled automatically, and refresh() follows pack installs and
// removals. Channels reachable by a public webhook (OPENNEKO_PUBLIC_URL set +
// ingress="webhook") receive inbound via POST /channels/:id/inbound instead and
// are skipped here.
import { processInboundUpdate } from "./delivery.js";
import {
  loadPollCursor,
  pruneInboundDedup,
  savePollCursor,
} from "./inbound-store.js";
import { pollBackoffMs, shouldLogPollFailure } from "./poll-backoff.js";
import { getChannelProviders, pollChannelInbound } from "./runtime.js";
import { runSlackSocketLoop } from "./socket-loop.js";

const DEDUP_PRUNE_INTERVAL_MS = 60 * 60 * 1000;

const msg = (e: unknown): string => (e instanceof Error ? e.message : String(e));
const sleep = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));

type Poller = {
  pollInbound: (
    channelId: string,
    cursor?: string,
  ) => Promise<{ updates: unknown[]; cursor?: string }>;
};

const channelPoller: Poller = { pollInbound: pollChannelInbound };

/**
 * One poll step: fetch a batch, dispatch each update exactly-once, and advance
 * the cursor only when the whole batch settled (each update done, duplicate, or
 * dead-lettered). A still-retrying update returns `held: true` with the cursor
 * unchanged, so the batch is re-polled — the ledger skips updates that already
 * settled, and the caller backs off before the next poll.
 */
export async function runPollIteration(
  orgId: string,
  pluginName: string,
  cursor: string | undefined,
  poller: Poller,
): Promise<{ cursor: string | undefined; held: boolean }> {
  const { updates, cursor: next } = await poller.pollInbound(pluginName, cursor);
  let held = false;
  for (const raw of updates) {
    if (!(await processInboundUpdate(orgId, pluginName, raw))) held = true;
  }
  if (!held && next && next !== cursor) {
    await savePollCursor(orgId, pluginName, next);
    return { cursor: next, held: false };
  }
  return { cursor, held };
}

/**
 * Start inbound transports for the org's turned-on inbound channels. `refresh`
 * starts loops for newly turned-on channels and stops loops for channels no
 * installed pack lists any more.
 */
export function startChannelInbound(orgId: string): { stop: () => void; refresh: () => Promise<void> } {
  let stopped = false;
  const running = new Map<string, { stopped: boolean }>();

  const pruneTimer = setInterval(() => {
    void pruneInboundDedup(Date.now())
      .then((n) => {
        if (n > 0) console.log(`[channel-inbound] pruned ${n} expired dedup row(s)`);
      })
      .catch((e) => console.warn(`[channel-inbound] dedup prune error: ${msg(e)}`));
  }, DEDUP_PRUNE_INTERVAL_MS);
  pruneTimer.unref?.();

  async function refresh(): Promise<void> {
    if (stopped) return;
    const hasPublicWebhook = Boolean(process.env.OPENNEKO_PUBLIC_URL);
    const inboundProviders = (await getChannelProviders(orgId)).filter((p) =>
      p.directions.includes("inbound"),
    );
    const wanted = new Set<string>(inboundProviders.map((p) => p.id));
    for (const [id, handle] of running) {
      if (wanted.has(id)) continue;
      handle.stopped = true;
      running.delete(id);
      console.log(`[channel-inbound] ${id}: turned off — inbound stopped`);
    }
    for (const provider of inboundProviders) {
      if (running.has(provider.id)) continue;
      if (hasPublicWebhook && provider.ingress === "webhook") {
        console.log(
          `[channel-inbound] ${provider.id}: webhook ingress at ${process.env.OPENNEKO_PUBLIC_URL}/channels/${encodeURIComponent(provider.id)}/inbound`,
        );
        continue;
      }
      const handle = { stopped: false };
      running.set(provider.id, handle);
      const isStopped = () => stopped || handle.stopped;
      if (provider.ingress === "socket") {
        void runSlackSocketLoop({ orgId, pluginName: provider.id, isStopped });
        continue;
      }
      void pollLoop(provider.id, isStopped);
    }
  }

  async function pollLoop(channelId: string, isStopped: () => boolean): Promise<void> {
    let cursor = await loadPollCursor(orgId, channelId);
    let failureStreak = 0;
    let lastError: string | undefined;
    console.log(
      `[channel-inbound] ${channelId}: polling for inbound (no public webhook URL)${cursor ? " — resumed from saved cursor" : ""}`,
    );
    while (!isStopped()) {
      try {
        const res = await runPollIteration(orgId, channelId, cursor, channelPoller);
        cursor = res.cursor;
        if (res.held) {
          // A still-retrying update holds the cursor; back off (shared streak)
          // so we don't re-poll the failing batch every base interval.
          failureStreak++;
          if (shouldLogPollFailure(failureStreak, failureStreak === 1)) {
            console.warn(
              `[channel-inbound] ${channelId}: holding cursor, retrying failed update(s) (attempt ${failureStreak})`,
            );
          }
        } else if (failureStreak > 0) {
          console.log(
            `[channel-inbound] ${channelId}: recovered after ${failureStreak} held/failed attempt(s)`,
          );
          failureStreak = 0;
          lastError = undefined;
        }
      } catch (err) {
        const m = msg(err);
        if (m.includes("does not implement poll_inbound")) {
          console.warn(
            `[channel-inbound] ${channelId}: no poll transport and no public webhook URL — inbound disabled. Set OPENNEKO_PUBLIC_URL to use webhook ingress.`,
          );
          return; // can't be polled; retrying won't help
        }
        if (!isStopped()) {
          failureStreak++;
          if (shouldLogPollFailure(failureStreak, m !== lastError)) {
            const attempt = failureStreak > 1 ? ` (attempt ${failureStreak})` : "";
            console.warn(`[channel-inbound] ${channelId} poll error${attempt}: ${m}`);
          }
          lastError = m;
        }
      }
      if (!isStopped()) await sleep(pollBackoffMs(failureStreak));
    }
  }

  void refresh().catch((e) => console.warn(`[channel-inbound] start failed: ${msg(e)}`));

  return {
    stop: () => {
      stopped = true;
      for (const handle of running.values()) handle.stopped = true;
      running.clear();
      clearInterval(pruneTimer);
    },
    refresh,
  };
}
