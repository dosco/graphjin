"use client";

/** Bot chats the reader opened or showed intent to open, kept for an instant reopen. */
const MAX_THREADS = 12;
const cache = new Map<string, unknown>();
const pending = new Map<string, Promise<unknown | null>>();

export const CHAT_PAGE_RUNS = 20;

export function cachedThread<T>(threadId: string): T | null {
  return (cache.get(threadId) as T | undefined) ?? null;
}

export function rememberThread(threadId: string, bundle: unknown) {
  cache.delete(threadId);
  cache.set(threadId, bundle);
  while (cache.size > MAX_THREADS) cache.delete(cache.keys().next().value as string);
}

/** Fetches the latest page of a bot chat once; later callers share the request. */
export function fetchThreadPage<T>(threadId: string): Promise<T | null> {
  const inFlight = pending.get(threadId);
  if (inFlight) return inFlight as Promise<T | null>;
  const request = fetch(`/api/work/threads/${threadId}?limit=${CHAT_PAGE_RUNS}`)
    .then((res) => (res.ok ? (res.json() as Promise<T>) : null))
    .then((bundle) => {
      if (bundle) rememberThread(threadId, bundle);
      return bundle;
    })
    .catch(() => null)
    .finally(() => pending.delete(threadId));
  pending.set(threadId, request);
  return request;
}

export function prefetchThread(threadId: string | null) {
  if (threadId && !cache.has(threadId)) void fetchThreadPage(threadId);
}

const VISIBLE_PREFETCH_MAX = 4;

/** Prefetches up to four chats whose rows scroll into view, one at a time while idle. Returns a cleanup. */
export function prefetchVisibleThreads(rows: Iterable<HTMLElement>): () => void {
  if (typeof IntersectionObserver === "undefined") return () => {};
  const queue: string[] = [];
  let started = 0;
  let idle: number | null = null;
  const schedule = typeof requestIdleCallback === "function"
    ? (fn: () => void) => requestIdleCallback(fn, { timeout: 2000 })
    : (fn: () => void) => window.setTimeout(fn, 300);
  const cancel = (id: number) =>
    typeof cancelIdleCallback === "function" ? cancelIdleCallback(id) : window.clearTimeout(id);
  const drain = () => {
    idle = null;
    const next = queue.shift();
    if (!next || started >= VISIBLE_PREFETCH_MAX) return;
    started += 1;
    void fetchThreadPage(next).finally(() => {
      if (queue.length) idle = schedule(drain);
    });
  };
  const observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      const id = (entry.target as HTMLElement).dataset.threadId;
      if (entry.isIntersecting && id && !cache.has(id) && !queue.includes(id)) queue.push(id);
    }
    if (queue.length && idle === null) idle = schedule(drain);
  });
  for (const row of rows) observer.observe(row);
  return () => {
    observer.disconnect();
    if (idle !== null) cancel(idle);
  };
}
