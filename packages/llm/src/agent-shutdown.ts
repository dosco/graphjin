/**
 * Backend-agnostic registry of in-flight agent calls.
 *
 * Each backend, when it starts a long-running agent invocation, registers
 * a cancel callback (kill the spawned process tree, abort the SDK
 * AbortController, etc.) and removes itself when the call completes.
 *
 * The worker calls `cancelAllAgents()` from its SIGTERM handler before
 * pg-boss `stop({graceful:true})` so jobs fail fast instead of blocking
 * the systemd unit past its TimeoutStopSec.
 *
 * Why this is necessary: agent backends spawn external processes (the Ax
 * Harness) which may fork their own subprocesses (shell commands, Python
 * scripts, etc.). Some of those grandchildren detach themselves and
 * survive even systemd's cgroup SIGKILL, leaving orphan processes
 * across redeploys. Application-level cancellation is the only reliable
 * fix; relying on cgroup teardown alone has proved flaky in practice.
 */

const cancellers = new Set<() => void>();

/**
 * Register a cancel callback. Returns the unregister function — call it
 * once the agent run completes (success or failure) so the registry
 * doesn't grow unbounded.
 */
export function registerAgentCanceller(fn: () => void): () => void {
  cancellers.add(fn);
  return () => cancellers.delete(fn);
}

/**
 * Synchronously cancel every in-flight agent call. Returns how many
 * cancellers were fired. Idempotent — safe to call multiple times.
 */
export function cancelAllAgents(): number {
  const n = cancellers.size;
  for (const fn of cancellers) {
    try {
      fn();
    } catch {
      // best-effort; one failed canceller mustn't block the rest
    }
  }
  cancellers.clear();
  return n;
}
