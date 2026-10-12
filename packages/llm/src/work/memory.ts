import { createHmac, randomUUID } from "node:crypto";
import { deriveSigningSecret } from "@neko/secret-crypt";
import {
  and,
  asc,
  config_ref,
  db,
  desc,
  eq,
  isNull,
  memory_fork,
  ne,
  sql,
  work_memory,
  work_memory_event,
  work_message,
  work_pending_memory,
  work_run,
} from "@neko/db";
import { embedText, vectorLiteral } from "../embedding";
import {
  WORK_MEMORY_KINDS,
  WORK_MEMORY_SCOPES,
  WORK_PENDING_MEMORY_STATUSES,
  type WorkMemoryContext,
  type WorkMemoryKind,
  type WorkMemoryScope,
  type WorkPendingMemoryStatus,
} from "./memory-types";

export {
  WORK_MEMORY_KINDS,
  WORK_MEMORY_SCOPES,
  WORK_PENDING_MEMORY_STATUSES,
  type WorkMemoryContext,
  type WorkMemoryKind,
  type WorkMemoryScope,
  type WorkPendingMemoryStatus,
} from "./memory-types";

export type WorkMemory = {
  id: string;
  orgId: string;
  kind: WorkMemoryKind;
  scope: WorkMemoryScope;
  scopeId: string | null;
  text: string;
  pinned: boolean;
  confidence: number;
  metadata: Record<string, unknown>;
  sourceRunId: string | null;
  sourceThreadId: string | null;
  useCount: number;
  lastUsedAt: string | null;
  userId: string | null;
  originId: string | null;
  overridesOriginId: string | null;
  suppressed: boolean;
  promotedFromId: string | null;
  promotedBy: string | null;
  promotedAt: string | null;
  /** SEC6: false = stored integrity hash no longer matches (tampered); dropped from agent context. */
  integrityOk: boolean;
  expiresAt: string | null;
  archivedAt: string | null;
  createdAt: string;
  updatedAt: string;
};

export type RememberWorkMemoryInput = WorkMemoryContext & {
  kind: WorkMemoryKind;
  scope?: WorkMemoryScope;
  scopeId?: string | null;
  text: string;
  pinned?: boolean;
  confidence?: number;
  metadata?: Record<string, unknown>;
};

export type WorkMemorySearchResult = {
  source: "saved_memory";
  memory: WorkMemory;
  score: number;
};

export type WorkArchiveSearchResult = {
  source: "thread_archive" | "run_archive";
  runId: string | null;
  threadId: string;
  role: "user" | "assistant";
  text: string;
  ts: string;
  score: number;
};

export type SearchWorkMemoryInput = WorkMemoryContext & {
  query: string;
  limit?: number;
  includeArchives?: boolean;
};

export function normalizeNewWorkMemoryScope(
  scope: WorkMemoryScope | undefined,
  ctx: { threadId?: string | null } = {},
): "global" | "thread" {
  if (scope === "thread" && ctx.threadId) return "thread";
  return "global";
}

export type WorkPendingMemoryConflict = {
  memoryId: string;
  text: string;
  similarity: number;
};

export type WorkPendingMemory = {
  id: string;
  orgId: string;
  threadId: string | null;
  runId: string | null;
  status: WorkPendingMemoryStatus;
  draftText: string;
  draftKind: WorkMemoryKind;
  draftScope: WorkMemoryScope;
  draftScopeId: string | null;
  confidence: number;
  reasoning: string | null;
  conflicts: WorkPendingMemoryConflict[];
  decisionText: string | null;
  decidedAt: string | null;
  memoryId: string | null;
  createdAt: string;
  updatedAt: string;
};

type WorkMemoryRow = typeof work_memory.$inferSelect;
type WorkPendingMemoryRow = typeof work_pending_memory.$inferSelect;

const CORE_GLOBAL_KINDS = new Set<WorkMemoryKind>([
  "preference",
  "business_rule",
  "metric_definition",
  "company_context",
]);

const CORE_THREAD_KINDS = new Set<WorkMemoryKind>([
  "business_rule",
  "metric_definition",
  "thread_note",
  "correction",
]);

/** CV2: a member's writes land in their personal layer; everyone else's in the team layer. */
export function memoryLayerForActor(actor: {
  userId: string | null;
  role: string | null;
}): string | null {
  return actor.role === "member" && actor.userId ? actor.userId : null;
}

/**
 * The active memory layer: members resolve to their own personal layer,
 * everyone else to the shared team layer (null).
 */
export function effectiveMemoryLayer(
  orgId: string,
  actor: { userId: string | null; role: string | null },
): string | null {
  void orgId;
  return memoryLayerForActor(actor);
}

async function resolveRunMemoryLayer(runId: string | null): Promise<string | null> {
  if (!runId) return null;
  const [run] = await db()
    .select({
      orgId: work_run.org_id,
      userId: work_run.actor_user_id,
      role: work_run.actor_role,
    })
    .from(work_run)
    .where(eq(work_run.id, runId))
    .limit(1);
  return run ? effectiveMemoryLayer(run.orgId, run) : null;
}

// Layered visibility (CV2). Team context sees only team rows. A member
// sees their own live personal rows plus team rows whose origin they
// haven't overridden — a personal row pointing overrides_origin_id at a
// team row's origin replaces it (edit) or hides it (suppressed).
async function runTeamMemory(orgId: string, runId: string): Promise<"*" | ReadonlySet<string>> {
  const { entitlementActorForRun } = await import("./entitlement-scope");
  const { heldItems } = await import("@neko/db");
  const actor = await entitlementActorForRun(orgId, runId);
  return actor ? heldItems(actor, "team_memory") : new Set<string>();
}

function teamMemoryFilter(teamMemory: "*" | ReadonlySet<string> | undefined) {
  if (!teamMemory || teamMemory === "*") return sql`true`;
  const held = `{${[...teamMemory].map((v) => `"${v.replace(/(["\\])/g, "\\$1")}"`).join(",")}}`;
  return sql`(${work_memory.scope} = 'thread'
    or (${work_memory.scope} = 'global' and 'global' = any(${held}::text[]))
    or (${work_memory.scope} = 'database' and ('database:' || coalesce(${work_memory.scope_id}, '')) = any(${held}::text[])))`;
}

