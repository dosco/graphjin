import { mkdir, mkdtemp, readFile, rename, rm, writeFile, stat } from "node:fs/promises";
import { join } from "node:path";
import { createHash } from "node:crypto";
import { startupEvent, startupPhase } from "@neko/telemetry/startup";
import type { PrefetchKnowledgeResult } from "./knowledge-pack";

const CACHE_FILE = ".knowledge-snapshot.json";
export const KNOWLEDGE_FILES = ["tables.json", "namespaces.json", "insights.json", "syntax.json", "INDEX.md", "mode.json"] as const;
type Snapshot = {
  format: 1;
  source: string;
  revision: string;
  builtAt?: number;
  files: Record<(typeof KNOWLEDGE_FILES)[number], string>;
};
const pending = new Map<string, Promise<PrefetchKnowledgeResult>>();

export async function removeKnowledgeSnapshot(root: string): Promise<void> {
  await rm(join(root, CACHE_FILE), { force: true });
}

export async function readKnowledgeSnapshot(root: string): Promise<Snapshot | null> {
  try {
    const value = JSON.parse(await readFile(join(root, CACHE_FILE), "utf8")) as Snapshot;
    if (value.format !== 1 || typeof value.source !== "string" || typeof value.revision !== "string") return null;
    for (const file of KNOWLEDGE_FILES) {
      if (typeof value.files[file] !== "string") return null;
      if (file.endsWith(".json")) JSON.parse(value.files[file]);
    }
    value.builtAt ??= (await stat(join(root, CACHE_FILE))).mtimeMs;
    return value;
  } catch { return null; }
}

async function publish(root: string, snapshot: Snapshot): Promise<void> {
  await mkdir(root, { recursive: true });
  const temp = await mkdtemp(join(root, ".publish-"));
  try {
    await writeFile(join(temp, CACHE_FILE), JSON.stringify(snapshot));
    await rename(join(temp, CACHE_FILE), join(root, CACHE_FILE));
  } finally { await rm(temp, { recursive: true, force: true }); }
}

/** Never expose the old datasource's files when its replacement cannot be loaded. */
export async function clearKnowledgeSnapshot(root: string, source: string, mode: string): Promise<void> {
  const files = Object.fromEntries(KNOWLEDGE_FILES.map(file => [file,
    file === "mode.json" ? JSON.stringify({ mode }) : file === "INDEX.md" ? "" : "{}",
  ])) as Snapshot["files"];
  await publish(root, { format: 1, source, revision: "", files });
}

function result(snapshot: Snapshot): PrefetchKnowledgeResult {
  return { ok: true, files: KNOWLEDGE_FILES.slice(0, 4).map(file => ({
    file, bytes: Buffer.byteLength(snapshot.files[file]),
  })) };
}

/** One complete snapshot, atomically replaced only after a stable revision is fetched. */
export async function refreshKnowledgeSnapshot(args: {
  root: string;
  source: string;
  mode: string;
  refresh?: boolean;
  revision: () => Promise<string>;
  build: (directory: string) => Promise<PrefetchKnowledgeResult>;
}): Promise<PrefetchKnowledgeResult> {
  const cached = await startupPhase("knowledge.snapshot_read", () => readKnowledgeSnapshot(args.root));
  const cacheMeta = { snapshotAgeMs: cached?.builtAt ? Math.max(0, Date.now() - cached.builtAt) : undefined,
    revision: cached?.revision ? createHash("sha256").update(cached.revision).digest("hex").slice(0, 16) : undefined,
    backgroundRefresh: args.refresh === true };

  // Warm callers do not wait for the worker's in-flight background refresh.
  if (cached?.source === args.source && cached.revision && !args.refresh) { startupEvent("knowledge.cache", { ...cacheMeta, outcome: "hit" }); return result(cached); }
  const key = JSON.stringify([args.root, args.source]);
  const existing = pending.get(key);
  if (existing) { startupEvent("knowledge.cache", { ...cacheMeta, outcome: "coalesced" }); return startupPhase("knowledge.refresh_wait", () => existing); }
  startupEvent("knowledge.cache", { ...cacheMeta, outcome: args.refresh ? "revision_check" : "miss", reason: !cached ? "absent" : cached.source !== args.source ? "source_changed" : "refresh_required" });
  const operation = (async () => {
    try {
      if (cached?.source !== args.source) await clearKnowledgeSnapshot(args.root, args.source, args.mode);
      for (let attempt = 0; attempt < 2; attempt++) {
        const revision = await startupPhase("knowledge.revision_check", args.revision);
        if (!revision) throw new Error("GraphJin returned no catalog revision");
        if (cached?.source === args.source && cached.revision === revision) { startupEvent("knowledge.cache", { ...cacheMeta, outcome: "revision_unchanged" }); return result(cached); }
        const temp = await mkdtemp(join(args.root, ".build-"));
        try {
          const built = await startupPhase("knowledge.rebuild", () => args.build(temp));
          if (!built.ok) throw new Error(built.error ?? "knowledge build failed");
          const files = Object.fromEntries(await Promise.all(KNOWLEDGE_FILES.map(async file => {
            const text = await readFile(join(temp, file), "utf8");
            if (file.endsWith(".json")) JSON.parse(text);
            return [file, text];
          }))) as Snapshot["files"];
          if (await startupPhase("knowledge.revision_check", args.revision) !== revision) continue;
          const snapshot: Snapshot = { format: 1, source: args.source, revision, files, builtAt: Date.now() };
          await startupPhase("knowledge.publish", () => publish(args.root, snapshot));
          startupEvent("knowledge.cache", { outcome: "rebuilt", revision: createHash("sha256").update(revision).digest("hex").slice(0, 16), snapshotAgeMs: 0 });
          return result(snapshot);
        } finally { await rm(temp, { recursive: true, force: true }); }
      }
      throw new Error("GraphJin catalog changed during knowledge refresh; retrying on the next sweep");
    } catch (error) {
      startupEvent("knowledge.cache", { ...cacheMeta, outcome: "refresh_failed", staleAvailable: cached?.source === args.source && Boolean(cached.revision), errorType: error instanceof Error ? error.name : "unknown" });
      return { ok: false, files: [], error: error instanceof Error ? error.message : String(error) };
    }
  })();
  pending.set(key, operation);
  try { return await operation; } finally { pending.delete(key); }
}
