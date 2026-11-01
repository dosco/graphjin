import { readSemanticSearchEnabled } from "@neko/db";

/**
 * Whether the admin "Semantic search" switch is on. Every embedding caller
 * checks this first: when it is off, OpenNeko never calls the embedding
 * service. The value is cached briefly so hot paths (each memory or skill
 * search) do not read the database on every call; a change made in another
 * process takes effect within SEMANTIC_SEARCH_CACHE_MS.
 */
export const SEMANTIC_SEARCH_CACHE_MS = 10_000;

let cached: { value: boolean; at: number } | null = null;
let pending: Promise<boolean> | null = null;
let override: boolean | null = null;

export async function semanticSearchEnabled(now = Date.now()): Promise<boolean> {
  if (override !== null) return override;
  if (cached && now - cached.at < SEMANTIC_SEARCH_CACHE_MS) return cached.value;
  pending ??= readSemanticSearchEnabled()
    .catch((error) => {
      // An unreadable setting is treated as off: keyword search still works.
      console.warn(
        "[semantic-search] could not read the setting; treating it as off:",
        error instanceof Error ? error.message : error,
      );
      return false;
    })
    .then((value) => {
      cached = { value, at: Date.now() };
      return value;
    })
    .finally(() => {
      pending = null;
    });
  return pending;
}

/** Drop the cached value, so this process reads a just-saved change at once. */
export function invalidateSemanticSearchCache(): void {
  cached = null;
}

/** Tests only: pin the switch (true/false) or restore the database value (null). */
export function setSemanticSearchEnabledForTests(value: boolean | null): void {
  override = value;
  cached = null;
}