function layerVisibilityFilter(userId: string | null | undefined, teamMemory?: "*" | ReadonlySet<string>) {
  const team = teamMemoryFilter(teamMemory);
  if (!userId) return and(isNull(work_memory.user_id), team)!;
  return sql`((${work_memory.user_id} = ${userId} and ${work_memory.suppressed} = false)
    or (${work_memory.user_id} is null and ${team} and not exists (
      select 1 from work_memory p
      where p.org_id = ${work_memory.org_id}
        and p.user_id = ${userId}
        and p.overrides_origin_id = ${work_memory.origin_id}
        and p.archived_at is null
    )))`;
}

/**
 * SEC6: HMAC over the row's identity-bearing fields with a per-org key
 * derived from the deployment secret. A row whose stored hash no longer
 * matches was modified outside this store (DB tampering / memory
 * poisoning) — readers drop it from agent context. Null when the
 * deployment key is unavailable (legacy mode).
 */
function memoryIntegrityHmac(row: {
  orgId: string;
  userId: string | null;
  kind: string;
  scope: string;
  scopeId: string | null;
  text: string;
}): string | null {
  try {
    const key = deriveSigningSecret(`work-memory:${row.orgId}`);
    return createHmac("sha256", key)
      .update(
        [row.userId ?? "", row.kind, row.scope, row.scopeId ?? "", row.text].join(
          "\u0000",
        ),
      )
      .digest("hex");
  } catch {
    return null;
  }
}

/** DATA_LIFECYCLE §4: thread notes are short-lived; durable kinds never expire by default. */
const THREAD_NOTE_TTL_DAYS = 30;
const PENDING_MEMORY_TTL_DAYS = 14;

function expiryForKind(kind: WorkMemoryKind, now: Date): Date | null {
  if (kind !== "thread_note") return null;
  return new Date(now.getTime() + THREAD_NOTE_TTL_DAYS * 24 * 60 * 60 * 1000);
}

/** SEC6: keep tampered rows out of anything an agent reads. */
function dropTampered(memories: WorkMemory[]): WorkMemory[] {
  const ok: WorkMemory[] = [];
  for (const memory of memories) {
    if (memory.integrityOk) {
      ok.push(memory);
    } else {
      console.warn(
        `[work-memory] integrity check FAILED for memory ${memory.id} (org ${memory.orgId}) — excluded from agent context`,
      );
    }
  }
  return ok;
}

export async function rememberWorkMemory(input: RememberWorkMemoryInput): Promise<WorkMemory> {
  assertKind(input.kind);
  const scope = normalizeNewWorkMemoryScope(input.scope, {
    threadId: input.threadId ?? null,
  });
  const now = new Date();
  const scopeId = resolveScopeId(scope, input);
  const pinned = input.pinned ?? shouldPinByDefault(input.kind, scope);
  const text = input.text.trim();
  const layerUserId =
    input.userId !== undefined
      ? input.userId
      : await resolveRunMemoryLayer(input.runId ?? null);
  const id = randomUUID();
  const rows = await db()
    .insert(work_memory)
    .values({
      id,
      org_id: input.orgId,
      kind: input.kind,
      scope,
      scope_id: scopeId,
      text,
      pinned,
      confidence: clamp(input.confidence ?? 0.8, 0, 1),
      metadata: input.metadata ?? {},
      source_run_id: input.runId ?? null,
      source_thread_id: input.threadId ?? null,
      user_id: layerUserId,
      origin_id: id,
      integrity_hmac: memoryIntegrityHmac({
        orgId: input.orgId,
        userId: layerUserId,
        kind: input.kind,
        scope,
        scopeId,
        text,
      }),
      expires_at: expiryForKind(input.kind, now),
      // Atomically mark content for the durable embedding dispatcher.
      embedding: null,
      created_at: now,
      updated_at: now,
    })
    .returning();
  const memory = rowToMemory(rows[0]);
  await insertWorkMemoryEvent({
    orgId: input.orgId,
    memoryId: memory.id,
    runId: input.runId ?? null,
    threadId: input.threadId ?? null,
    action: "remember",
    payload: {
      kind: input.kind,
      scope,
      scopeId,
      pinned,
      userId: layerUserId,
      text: input.text.trim(),
    },
  });
  return memory;
}

export async function archiveWorkMemory(
  orgId: string,
  id: string,
  ctx: Omit<WorkMemoryContext, "orgId"> & { reason?: string } = {},
): Promise<boolean> {
  const now = new Date();
  // A member (ctx.userId set) can only archive rows in their own layer;
  // team rows are hidden for them via suppression instead.
  const layerGuard = ctx.userId ? eq(work_memory.user_id, ctx.userId) : undefined;
  const rows = await db()
    .update(work_memory)
    .set({ archived_at: now, updated_at: now })
    .where(
      and(
        eq(work_memory.org_id, orgId),
        eq(work_memory.id, id),
        isNull(work_memory.archived_at),
        ...(layerGuard ? [layerGuard] : []),
      ),
    )
    .returning({ id: work_memory.id });
  if (!rows[0]) return false;
  await insertWorkMemoryEvent({
    orgId,
    memoryId: id,
    runId: ctx.runId ?? null,
    threadId: ctx.threadId ?? null,
    action: "forget",
    payload: { reason: ctx.reason ?? null },
  });
  return true;
}

/**
 * An agent edits a memory its run can see. A member's run edits through
 * their personal layer (copy-on-write for team rows); a team-layer run
 * rewrites the team row in place.
 */
export async function updateWorkMemoryForRun(input: {
  orgId: string;
  runId: string | null;
  threadId?: string | null;
  memoryId: string;
  text: string;
}): Promise<WorkMemory> {
  const text = input.text.trim();
  if (!text) throw new Error("Memory text is empty");
  const layerUserId = await resolveRunMemoryLayer(input.runId);
  const teamMemory = input.runId ? await runTeamMemory(input.orgId, input.runId) : undefined;
  const [visible] = await db()
    .select()
    .from(work_memory)
    .where(
      and(
        eq(work_memory.org_id, input.orgId),
        eq(work_memory.id, input.memoryId),
        isNull(work_memory.archived_at),
        layerVisibilityFilter(layerUserId, teamMemory),
      ),
    )
    .limit(1);
  const target = visible ? rowToMemory(visible) : null;
  if (!target?.integrityOk) throw new Error(`Memory not found: ${input.memoryId}`);
  if (layerUserId) {
    return overrideWorkMemoryForUser({
      orgId: input.orgId,
      userId: layerUserId,
      memoryId: target.id,
      text,
      runId: input.runId,
      threadId: input.threadId ?? null,
    });
  }
  const rows = await db()
    .update(work_memory)
    .set({
      text,
      integrity_hmac: memoryIntegrityHmac({
        orgId: input.orgId,
        userId: null,
        kind: target.kind,
        scope: target.scope,
        scopeId: target.scopeId,
        text,
      }),
      embedding: null,
      updated_at: new Date(),
    })
    .where(and(eq(work_memory.org_id, input.orgId), eq(work_memory.id, target.id)))
    .returning();
  await insertWorkMemoryEvent({
    orgId: input.orgId,
    memoryId: target.id,
    runId: input.runId,
    threadId: input.threadId ?? null,
    action: "update",
    payload: { previousText: target.text, text },
  });
  return rowToMemory(rows[0]);
}

