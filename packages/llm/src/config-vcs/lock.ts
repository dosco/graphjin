/**
 * Per-repo async mutex: all git operations against one org repo are
 * serialized (the git index is not concurrency-safe). Promise-chain
 * implementation — no dependency, FIFO order.
 */
const chains = new Map<string, Promise<unknown>>();

export function withRepoLock<T>(key: string, fn: () => Promise<T>): Promise<T> {
  const prev = chains.get(key) ?? Promise.resolve();
  const next = prev.then(fn, fn);
  chains.set(
    key,
    next.catch(() => {}),
  );
  return next;
}
