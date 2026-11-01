// The admin "Semantic search" switch: read and save. Saving applies the
// change everywhere it matters: this process's cache, the embedding backlog
// (indexed in the background after turning on), and GraphJin (semantic
// catalog and Library search, applied live).

import { pool, readSemanticSearchEnabled, writeSemanticSearchEnabled } from "@neko/db";
import { embeddingBacklogCount, requestEmbeddingBackfill } from "./embedding-jobs";
import { provisionGraphjinSemanticSearch } from "./graphjin/semantic-search";
import { invalidateSemanticSearchCache } from "./semantic-search";

export type SemanticSearchSetting = {
  enabled: boolean;
  /** Memories and skills not indexed yet; absent after a save. */
  waiting?: number;
  /** How GraphJin took the change; absent when reading. */
  graphjin?: { status: "applied" } | { status: "skipped" | "failed"; reason: string };
};

export async function getSemanticSearchSetting(): Promise<SemanticSearchSetting> {
  const [enabled, waiting] = await Promise.all([readSemanticSearchEnabled(), embeddingBacklogCount(pool())]);
  return { enabled, waiting };
}

export type SaveSemanticSearchDeps = {
  write: (orgId: string, enabled: boolean) => Promise<void>;
  backfill: () => Promise<unknown>;
  graphjin: (orgId: string, enabled: boolean) => Promise<{ status: "applied" } | { status: "skipped"; reason: string }>;
};

const defaultDeps: SaveSemanticSearchDeps = {
  write: writeSemanticSearchEnabled,
  backfill: () => requestEmbeddingBackfill("enabled"),
  graphjin: (orgId, enabled) => provisionGraphjinSemanticSearch(orgId, { enabled }),
};

/**
 * Saves the switch. On: queues the background backfill of every memory and
 * skill without a vector. Off: nothing new is scheduled; stored vectors stay,
 * so turning it back on only indexes what changed meanwhile. The setting is
 * saved even when GraphJin cannot take the change now; the worker's next
 * provisioning pass retries it.
 */
export async function saveSemanticSearchSetting(
  orgId: string,
  enabled: boolean,
  deps: SaveSemanticSearchDeps = defaultDeps,
): Promise<SemanticSearchSetting> {
  await deps.write(orgId, enabled);
  invalidateSemanticSearchCache();
  if (enabled) {
    await deps.backfill().catch((error) => {
      // The worker's minute sweep queues it instead.
      console.warn("[semantic-search] could not queue the backfill:", error instanceof Error ? error.message : error);
    });
  }
  let graphjin: SemanticSearchSetting["graphjin"];
  try {
    graphjin = await deps.graphjin(orgId, enabled);
  } catch (error) {
    graphjin = { status: "failed", reason: error instanceof Error ? error.message : String(error) };
  }
  return { enabled, graphjin };
}