export async function getWorkMemory(orgId: string, id: string): Promise<WorkMemory | null> {
  const rows = await db()
    .select()
    .from(work_memory)
    .where(and(eq(work_memory.org_id, orgId), eq(work_memory.id, id)))
    .limit(1);
  return rows[0] ? rowToMemory(rows[0]) : null;
}

export async function listWorkMemories(
  orgId: string,
  options: {
    includeArchived?: boolean;
    limit?: number;
    userId?: string | null;
    teamMemory?: "*" | ReadonlySet<string>;
  } = {},
): Promise<WorkMemory[]> {
  const activeFilter = options.includeArchived ? undefined : isNull(work_memory.archived_at);
  const rows = await db()
    .select()
    .from(work_memory)
    .where(
      and(
        eq(work_memory.org_id, orgId),
        layerVisibilityFilter(options.userId, options.teamMemory),
        ...(activeFilter ? [activeFilter] : []),
      ),
    )
    .orderBy(desc(work_memory.pinned), desc(work_memory.updated_at))
    .limit(options.limit ?? 200);
  return rows.map(rowToMemory);
}

export async function getCoreWorkMemories(
  ctx: WorkMemoryContext,
  limit = 16,
): Promise<WorkMemory[]> {
  const rows = await db()
    .select()
    .from(work_memory)
    .where(
      and(
        eq(work_memory.org_id, ctx.orgId),
        layerVisibilityFilter(ctx.userId, ctx.teamMemory),
        isNull(work_memory.archived_at),
      ),
    )
    .orderBy(desc(work_memory.pinned), desc(work_memory.updated_at))
    .limit(500);

  return dropTampered(rows.map(rowToMemory))
    .filter((memory) => {
      if (memory.pinned) return true;
      if (memory.scope === "global" && CORE_GLOBAL_KINDS.has(memory.kind)) return true;
      if (
        memory.scope === "thread" &&
        memory.scopeId === ctx.threadId &&
        CORE_THREAD_KINDS.has(memory.kind)
      ) {
        return true;
      }
      return false;
    })
    .slice(0, limit);
}

export async function formatWorkMemoryPromptContext(
  ctx: WorkMemoryContext,
  options: { contextQuery?: string; contextLimit?: number } = {},
): Promise<string> {
  const core = await getCoreWorkMemories(ctx);
  // When the caller hands us a contextQuery (the user's latest message,
  // a card title, a workflow intent, etc.) pull the top-N semantically
  // closest memories via pgvector. Merge with core (pinned + kind-based)
  // by id so we never repeat a memory.
  const seen = new Set(core.map((m) => m.id));
  const contextual = options.contextQuery?.trim()
    ? (
        await searchWorkMemoryByContext({
          orgId: ctx.orgId,
          query: options.contextQuery,
          limit: options.contextLimit ?? 5,
          userId: ctx.userId ?? null,
          runId: ctx.runId ?? null,
          ...(ctx.teamMemory !== undefined ? { teamMemory: ctx.teamMemory } : {}),
        })
      )
        .map((r) => r.memory)
        .filter((m) => !seen.has(m.id))
    : [];

  const merged = [...core, ...contextual];
  if (merged.length === 0) {
    return "No memories are currently saved for this workspace or thread.";
  }
  const lines = [
    "Memories the operator has saved — treat as durable context:",
    ...merged.map(
      (memory) => `- [${memory.id}] ${formatMemoryLabel(memory)}: ${memory.text}`,
    ),
  ];
  return lines.join("\n");
}

// Global-only prefetch used by one-shot/headless agents (metric, workflow
// runner). Pulls top-N non-archived global memories ordered by pinned desc,
// updated_at desc. No thread scoping, no semantic match — those agents lean
// on the search tool instead of a wide preload.
export async function formatGlobalMemoryPromptContext(
  orgId: string,
  limit = 5,
  teamMemory: "*" | ReadonlySet<string> = "*",
): Promise<string> {
  if (teamMemory !== "*" && !teamMemory.has("global")) return "No global memories are currently saved for this workspace.";
  const rows = await db()
    .select()
    .from(work_memory)
    .where(
      and(
        eq(work_memory.org_id, orgId),
        eq(work_memory.scope, "global"),
        isNull(work_memory.user_id),
        isNull(work_memory.archived_at),
      ),
    )
    .orderBy(desc(work_memory.pinned), desc(work_memory.updated_at))
    .limit(limit);
  const memories = dropTampered(rows.map(rowToMemory));
  if (memories.length === 0) {
    return "No global memories are currently saved for this workspace.";
  }
  return [
    "Memories the operator has saved — treat as durable context:",
    ...memories.map(
      (memory) => `- [${memory.id}] ${formatMemoryLabel(memory)}: ${memory.text}`,
    ),
  ].join("\n");
}

export async function searchWorkMemory(
  input: SearchWorkMemoryInput,
): Promise<{ saved: WorkMemorySearchResult[]; archives: WorkArchiveSearchResult[] }> {
  const limit = clamp(Math.floor(input.limit ?? 8), 1, 20);
  const tokens = tokenize(input.query);
  const saved = await searchSavedWorkMemories(input, tokens, limit);
  const archives =
    input.includeArchives === false
      ? []
      : await searchWorkArchiveMessages(input, tokens, limit);

  if (saved.length > 0) {
    await touchWorkMemories(
      input,
      saved.map((result) => result.memory.id),
    );
  }

  return { saved, archives };
}

