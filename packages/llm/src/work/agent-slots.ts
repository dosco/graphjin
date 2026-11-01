import { AGENT_DEFAULT_GLOBAL_CAP } from "../agent-backend";

/** Ask questions one web host runs at once; more wait in line. */
export const ASK_DEFAULT_CONCURRENCY = 2;
const HEARTBEAT_MS = 60_000;

function defaultLimit(): number {
  const configured = Number(process.env.OPENNEKO_AGENT_CONCURRENCY);
  if (Number.isInteger(configured) && configured >= 1) return configured;
  return (process.env.OPENNEKO_SANDBOX_OWNER ?? "").includes("worker")
    ? AGENT_DEFAULT_GLOBAL_CAP
    : ASK_DEFAULT_CONCURRENCY;
}

let limit: number | null = null;
let active = 0;
const waiting: Array<() => void> = [];

function currentLimit(): number {
  return limit ?? (limit = defaultLimit());
}

function admitNext(): void {
  while (active < currentLimit() && waiting.length > 0) {
    active += 1;
    waiting.shift()!();
  }
}

/** The worker sets its configured agent cap at boot. */
export function setAgentSlotLimit(next: number): void {
  if (!Number.isInteger(next) || next < 1) throw new Error("agent slot limit must be a positive integer");
  limit = next;
  admitNext();
}

export function agentSlotUsage(): { active: number; waiting: number; limit: number } {
  return { active, waiting: waiting.length, limit: currentLimit() };
}

/**
 * Hold one agent sandbox slot for this process. While the run waits, it
 * reports once through `onWait` and refreshes its row through `heartbeat`, so
 * the stale-run sweep does not cancel it.
 */
export async function acquireAgentSlot(opts: {
  signal?: AbortSignal;
  onWait?: () => Promise<void> | void;
  heartbeat?: () => Promise<void>;
} = {}): Promise<() => void> {
  if (opts.signal?.aborted) throw opts.signal.reason ?? new Error("aborted");
  let released = false;
  const release = () => {
    if (released) return;
    released = true;
    active -= 1;
    admitNext();
  };
  if (active < currentLimit() && waiting.length === 0) {
    active += 1;
    return release;
  }
  await opts.onWait?.();
  const beat = opts.heartbeat
    ? setInterval(() => { void opts.heartbeat!().catch(() => undefined); }, HEARTBEAT_MS)
    : undefined;
  try {
    await new Promise<void>((resolve, reject) => {
      const admit = () => {
        opts.signal?.removeEventListener("abort", abort);
        resolve();
      };
      const abort = () => {
        const index = waiting.indexOf(admit);
        if (index >= 0) waiting.splice(index, 1);
        reject(opts.signal?.reason ?? new Error("aborted"));
      };
      if (opts.signal?.aborted) {
        reject(opts.signal.reason ?? new Error("aborted"));
        return;
      }
      waiting.push(admit);
      opts.signal?.addEventListener("abort", abort, { once: true });
    });
  } finally {
    if (beat) clearInterval(beat);
  }
  return release;
}
