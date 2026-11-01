// Backoff + log-gating policy for the inbound poll loop, factored out of
// inbound-poll.ts so it can be unit-tested without the loop's registry and
// delivery dependencies.

export const POLL_INTERVAL_MS = 3_000;
const MAX_BACKOFF_MS = 60_000;
const REPEAT_LOG_EVERY = 10;

/**
 * Delay before the next poll. Healthy (streak 0) polls at the base interval;
 * each consecutive failure doubles the delay up to a cap, so a downed channel
 * stops hammering the plugin VM every 3s (and stops flooding the log).
 */
export function pollBackoffMs(failureStreak: number): number {
  if (failureStreak <= 0) return POLL_INTERVAL_MS;
  const exp = Math.min(failureStreak - 1, 6);
  return Math.min(POLL_INTERVAL_MS * 2 ** exp, MAX_BACKOFF_MS);
}

/**
 * Whether to log this poll failure: always for a new/changed error, otherwise
 * only every REPEAT_LOG_EVERY-th identical repeat — so a persistent outage
 * leaves a periodic breadcrumb instead of one line per cycle.
 */
export function shouldLogPollFailure(failureStreak: number, changed: boolean): boolean {
  return changed || failureStreak % REPEAT_LOG_EVERY === 0;
}