// Semantic search via pgvector. Embeds the query with the same
// transformers.js model used at write time and pulls the N nearest
// non-archived memories by cosine distance. Returned `score` is the
// cosine similarity (1 - distance), so higher is better.
export async function searchWorkMemoryByContext(args: {
  orgId: string;
  query: string;
  limit?: number;
  userId?: string | null;
  runId?: string | null;
  teamMemory?: "*" | ReadonlySet<string>;
}): Promise<WorkMemorySearchResult[]> {
  const limit = clamp(Math.floor(args.limit ?? 5), 1, 20);
  const trimmed = args.query.trim();
  if (!trimmed) return [];
  const layerUserId =
    args.userId !== undefined
      ? args.userId
      : await resolveRunMemoryLayer(args.runId ?? null);
  let queryVec: string;
  try {
    queryVec = vectorLiteral(await embedText(trimmed));
  } catch (err) {
    console.error(
      "[work-memory] context search embed failed:",
      err instanceof Error ? err.message : err,
    );
    return [];
  }
  // Use Drizzle's typed select so timestamp columns come back as Dates
  // (not strings) and the row shape matches rowToMemory's expectations.
  // Vector ordering goes in via raw sql; the score is computed as a
  // separate column so we don't have to re-embed for the result.
  const orderExpr = sql`work_memory.embedding <=> ${queryVec}::vector`;
  const rows = await db()
    .select({
      id: work_memory.id,
      org_id: work_memory.org_id,
      kind: work_memory.kind,
      scope: work_memory.scope,
      scope_id: work_memory.scope_id,
      text: work_memory.text,
      pinned: work_memory.pinned,
      confidence: work_memory.confidence,
      metadata: work_memory.metadata,
      source_run_id: work_memory.source_run_id,
      source_thread_id: work_memory.source_thread_id,
      use_count: work_memory.use_count,
      last_used_at: work_memory.last_used_at,
      user_id: work_memory.user_id,
      origin_id: work_memory.origin_id,
      overrides_origin_id: work_memory.overrides_origin_id,
      suppressed: work_memory.suppressed,
      promoted_from_id: work_memory.promoted_from_id,
      promoted_by: work_memory.promoted_by,
      promoted_at: work_memory.promoted_at,
      integrity_hmac: work_memory.integrity_hmac,
      expires_at: work_memory.expires_at,
      archived_at: work_memory.archived_at,
      embedding: work_memory.embedding,
      created_at: work_memory.created_at,
      updated_at: work_memory.updated_at,
      score: sql<number>`1 - (work_memory.embedding <=> ${queryVec}::vector)`,
    })
    .from(work_memory)
    .where(
      and(
        eq(work_memory.org_id, args.orgId),
        layerVisibilityFilter(layerUserId, args.teamMemory ?? (args.runId ? await runTeamMemory(args.orgId, args.runId) : undefined)),
        isNull(work_memory.archived_at),
        sql`work_memory.embedding IS NOT NULL`,
      ),
    )
    .orderBy(orderExpr)
    .limit(limit);
  if (rows.length === 0) return [];
  await touchWorkMemories(
    { orgId: args.orgId, runId: args.runId ?? null },
    rows.map((r) => r.id),
  );
  return rows
    .map((r) => ({
      source: "saved_memory" as const,
      memory: rowToMemory(r),
      score: Number(r.score) || 0,
    }))
    .filter((result) => {
      if (result.memory.integrityOk) return true;
      console.warn(
        `[work-memory] integrity check FAILED for memory ${result.memory.id} (org ${result.memory.orgId}) — excluded from agent context`,
      );
      return false;
    });
}

export async function findConflictingWorkMemories(
  draft: {
    orgId: string;
    text: string;
    kind: WorkMemoryKind;
    scope: WorkMemoryScope;
    scopeId?: string | null;
    userId?: string | null;
  },
  threshold = 0.3,
): Promise<Array<{ memory: WorkMemory; similarity: number }>> {
  assertKind(draft.kind);
  assertScope(draft.scope);
  const scopedById = draft.scope === "thread";
  const rows = await db()
    .select()
    .from(work_memory)
    .where(
      and(
        eq(work_memory.org_id, draft.orgId),
        eq(work_memory.kind, draft.kind),
        eq(work_memory.scope, draft.scope),
        layerVisibilityFilter(draft.userId),
        isNull(work_memory.archived_at),
        ...(scopedById ? [eq(work_memory.scope_id, draft.scopeId ?? "")] : []),
      ),
    );

  const draftTokens = new Set(tokenize(draft.text));
  if (draftTokens.size === 0) return [];

  return rows
    .map((row) => {
      const memory = rowToMemory(row);
      const memoryTokens = new Set(tokenize(memory.text));
      return { memory, similarity: jaccard(draftTokens, memoryTokens) };
    })
    .filter((result) => result.similarity >= threshold)
    .sort((a, b) => b.similarity - a.similarity);
}

/**
 * CV2 copy-on-write personalization. Editing a team memory copies it
 * into the member's personal layer (overrides_origin_id shadows the
 * team row for them); suppress=true hides it for them instead. Editing
 * their own personal row updates it in place. The team row is never
 * touched.
 */
export async function overrideWorkMemoryForUser(input: {
  orgId: string;
  userId: string;
  memoryId: string;
  text?: string;
  suppress?: boolean;
  runId?: string | null;
  threadId?: string | null;
}): Promise<WorkMemory> {
  const target = await getWorkMemory(input.orgId, input.memoryId);
  if (!target || target.archivedAt) {
    throw new Error(`Memory not found: ${input.memoryId}`);
  }
  if (target.userId && target.userId !== input.userId) {
    throw new Error(`Memory not found: ${input.memoryId}`);
  }
  const suppress = input.suppress === true;
  const text = input.text?.trim() || target.text;
  const now = new Date();
  const eventCtx = {
    orgId: input.orgId,
    runId: input.runId ?? null,
    threadId: input.threadId ?? null,
  };

  if (target.userId === input.userId) {
    const rows = await db()
      .update(work_memory)
      .set({
        text,
        suppressed: suppress,
        integrity_hmac: memoryIntegrityHmac({
          orgId: input.orgId,
          userId: input.userId,
          kind: target.kind,
          scope: target.scope,
          scopeId: target.scopeId,
          text,
        }),
        ...(input.text
          ? { embedding: null }
          : {}),
        updated_at: now,
      })
      .where(and(eq(work_memory.org_id, input.orgId), eq(work_memory.id, target.id)))
      .returning();
    const memory = rowToMemory(rows[0]);
    await insertWorkMemoryEvent({
      ...eventCtx,
      memoryId: memory.id,
      action: suppress ? "suppress" : "override",
      payload: { userId: input.userId, originId: memory.originId },
    });
    return memory;
  }

  // Team row: upsert the member's override for this origin. The first
  // override is the implicit fork — record the baseline for 3-way pulls.
  await ensureMemoryFork(input.orgId, input.userId);
  const [existing] = await db()
    .select()
    .from(work_memory)
    .where(
      and(
        eq(work_memory.org_id, input.orgId),
        eq(work_memory.user_id, input.userId),
        eq(work_memory.overrides_origin_id, target.originId ?? target.id),
        isNull(work_memory.archived_at),
      ),
    )
    .limit(1);
  const overrideHmac = memoryIntegrityHmac({
    orgId: input.orgId,
    userId: input.userId,
    kind: target.kind,
    scope: target.scope,
    scopeId: target.scopeId,
    text,
  });
  let memory: WorkMemory;
  if (existing) {
    const rows = await db()
      .update(work_memory)
      .set({
        text,
        suppressed: suppress,
        integrity_hmac: overrideHmac,
        ...(input.text
          ? { embedding: null }
          : {}),
        updated_at: now,
      })
      .where(eq(work_memory.id, existing.id))
      .returning();
    memory = rowToMemory(rows[0]);
  } else {
    const id = randomUUID();
    const rows = await db()
      .insert(work_memory)
      .values({
        id,
        org_id: input.orgId,
        kind: target.kind,
        scope: target.scope,
        scope_id: target.scopeId,
        text,
        pinned: target.pinned,
        confidence: target.confidence,
        metadata: { ...target.metadata, origin: "user_override" },
        source_run_id: input.runId ?? null,
        source_thread_id: input.threadId ?? null,
        user_id: input.userId,
        origin_id: id,
        overrides_origin_id: target.originId ?? target.id,
        suppressed: suppress,
        integrity_hmac: overrideHmac,
        ...(suppress ? {} : { embedding: null }),
        created_at: now,
        updated_at: now,
      })
      .returning();
    memory = rowToMemory(rows[0]);
  }
  await insertWorkMemoryEvent({
    ...eventCtx,
    memoryId: memory.id,
    action: suppress ? "suppress" : "override",
    payload: {
      userId: input.userId,
      overridesOriginId: memory.overridesOriginId,
    },
  });
  return memory;
}

/**
 * CV2 promote: an admin pulls a member's personal memory into the team
 * layer. The new team row keeps the origin lineage; the personal source
 * (and, for an override, the team row it replaced) is archived. Other
 * members' overrides of the same origin keep shadowing the new row.
 */
export async function promoteWorkMemoryToOrg(input: {
  orgId: string;
  memoryId: string;
  promotedBy: string;
  runId?: string | null;
  threadId?: string | null;
}): Promise<WorkMemory> {
  const source = await getWorkMemory(input.orgId, input.memoryId);
  if (!source || source.archivedAt || !source.userId) {
    throw new Error(`Personal memory not found: ${input.memoryId}`);
  }
  if (source.suppressed) {
    throw new Error(`Cannot promote a suppressed memory: ${input.memoryId}`);
  }
  const now = new Date();
  const originId = source.overridesOriginId ?? source.originId ?? source.id;
  if (source.overridesOriginId) {
    await db()
      .update(work_memory)
      .set({ archived_at: now, updated_at: now })
      .where(
        and(
          eq(work_memory.org_id, input.orgId),
          eq(work_memory.origin_id, source.overridesOriginId),
          isNull(work_memory.user_id),
          isNull(work_memory.archived_at),
        ),
      );
  }
  const rows = await db()
    .insert(work_memory)
    .values({
      org_id: input.orgId,
      kind: source.kind,
      scope: source.scope,
      scope_id: source.scopeId,
      text: source.text,
      pinned: source.pinned,
      confidence: source.confidence,
      metadata: { ...source.metadata, origin: "promoted" },
      source_run_id: input.runId ?? null,
      source_thread_id: input.threadId ?? null,
      user_id: null,
      origin_id: originId,
      promoted_from_id: source.id,
      promoted_by: input.promotedBy,
      promoted_at: now,
      integrity_hmac: memoryIntegrityHmac({
        orgId: input.orgId,
        userId: null,
        kind: source.kind,
        scope: source.scope,
        scopeId: source.scopeId,
        text: source.text,
      }),
      embedding: null,
      created_at: now,
      updated_at: now,
    })
    .returning();
  await db()
    .update(work_memory)
    .set({ archived_at: now, updated_at: now })
    .where(and(eq(work_memory.org_id, input.orgId), eq(work_memory.id, source.id)));
  const memory = rowToMemory(rows[0]);
  await insertWorkMemoryEvent({
    orgId: input.orgId,
    memoryId: memory.id,
    runId: input.runId ?? null,
    threadId: input.threadId ?? null,
    action: "promote",
    payload: {
      promotedFromId: source.id,
      promotedBy: input.promotedBy,
      originId,
    },
  });
  // CV4: promote is an org-layer version bump — snapshot main and leave
  // the attribution in config_change (the git commit stays anonymous).
  try {
    const { getOrgAgentRoot } = await import("./workspace");
    const { snapshotDurableMemories } = await import("../config-vcs/snapshot");
    const { insertConfigChangeRow } = await import("../config-vcs");
    await snapshotDurableMemories(input.orgId, getOrgAgentRoot(input.orgId));
    await insertConfigChangeRow({
      orgId: input.orgId,
      artifactKind: "memory",
      artifactRef: memory.id,
      actorUserId: input.promotedBy,
      summary: "Promoted a personal memory to the team",
      scope: "team",
      userId: source.userId,
      status: "promoted",
    });
  } catch (err) {
    console.warn(
      `[work-memory] promote snapshot failed (promote persisted): ${err instanceof Error ? err.message : err}`,
    );
  }
  return memory;
}

async function ensureMemoryFork(orgId: string, userId: string): Promise<void> {
  // baseline_at must come from the same clock that stamps
  // work_memory.updated_at (JS), not the DB's now() — staleness checks
  // compare the two directly.
  await db()
    .insert(memory_fork)
    .values({ org_id: orgId, user_id: userId, baseline_at: new Date() })
    .onConflictDoNothing();
}

export type MemoryPullUpdate = {
  originId: string;
  /** The member's row that shadows/suppresses this origin. */
  override: WorkMemory;
  /** Current team version, or null when the team archived it. */
  teamMemory: WorkMemory | null;
  teamRemoved: boolean;
};

/**
 * CV4 pull, step 1 — "Update my context with the team's latest". Lists
 * the member's overridden origins whose team version changed since
 * their fork baseline (net-new team memories already flow in live, so
 * only overrides can go stale).
 */
export async function listMemoryPullUpdates(
  orgId: string,
  userId: string,
): Promise<MemoryPullUpdate[]> {
  const [fork] = await db()
    .select()
    .from(memory_fork)
    .where(and(eq(memory_fork.org_id, orgId), eq(memory_fork.user_id, userId)))
    .limit(1);
  const baselineAt = fork?.baseline_at ?? new Date(0);

  const overrides = await db()
    .select()
    .from(work_memory)
    .where(
      and(
        eq(work_memory.org_id, orgId),
        eq(work_memory.user_id, userId),
        sql`${work_memory.overrides_origin_id} IS NOT NULL`,
        isNull(work_memory.archived_at),
      ),
    );
  const updates: MemoryPullUpdate[] = [];
  for (const row of overrides) {
    const override = rowToMemory(row);
    if (!override.overridesOriginId) continue;
    const [teamRow] = await db()
      .select()
      .from(work_memory)
      .where(
        and(
          eq(work_memory.org_id, orgId),
          eq(work_memory.origin_id, override.overridesOriginId),
          isNull(work_memory.user_id),
          isNull(work_memory.archived_at),
        ),
      )
      .limit(1);
    if (!teamRow) {
      updates.push({
        originId: override.overridesOriginId,
        override,
        teamMemory: null,
        teamRemoved: true,
      });
      continue;
    }
    if (teamRow.updated_at > baselineAt) {
      updates.push({
        originId: override.overridesOriginId,
        override,
        teamMemory: rowToMemory(teamRow),
        teamRemoved: false,
      });
    }
  }
  return updates;
}

/**
 * CV4 pull, step 2 — apply the member's choices and advance their fork
 * baseline. take_theirs archives the member's override so the live team
 * version shows again; keep_mine leaves it shadowing.
 */
export async function applyMemoryPull(input: {
  orgId: string;
  userId: string;
  decisions: Array<{ originId: string; choice: "take_theirs" | "keep_mine" }>;
}): Promise<{ applied: number }> {
  const now = new Date();
  let applied = 0;
  for (const decision of input.decisions) {
    if (decision.choice !== "take_theirs") continue;
    const rows = await db()
      .update(work_memory)
      .set({ archived_at: now, updated_at: now })
      .where(
        and(
          eq(work_memory.org_id, input.orgId),
          eq(work_memory.user_id, input.userId),
          eq(work_memory.overrides_origin_id, decision.originId),
          isNull(work_memory.archived_at),
        ),
      )
      .returning({ id: work_memory.id });
    for (const row of rows) {
      applied += 1;
      await insertWorkMemoryEvent({
        orgId: input.orgId,
        memoryId: row.id,
        runId: null,
        threadId: null,
        action: "pull_take_theirs",
        payload: { originId: decision.originId, userId: input.userId },
      });
    }
  }
  const [teamRef] = await db()
    .select({ sha: config_ref.commit_sha })
    .from(config_ref)
    .where(and(eq(config_ref.org_id, input.orgId), eq(config_ref.scope, "team")))
    .limit(1);
  await db()
    .insert(memory_fork)
    .values({
      org_id: input.orgId,
      user_id: input.userId,
      baseline_sha: teamRef?.sha ?? "",
      baseline_at: now,
    })
    .onConflictDoUpdate({
      target: [memory_fork.org_id, memory_fork.user_id],
      set: {
        baseline_sha: teamRef?.sha ?? "",
        baseline_at: now,
        updated_at: now,
      },
    });
  return { applied };
}

/**
 * SEC6 TTL sweep (DATA_LIFECYCLE §4): archive memories past their
 * expires_at and expire stale proposed pending-memories. Called from
 * the worker's nightly sweep; cheap when nothing qualifies.
 */
export async function sweepExpiredWorkMemories(): Promise<{
  archived: number;
  expiredPending: number;
}> {
  const now = new Date();
  const archivedRows = await db()
    .update(work_memory)
    .set({ archived_at: now, updated_at: now })
    .where(
      and(
        sql`${work_memory.expires_at} is not null`,
        sql`${work_memory.expires_at} <= now()`,
        isNull(work_memory.archived_at),
      ),
    )
    .returning({ id: work_memory.id, org_id: work_memory.org_id });
  for (const row of archivedRows) {
    await insertWorkMemoryEvent({
      orgId: row.org_id,
      memoryId: row.id,
      runId: null,
      threadId: null,
      action: "expired",
      payload: {},
    });
  }
  const cutoff = new Date(
    now.getTime() - PENDING_MEMORY_TTL_DAYS * 24 * 60 * 60 * 1000,
  );
  const expiredPending = await db()
    .update(work_pending_memory)
    .set({
      status: "declined",
      decision_text: "expired (unreviewed past TTL)",
      decided_at: now,
      updated_at: now,
    })
    .where(
      and(
        eq(work_pending_memory.status, "proposed"),
        sql`${work_pending_memory.created_at} <= ${cutoff}`,
      ),
    )
    .returning({ id: work_pending_memory.id });
  return { archived: archivedRows.length, expiredPending: expiredPending.length };
}

export async function createPendingWorkMemory(input: {
  orgId: string;
  threadId?: string | null;
  runId?: string | null;
  draftText: string;
  draftKind: WorkMemoryKind;
  draftScope: WorkMemoryScope;
  draftScopeId?: string | null;
  confidence: number;
  reasoning?: string | null;
  conflicts?: WorkPendingMemoryConflict[];
}): Promise<WorkPendingMemory> {
  assertKind(input.draftKind);
  assertScope(input.draftScope);
  const draftScope = normalizeNewWorkMemoryScope(input.draftScope, {
    threadId: input.threadId ?? null,
  });
  const draftScopeId =
    draftScope === "thread" ? input.threadId ?? input.draftScopeId ?? null : null;
  const rows = await db()
    .insert(work_pending_memory)
    .values({
      org_id: input.orgId,
      thread_id: input.threadId ?? null,
      run_id: input.runId ?? null,
      draft_text: input.draftText.trim(),
      draft_kind: input.draftKind,
      draft_scope: draftScope,
      draft_scope_id: draftScopeId,
      confidence: clamp(input.confidence, 0, 1),
      reasoning: input.reasoning ?? null,
      conflict: input.conflicts && input.conflicts.length > 0 ? input.conflicts : null,
    })
    .returning();
  return rowToPending(rows[0]);
}

export async function listPendingWorkMemories(input: {
  orgId: string;
  threadId?: string | null;
  runId?: string | null;
  status?: WorkPendingMemoryStatus;
  limit?: number;
}): Promise<WorkPendingMemory[]> {
  const status = input.status ?? "proposed";
  const base = and(eq(work_pending_memory.org_id, input.orgId), eq(work_pending_memory.status, status));
  const where = input.runId
    ? and(base, eq(work_pending_memory.run_id, input.runId))
    : input.threadId
      ? and(base, eq(work_pending_memory.thread_id, input.threadId))
      : base;
  const rows = await db()
    .select()
    .from(work_pending_memory)
    .where(where)
    .orderBy(asc(work_pending_memory.created_at))
    .limit(input.limit ?? 200);
  return rows.map(rowToPending);
}

export async function acceptPendingWorkMemory(input: {
  orgId: string;
  id: string;
  text?: string;
  scope?: WorkMemoryScope;
  scopeId?: string | null;
  pinned?: boolean;
}): Promise<{ pending: WorkPendingMemory; memory: WorkMemory }> {
  const pending = await getPendingWorkMemory(input.orgId, input.id);
  if (!pending) throw new Error(`Pending memory not found: ${input.id}`);
  if (pending.status !== "proposed") {
    throw new Error(`Pending memory ${input.id} already ${pending.status}`);
  }
  const scope = input.scope ?? pending.draftScope;
  const normalizedScope = normalizeNewWorkMemoryScope(scope, {
    threadId: pending.threadId,
  });
  const memory = await rememberWorkMemory({
    orgId: input.orgId,
    threadId: pending.threadId,
    runId: pending.runId,
    text: input.text?.trim() || pending.draftText,
    kind: pending.draftKind,
    scope: normalizedScope,
    scopeId: input.scopeId ?? pending.draftScopeId,
    pinned: input.pinned,
    confidence: pending.confidence,
    metadata: { origin: "auto_memory_accepted", pendingId: pending.id },
  });
  const now = new Date();
  await db()
    .update(work_pending_memory)
    .set({
      status: "accepted",
      decision_text: input.text?.trim() ?? null,
      decided_at: now,
      memory_id: memory.id,
      updated_at: now,
    })
    .where(and(eq(work_pending_memory.org_id, input.orgId), eq(work_pending_memory.id, input.id)));
  return { pending: (await getPendingWorkMemory(input.orgId, input.id))!, memory };
}

export async function declinePendingWorkMemory(
  orgId: string,
  id: string,
  reason?: string,
): Promise<WorkPendingMemory> {
  const pending = await getPendingWorkMemory(orgId, id);
  if (!pending) throw new Error(`Pending memory not found: ${id}`);
  if (pending.status !== "proposed") {
    throw new Error(`Pending memory ${id} already ${pending.status}`);
  }
  const now = new Date();
  await db()
    .update(work_pending_memory)
    .set({
      status: "declined",
      decision_text: reason ?? null,
      decided_at: now,
      updated_at: now,
    })
    .where(and(eq(work_pending_memory.org_id, orgId), eq(work_pending_memory.id, id)));
  return (await getPendingWorkMemory(orgId, id))!;
}

export async function getPendingWorkMemory(
  orgId: string,
  id: string,
): Promise<WorkPendingMemory | null> {
  const rows = await db()
    .select()
    .from(work_pending_memory)
    .where(and(eq(work_pending_memory.org_id, orgId), eq(work_pending_memory.id, id)))
    .limit(1);
  return rows[0] ? rowToPending(rows[0]) : null;
}

async function searchSavedWorkMemories(
  input: SearchWorkMemoryInput,
  tokens: string[],
  limit: number,
): Promise<WorkMemorySearchResult[]> {
  const rows = await db()
    .select()
    .from(work_memory)
    .where(
      and(
        eq(work_memory.org_id, input.orgId),
        layerVisibilityFilter(input.userId),
        isNull(work_memory.archived_at),
      ),
    )
    .orderBy(desc(work_memory.pinned), desc(work_memory.updated_at))
    .limit(1000);
  const query = input.query.trim();
  return dropTampered(rows.map(rowToMemory))
    .map((memory) => ({ source: "saved_memory" as const, memory, score: scoreMemory(memory, query, tokens, input) }))
    .filter((result) => result.score > 0 || tokens.length === 0)
    .sort((a, b) => b.score - a.score || b.memory.updatedAt.localeCompare(a.memory.updatedAt))
    .slice(0, limit);
}

async function searchWorkArchiveMessages(
  input: SearchWorkMemoryInput,
  tokens: string[],
  limit: number,
): Promise<WorkArchiveSearchResult[]> {
  if (tokens.length === 0) return [];
  const rows = await db()
    .select({
      runId: work_message.run_id,
      threadId: work_message.thread_id,
      role: work_message.role,
      content: work_message.content,
      createdAt: work_message.created_at,
    })
    .from(work_message)
    .where(
      input.runId
        ? and(eq(work_message.org_id, input.orgId), ne(work_message.run_id, input.runId))
        : eq(work_message.org_id, input.orgId),
    )
    .orderBy(desc(work_message.created_at))
    .limit(500);

  return rows
    .map((row) => {
      const text = row.content.trim();
      if (!text) return null;
      const score = scoreArchive(text, row, input.query, tokens, input);
      if (score <= 0) return null;
      return {
        source: row.runId ? "run_archive" : "thread_archive",
        runId: row.runId,
        threadId: row.threadId,
        role: row.role as "user" | "assistant",
        text: truncate(text, 1200),
        ts: row.createdAt.toISOString(),
        score,
      } satisfies WorkArchiveSearchResult;
    })
    .filter((result): result is WorkArchiveSearchResult => result !== null)
    .sort((a, b) => b.score - a.score || b.ts.localeCompare(a.ts))
    .slice(0, limit);
}

async function touchWorkMemories(ctx: WorkMemoryContext, ids: string[]): Promise<void> {
  const unique = Array.from(new Set(ids));
  if (unique.length === 0) return;
  const now = new Date();
  for (const id of unique) {
    await db()
      .update(work_memory)
      .set({
        use_count: sql`${work_memory.use_count} + 1`,
        last_used_at: now,
      })
      .where(and(eq(work_memory.org_id, ctx.orgId), eq(work_memory.id, id)));
    await insertWorkMemoryEvent({
      orgId: ctx.orgId,
      memoryId: id,
      runId: ctx.runId ?? null,
      threadId: ctx.threadId ?? null,
      action: "search_hit",
      payload: {},
    });
  }
}

async function insertWorkMemoryEvent(input: {
  orgId: string;
  memoryId: string | null;
  runId: string | null;
  threadId: string | null;
  action: string;
  payload: Record<string, unknown>;
}): Promise<void> {
  await db().insert(work_memory_event).values({
    org_id: input.orgId,
    memory_id: input.memoryId,
    run_id: input.runId,
    thread_id: input.threadId,
    action: input.action,
    payload: input.payload,
  });
}

function rowToMemory(row: WorkMemoryRow): WorkMemory {
  // SEC6: verify on read. Pre-SEC6 rows (null hash) and key-less
  // deployments stay trusted; only an actual mismatch flags.
  const expected = row.integrity_hmac
    ? memoryIntegrityHmac({
        orgId: row.org_id,
        userId: row.user_id,
        kind: row.kind,
        scope: row.scope,
        scopeId: row.scope_id,
        text: row.text,
      })
    : null;
  const integrityOk = !row.integrity_hmac || !expected || expected === row.integrity_hmac;
  return {
    integrityOk,
    expiresAt: row.expires_at ? row.expires_at.toISOString() : null,
    id: row.id,
    orgId: row.org_id,
    kind: row.kind as WorkMemoryKind,
    scope: row.scope as WorkMemoryScope,
    scopeId: row.scope_id,
    text: row.text,
    pinned: row.pinned,
    confidence: row.confidence,
    metadata: asRecord(row.metadata),
    sourceRunId: row.source_run_id,
    sourceThreadId: row.source_thread_id,
    useCount: row.use_count,
    lastUsedAt: row.last_used_at ? row.last_used_at.toISOString() : null,
    userId: row.user_id,
    originId: row.origin_id,
    overridesOriginId: row.overrides_origin_id,
    suppressed: row.suppressed,
    promotedFromId: row.promoted_from_id,
    promotedBy: row.promoted_by,
    promotedAt: row.promoted_at ? row.promoted_at.toISOString() : null,
    archivedAt: row.archived_at ? row.archived_at.toISOString() : null,
    createdAt: row.created_at.toISOString(),
    updatedAt: row.updated_at.toISOString(),
  };
}

function rowToPending(row: WorkPendingMemoryRow): WorkPendingMemory {
  return {
    id: row.id,
    orgId: row.org_id,
    threadId: row.thread_id,
    runId: row.run_id,
    status: row.status as WorkPendingMemoryStatus,
    draftText: row.draft_text,
    draftKind: row.draft_kind as WorkMemoryKind,
    draftScope: row.draft_scope as WorkMemoryScope,
    draftScopeId: row.draft_scope_id,
    confidence: row.confidence,
    reasoning: row.reasoning,
    conflicts: Array.isArray(row.conflict) ? (row.conflict as WorkPendingMemoryConflict[]) : [],
    decisionText: row.decision_text,
    decidedAt: row.decided_at ? row.decided_at.toISOString() : null,
    memoryId: row.memory_id,
    createdAt: row.created_at.toISOString(),
    updatedAt: row.updated_at.toISOString(),
  };
}

function scoreMemory(
  memory: WorkMemory,
  query: string,
  tokens: string[],
  ctx: WorkMemoryContext,
): number {
  let score = scoreText(memory.text, query, tokens);
  score += scoreText(`${memory.kind} ${memory.scope} ${memory.scopeId ?? ""}`, query, tokens) * 0.4;
  if (memory.pinned) score += 3;
  if (memory.scope === "global") score += 1;
  if (memory.scope === "thread" && memory.scopeId === ctx.threadId) score += 3;
  return score;
}

function scoreArchive(
  text: string,
  row: { threadId: string; role: string },
  query: string,
  tokens: string[],
  ctx: WorkMemoryContext,
): number {
  let score = scoreText(text, query, tokens);
  if (row.threadId === ctx.threadId) score += 2;
  if (row.role === "user") score += 1;
  return score;
}

function scoreText(text: string, query: string, tokens: string[]): number {
  const lower = text.toLowerCase();
  const phrase = query.trim().toLowerCase();
  let score = phrase && lower.includes(phrase) ? 8 : 0;
  for (const token of tokens) {
    if (lower.includes(token)) score += 2;
  }
  return score;
}

function formatMemoryLabel(memory: WorkMemory): string {
  const scope = memory.scopeId ? `${memory.scope}:${memory.scopeId}` : memory.scope;
  return `${memory.kind} (${scope})`;
}

function resolveScopeId(scope: "global" | "thread", input: WorkMemoryContext & { scopeId?: string | null }): string | null {
  if (scope === "global") return null;
  if (input.scopeId) return input.scopeId;
  if (scope === "thread") return input.threadId ?? null;
  return null;
}

function shouldPinByDefault(kind: WorkMemoryKind, scope: WorkMemoryScope): boolean {
  if (scope === "thread") return false;
  return kind === "preference" || kind === "business_rule" || kind === "metric_definition";
}

function tokenize(query: string): string[] {
  const seen = new Set<string>();
  const raw = query.toLowerCase().match(/[a-z0-9][a-z0-9_-]+/g) ?? [];
  for (const token of raw) {
    if (token.length >= 2) seen.add(token);
    if (seen.size >= 12) break;
  }
  return Array.from(seen);
}

function jaccard(a: Set<string>, b: Set<string>): number {
  if (a.size === 0 && b.size === 0) return 0;
  let intersection = 0;
  for (const token of a) if (b.has(token)) intersection += 1;
  const union = a.size + b.size - intersection;
  return union === 0 ? 0 : intersection / union;
}

function truncate(text: string, max: number): string {
  return text.length <= max ? text : `${text.slice(0, max - 3)}...`;
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function assertKind(kind: string): asserts kind is WorkMemoryKind {
  if (!WORK_MEMORY_KINDS.includes(kind as WorkMemoryKind)) {
    throw new Error(`Invalid work memory kind: ${kind}`);
  }
}

function assertScope(scope: string): asserts scope is WorkMemoryScope {
  if (!WORK_MEMORY_SCOPES.includes(scope as WorkMemoryScope)) {
    throw new Error(`Invalid work memory scope: ${scope}`);
  }
}

function clamp(n: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, n));
}
