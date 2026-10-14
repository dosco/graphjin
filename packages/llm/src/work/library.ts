// Document library domain: uploaded files tracked as library_document
// rows, distilled into library_concept rows (the OKF layer), searched by
// the agent, and shared/approved into the team layer. Mirrors memory.ts
// conventions: db() per query, as-const enums, rowToX mappers, embeddings
// that never fail the write. Layering rule (same as work_memory): NULL
// user_id = team layer, visible org-wide; non-NULL = the owner's
// personal layer, visible only to them.

import {
  and,
  asc,
  db,
  desc,
  eq,
  inArray,
  isNull,
  lt,
  library_concept,
  library_document,
  library_event,
  or,
  sql,
  work_run,
  work_thread,
} from "@neko/db";
import { embedText, vectorLiteral } from "../embedding";
import type { HeldItems } from "@neko/db";
import { entitlementActorForRun, libraryAccessFor } from "./entitlement-scope";
import type { OkfActorStamp, OkfSource } from "../library/okf";

export const LIBRARY_DOCUMENT_STATUSES = [
  "uploaded",
  "extracting",
  "extracted",
  "distilling",
  "cataloged",
  "skipped",
  "failed",
] as const;
export type LibraryDocumentStatus = (typeof LIBRARY_DOCUMENT_STATUSES)[number];

export const LIBRARY_CONCEPT_STATUSES = ["draft", "stable", "deprecated"] as const;
export type LibraryConceptStatus = (typeof LIBRARY_CONCEPT_STATUSES)[number];

export type LibraryDocument = {
  id: string;
  orgId: string;
  userId: string | null;
  sourceThreadId: string | null;
  filename: string;
  relativePath: string;
  contentHash: string;
  sizeBytes: number;
  status: LibraryDocumentStatus;
  skipReason: string | null;
  error: string | null;
  extractCheckpoint: Record<string, unknown> | null;
  extractedRelativePath: string | null;
  extractedContentHash: string | null;
  extractorFingerprint: string | null;
  extractedAt: string | null;
  distilledAt: string | null;
  createdAt: string;
  updatedAt: string;
};

export type LibraryConcept = {
  id: string;
  orgId: string;
  userId: string | null;
  path: string;
  type: string;
  title: string;
  description: string | null;
  tags: string[];
  body: string;
  status: LibraryConceptStatus;
  sources: OkfSource[];
  generatedBy: string | null;
  generatedAt: string | null;
  verified: OkfActorStamp[];
  staleAfter: string | null;
  sourceDocumentId: string | null;
  promotedFromId: string | null;
  promotedBy: string | null;
  promotedAt: string | null;
  archivedAt: string | null;
  createdAt: string;
  updatedAt: string;
};

type DocumentRow = typeof library_document.$inferSelect;
type ConceptRow = typeof library_concept.$inferSelect;

export type LibraryBrowseOptions = {
  view: "documents" | "concepts" | "review";
  query: string;
  layer: "all" | "personal" | "team";
  type: string;
  status: string;
  documentId: string;
  sort: "recent" | "name" | "relevance";
  page: number;
  pageSize: number;
};

/** Held library items; undefined means every team concept. */
export type LibraryAccess = { concepts: HeldItems; collections: HeldItems };
type LibraryReader = { orgId: string; userId: string | null; isAdmin: boolean; access?: LibraryAccess };

function pgTextArray(values: Iterable<string>): string {
  return `{${[...values].map((v) => `"${v.replace(/(["\\])/g, "\\$1")}"`).join(",")}}`;
}

/** Team concepts the reader holds by concept id or by collection path prefix. */
function heldTeamConcepts(access: LibraryAccess | undefined) {
  if (!access || access.concepts === "*" || access.collections === "*") return sql`true`;
  const prefixes = [...access.collections].map((prefix) => `${prefix.replace(/[\\%_]/g, "\\$&")}%`);
  return sql`(${library_concept.id}::text = any(${pgTextArray(access.concepts)}::text[]) or ${library_concept.path} like any(${pgTextArray(prefixes)}::text[]))`;
}

// Browser search intentionally does not depend on embeddings: filenames,
// partial terms and newly distilled concepts must remain discoverable.
export function parseLibraryBrowseOptions(params: URLSearchParams): LibraryBrowseOptions {
  const view = params.get("view") ?? "documents";
  const layer = params.get("layer") ?? "all";
  const query = (params.get("q") ?? "").trim();
  const sort = params.get("sort") ?? (query ? "relevance" : "recent");
  const type = params.get("type") ?? "";
  const status = params.get("status") ?? "";
  const documentId = params.get("documentId") ?? "";
  const page = Number(params.get("page") ?? 1);
  const pageSize = Number(params.get("pageSize") ?? 50);
  if (!["documents", "concepts", "review"].includes(view)
    || !["all", "personal", "team"].includes(layer)
    || !["recent", "name", "relevance"].includes(sort)
    || query.length > 200 || type.length > 100
    || (documentId && !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(documentId))
    || !Number.isSafeInteger(page) || page < 1 || page > 1_000_000
    || !Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100
    || (status && !(view === "documents" ? LIBRARY_DOCUMENT_STATUSES : LIBRARY_CONCEPT_STATUSES).some(value => value === status))) {
    throw new Error("Invalid library filters.");
  }
  return { view, layer, sort, query, type, status, documentId, page, pageSize } as LibraryBrowseOptions;
}

function visibleLibraryConcepts(reader: LibraryReader, review = false, layer = "all") {
  const personal = reader.userId && layer !== "team"
    ? eq(library_concept.user_id, reader.userId) : sql`false`;
  const team = layer !== "personal"
    ? and(isNull(library_concept.user_id), eq(library_concept.status, "stable"), reader.isAdmin ? undefined : heldTeamConcepts(reader.access)) : sql`false`;
  return and(eq(library_concept.org_id, reader.orgId), isNull(library_concept.archived_at),
    review ? (reader.isAdmin ? and(isNull(library_concept.user_id), eq(library_concept.status, "draft")) : sql`false`)
      : or(personal, team));
}

export async function browseLibrary(reader: LibraryReader, options: LibraryBrowseOptions) {
  const documentsVisible = and(eq(library_document.org_id, reader.orgId), userLayerCondition(library_document.user_id, reader.userId));
  const conceptsVisible = visibleLibraryConcepts(reader);
  const reviewVisible = visibleLibraryConcepts(reader, true);
  const count = sql<number>`count(*)::int`;
  const [documentsCount, conceptsCount, reviewCount, types] = await Promise.all([
    db().select({ count }).from(library_document).where(documentsVisible),
    db().select({ count }).from(library_concept).where(conceptsVisible),
    db().select({ count }).from(library_concept).where(reviewVisible),
    db().selectDistinct({ type: library_concept.type }).from(library_concept)
      .where(options.view === "review" ? reviewVisible : visibleLibraryConcepts(reader, false, options.layer))
      .orderBy(asc(library_concept.type)),
  ]);
  const counts = { documents: documentsCount[0].count, concepts: conceptsCount[0].count, review: reviewCount[0].count };
  // Escape LIKE metacharacters; a user's '%' or '_' is text, not a wildcard.
  const words = options.query.split(/\s+/).filter(Boolean).map(word => `%${word.replace(/[\\%_]/g, "\\$&")}%`);
  const isDocuments = options.view === "documents";
  const conceptText = sql`concat_ws(' ', ${library_concept.title}, ${library_concept.description}, ${library_concept.path}, ${library_concept.type}, ${library_concept.tags}::text, ${library_concept.body})`;
  const documentText = sql`concat_ws(' ', ${library_document.filename}, ${library_document.relative_path}, ${library_document.status})`;
  const queryVec = options.query ? await tryEmbed(options.query) : null;
  const conceptKeywords = words.length ? and(...words.map(word => sql`${conceptText} ilike ${word}`))! : sql`false`;
  const documentKeywords = words.length ? and(...words.map(word => sql`${documentText} ilike ${word}`))! : sql`false`;
  const similarity = queryVec ? sql`greatest(coalesce(1 - (${library_concept.embedding} <=> ${queryVec}::vector), 0), 0)` : sql`0`;
  const conceptScore = sql`(case when ${conceptKeywords} then 1 else 0 end + ${similarity})`;
  // Documents reuse the concepts distilled from them; no second embedding store.
  const score = isDocuments
    ? sql`(case when ${documentKeywords} then 1 else 0 end + coalesce((select max(${conceptScore}) from ${library_concept}
        where ${visibleLibraryConcepts(reader)} and ${library_concept.source_document_id} = ${library_document.id}), 0))`
    : conceptScore;
  const where = and(
    isDocuments ? documentsVisible : visibleLibraryConcepts(reader, options.view === "review", options.layer),
    // Same cosine relevance floor as memory similarity search; exact text wins
    // even without an embedding. Unrelated vectors must not fill every page.
    options.query ? sql`${score} >= 0.3` : undefined,
    options.status ? eq(isDocuments ? library_document.status : library_concept.status, options.status) : undefined,
    !isDocuments && options.type ? eq(library_concept.type, options.type) : undefined,
    !isDocuments && options.documentId ? eq(library_concept.source_document_id, options.documentId) : undefined,
  );
  const totalRows = isDocuments
    ? await db().select({ count }).from(library_document).where(where)
    : await db().select({ count }).from(library_concept).where(where);
  const total = totalRows[0].count;
  const page = Math.min(options.page, Math.max(1, Math.ceil(total / options.pageSize)));
  const offset = (page - 1) * options.pageSize;
  const documents = isDocuments ? await db().select({
    id: library_document.id, filename: library_document.filename, relativePath: library_document.relative_path,
    sizeBytes: library_document.size_bytes, status: library_document.status, createdAt: library_document.created_at,
    skipReason: library_document.skip_reason,
  }).from(library_document).where(where)
    .orderBy(options.sort === "name" ? asc(library_document.filename) : options.sort === "relevance" && options.query ? desc(score) : desc(library_document.created_at), asc(library_document.id))
    .limit(options.pageSize).offset(offset) : [];
  const concepts = !isDocuments ? await db().select({
    id: library_concept.id, title: library_concept.title, description: library_concept.description,
    path: library_concept.path, type: library_concept.type, status: library_concept.status,
    updatedAt: library_concept.updated_at, userId: library_concept.user_id,
  }).from(library_concept).where(where)
    .orderBy(options.sort === "name" ? asc(library_concept.title) : options.sort === "relevance" && options.query ? desc(score) : desc(library_concept.updated_at), asc(library_concept.id))
    .limit(options.pageSize).offset(offset) : [];
  return { isAdmin: reader.isAdmin, counts, total, page, pageSize: options.pageSize,
    searchMode: options.query ? queryVec ? "hybrid" as const : "keyword" as const : "browse" as const,
    types: types.map(row => row.type),
    documents: documents.map(row => ({ ...row, createdAt: row.createdAt.toISOString() })),
    concepts: concepts.map(({ userId, ...row }) => ({ ...row, updatedAt: row.updatedAt.toISOString(), layer: userId === null ? "team" as const : "personal" as const })),
  };
}

/** Detail uses the same visibility as browsing, including admin-only review. */
export async function readLibraryConcept(reader: LibraryReader, id: string) {
  const rows = await db().select().from(library_concept).where(and(
    eq(library_concept.id, id), or(visibleLibraryConcepts(reader), visibleLibraryConcepts(reader, true)),
  )).limit(1);
  return rows[0] ? rowToConcept(rows[0]) : null;
}

export async function editLibraryConcept(reader: LibraryReader, input: {
  id: string; title: string; description: string; type: string; body: string; updatedAt: string;
}) {
  const concept = await readLibraryConcept(reader, input.id);
  const writable = concept && (concept.userId !== null ? concept.userId === reader.userId : reader.isAdmin);
  if (!writable) return { status: "not_found" as const };
  if (concept.updatedAt !== input.updatedAt) return { status: "conflict" as const };
  const now = new Date(Math.max(Date.now(), Date.parse(concept.updatedAt) + 1));
  return db().transaction(async tx => {
    const rows = await tx.update(library_concept).set({
      title: input.title, description: input.description || null, type: input.type, body: input.body,
      // A null vector atomically queues this revision for background indexing.
      embedding: null,
      verified: concept.userId === null && concept.status === "stable"
        ? [{ by: `human:${reader.userId ?? "admin"}`, at: now.toISOString() }] : [],
      updated_at: now,
    }).where(and(eq(library_concept.org_id, reader.orgId), eq(library_concept.id, input.id),
      isNull(library_concept.archived_at),
      // JS dates expose millisecond precision; Postgres defaults can include microseconds.
      sql`date_trunc('milliseconds', ${library_concept.updated_at}) = ${input.updatedAt}::timestamptz`,
      concept.userId !== null ? eq(library_concept.user_id, reader.userId!) : isNull(library_concept.user_id),
    )).returning();
    if (!rows[0]) return { status: "conflict" as const };
    await tx.insert(library_event).values({
      org_id: reader.orgId, concept_id: input.id, user_id: reader.userId, action: "concept_edited",
      payload: { path: concept.path, previous: { title: concept.title, description: concept.description, type: concept.type, body: concept.body, verified: concept.verified } },
    });
    return { status: "saved" as const, concept: rowToConcept(rows[0]), searchIndexed: false };
  });
}

export async function createLibraryDocument(input: {
  orgId: string;
  userId: string | null;
  sourceThreadId?: string | null;
  filename: string;
  relativePath: string;
  contentHash: string;
  sizeBytes: number;
}): Promise<{ document: LibraryDocument; created: boolean }> {
  // Idempotent on content: re-uploading the identical file returns the
  // existing row instead of queuing a second distillation.
  const existing = await db()
    .select()
    .from(library_document)
    .where(
      and(
        eq(library_document.org_id, input.orgId),
        userLayerCondition(library_document.user_id, input.userId),
        eq(library_document.content_hash, input.contentHash),
      ),
    )
    .limit(1);
  if (existing[0]) {
    return { document: rowToDocument(existing[0]), created: false };
  }
  const rows = await db()
    .insert(library_document)
    .values({
      org_id: input.orgId,
      user_id: input.userId,
      source_thread_id: input.sourceThreadId ?? null,
      filename: input.filename,
      relative_path: input.relativePath,
      content_hash: input.contentHash,
      size_bytes: input.sizeBytes,
    })
    .returning();
  await insertLibraryEvent({
    orgId: input.orgId,
    documentId: rows[0].id,
    userId: input.userId,
    action: "document_created",
    payload: { filename: input.filename },
  });
  return { document: rowToDocument(rows[0]), created: true };
}

export async function getLibraryDocument(
  orgId: string,
  id: string,
): Promise<LibraryDocument | null> {
  const rows = await db()
    .select()
    .from(library_document)
    .where(and(eq(library_document.org_id, orgId), eq(library_document.id, id)))
    .limit(1);
  return rows[0] ? rowToDocument(rows[0]) : null;
}

export async function listLibraryDocuments(input: {
  orgId: string;
  userId: string | null;
  limit?: number;
}): Promise<LibraryDocument[]> {
  const rows = await db()
    .select()
    .from(library_document)
    .where(
      and(
        eq(library_document.org_id, input.orgId),
        userLayerCondition(library_document.user_id, input.userId),
      ),
    )
    .orderBy(desc(library_document.created_at))
    .limit(clampLimit(input.limit ?? 100, 200));
  return rows.map(rowToDocument);
}

/**
 * Safety net for the database-insert → queue-send boundary in HTTP uploads.
 * A row that is still `uploaded` after the grace period has no worker-owned
 * progress and can be enqueued again safely.
 */
export async function listStaleUploadedLibraryDocuments(
  before: Date,
): Promise<Array<{ orgId: string; documentId: string }>> {
  return db()
    .select({
      orgId: library_document.org_id,
      documentId: library_document.id,
    })
    .from(library_document)
    .where(
      and(
        eq(library_document.status, "uploaded"),
        lt(library_document.updated_at, before),
      ),
    );
}

export async function markLibraryDocumentStatus(input: {
  orgId: string;
  id: string;
  status: LibraryDocumentStatus;
  skipReason?: string | null;
  error?: string | null;
}): Promise<void> {
  const now = new Date();
  await db()
    .update(library_document)
    .set({
      status: input.status,
      skip_reason: input.skipReason ?? null,
      error: input.error ?? null,
      distilled_at:
        input.status === "cataloged" || input.status === "skipped" ? now : null,
      updated_at: now,
    })
    .where(
      and(eq(library_document.org_id, input.orgId), eq(library_document.id, input.id)),
    );
  if (
    input.status !== "distilling" &&
    input.status !== "extracting" &&
    input.status !== "extracted" &&
    input.status !== "uploaded"
  ) {
    await insertLibraryEvent({
      orgId: input.orgId,
      documentId: input.id,
      action: `document_${input.status}`,
      payload: {
        ...(input.skipReason ? { skipReason: input.skipReason } : {}),
        ...(input.error ? { error: input.error } : {}),
      },
    });
  }
}

/** Small durable state for one in-flight asynchronous extraction task. */
export async function getLibraryExtractCheckpoint(
  orgId: string,
  id: string,
): Promise<Record<string, unknown> | null> {
  const rows = await db()
    .select({ checkpoint: library_document.extract_checkpoint })
    .from(library_document)
    .where(and(eq(library_document.org_id, orgId), eq(library_document.id, id)))
    .limit(1);
  const checkpoint = rows[0]?.checkpoint;
  return checkpoint && typeof checkpoint === "object"
    ? (checkpoint as Record<string, unknown>)
    : null;
}

export async function saveLibraryExtractCheckpoint(
  orgId: string,
  id: string,
  checkpoint: Record<string, unknown>,
): Promise<void> {
  await db()
    .update(library_document)
    .set({
      status: "extracting",
      extract_checkpoint: checkpoint,
      error: null,
      updated_at: new Date(),
    })
    .where(and(eq(library_document.org_id, orgId), eq(library_document.id, id)));
}

/** Commit a normalized Markdown artifact before queueing distillation. */
export async function completeLibraryExtraction(input: {
  orgId: string;
  id: string;
  relativePath: string;
  contentHash: string;
  extractorFingerprint: string;
}): Promise<void> {
  const now = new Date();
  await db()
    .update(library_document)
    .set({
      status: "extracted",
      error: null,
      extract_checkpoint: null,
      extracted_relative_path: input.relativePath,
      extracted_content_hash: input.contentHash,
      extractor_fingerprint: input.extractorFingerprint,
      extracted_at: now,
      updated_at: now,
    })
    .where(
      and(eq(library_document.org_id, input.orgId), eq(library_document.id, input.id)),
    );
}

/**
 * Clear task/cursor state and the derived-artifact pointer after the caller has
 * removed the temporary file. Compact provenance hashes remain on the row.
 */
export async function clearLibraryTransientState(
  orgId: string,
  id: string,
): Promise<void> {
  await db()
    .update(library_document)
    .set({
      extract_checkpoint: null,
      extracted_relative_path: null,
      distill_checkpoint: null,
      updated_at: new Date(),
    })
    .where(and(eq(library_document.org_id, orgId), eq(library_document.id, id)));
}

export type LibraryTransientCleanupCandidate = {
  orgId: string;
  documentId: string;
  extractedRelativePath: string | null;
};

/**
 * Completed rows are eligible immediately; failed processing state is kept
 * for seven days before becoming eligible. Retaining the artifact pointer
 * until deletion lets the boot sweep finish cleanup after a process crash.
 */
export async function listLibraryTransientCleanupCandidates(
  before: Date,
): Promise<LibraryTransientCleanupCandidate[]> {
  return db()
    .select({
      orgId: library_document.org_id,
      documentId: library_document.id,
      extractedRelativePath: library_document.extracted_relative_path,
    })
    .from(library_document)
    .where(
      and(
        sql`(${library_document.extract_checkpoint} IS NOT NULL OR ${library_document.distill_checkpoint} IS NOT NULL OR ${library_document.extracted_relative_path} IS NOT NULL)`,
        or(
          and(
            eq(library_document.status, "failed"),
            lt(library_document.updated_at, before),
          ),
          inArray(library_document.status, ["cataloged", "skipped"]),
        ),
      ),
    );
}

export async function clearLibraryTransientCleanupCandidates(
  entries: readonly LibraryTransientCleanupCandidate[],
): Promise<void> {
  const ids = entries.map((entry) => entry.documentId);
  if (ids.length === 0) return;
  await db()
    .update(library_document)
    .set({
      extract_checkpoint: null,
      extracted_relative_path: null,
      distill_checkpoint: null,
      updated_at: new Date(),
    })
    .where(inArray(library_document.id, ids));
}

/**
 * Resumable-distillation checkpoint: an opaque blob the librarian writes after
 * each chunk of a large document and reads back on a retry to resume from the
 * next chunk. Typed by the distiller (packages/llm/src/library/distill.ts);
 * stored here as jsonb.
 */
export async function getLibraryDistillCheckpoint(
  orgId: string,
  id: string,
): Promise<Record<string, unknown> | null> {
  const rows = await db()
    .select({ checkpoint: library_document.distill_checkpoint })
    .from(library_document)
    .where(and(eq(library_document.org_id, orgId), eq(library_document.id, id)))
    .limit(1);
  const cp = rows[0]?.checkpoint;
  return cp && typeof cp === "object" ? (cp as Record<string, unknown>) : null;
}

export async function saveLibraryDistillCheckpoint(
  orgId: string,
  id: string,
  checkpoint: Record<string, unknown>,
): Promise<void> {
  await db()
    .update(library_document)
    .set({ distill_checkpoint: checkpoint, updated_at: new Date() })
    .where(and(eq(library_document.org_id, orgId), eq(library_document.id, id)));
}

export async function clearLibraryDistillCheckpoint(
  orgId: string,
  id: string,
): Promise<void> {
  await db()
    .update(library_document)
    .set({ distill_checkpoint: null, updated_at: new Date() })
    .where(and(eq(library_document.org_id, orgId), eq(library_document.id, id)));
}

/**
 * Insert or revise a concept at (org, layer, path). Updates keep the
 * row's status — a personal draft stays draft; a stable team concept
 * revised through share flows is downgraded explicitly by the caller,
 * never silently here.
 */
export async function upsertLibraryConcept(input: {
  orgId: string;
  userId: string | null;
  path: string;
  type: string;
  title: string;
  description?: string | null;
  tags?: string[];
  body: string;
  sources?: OkfSource[];
  generatedBy?: string | null;
  sourceDocumentId?: string | null;
  status?: LibraryConceptStatus;
  /** YYYY-MM-DD after which the concept is considered stale. */
  staleAfter?: string | null;
  /** Import path only: carry verification stamps from a bundle. */
  verified?: OkfActorStamp[];
}): Promise<{ concept: LibraryConcept; created: boolean }> {
  const now = new Date();
  const existing = await findActiveConceptByPath(input.orgId, input.userId, input.path);
  if (existing) {
    const rows = await db()
      .update(library_concept)
      .set({
        type: input.type,
        title: input.title,
        description: input.description ?? null,
        tags: input.tags ?? [],
        body: input.body,
        sources: mergeSources(existing.sources, input.sources ?? []),
        generated_by: input.generatedBy ?? existing.generatedBy,
        generated_at: now,
        source_document_id: input.sourceDocumentId ?? existing.sourceDocumentId,
        ...(input.status ? { status: input.status } : {}),
        ...(input.staleAfter !== undefined ? { stale_after: input.staleAfter } : {}),
        ...(input.verified !== undefined ? { verified: input.verified } : {}),
        embedding: null,
        updated_at: now,
      })
      .where(
        and(eq(library_concept.org_id, input.orgId), eq(library_concept.id, existing.id)),
      )
      .returning();
    await insertLibraryEvent({
      orgId: input.orgId,
      conceptId: existing.id,
      documentId: input.sourceDocumentId,
      userId: input.userId,
      action: "concept_updated",
      payload: { path: input.path },
    });
    return { concept: rowToConcept(rows[0]), created: false };
  }
  const rows = await db()
    .insert(library_concept)
    .values({
      org_id: input.orgId,
      user_id: input.userId,
      path: input.path,
      type: input.type,
      title: input.title,
      description: input.description ?? null,
      tags: input.tags ?? [],
      body: input.body,
      status: input.status ?? "draft",
      sources: input.sources ?? [],
      generated_by: input.generatedBy ?? null,
      generated_at: now,
      source_document_id: input.sourceDocumentId ?? null,
      stale_after: input.staleAfter ?? null,
      verified: input.verified ?? [],
      embedding: null,
    })
    .returning();
  await insertLibraryEvent({
    orgId: input.orgId,
    conceptId: rows[0].id,
    documentId: input.sourceDocumentId,
    userId: input.userId,
    action: "concept_created",
    payload: { path: input.path },
  });
  return { concept: rowToConcept(rows[0]), created: true };
}

export async function getLibraryConcept(
  orgId: string,
  id: string,
): Promise<LibraryConcept | null> {
  const rows = await db()
    .select()
    .from(library_concept)
    .where(and(eq(library_concept.org_id, orgId), eq(library_concept.id, id)))
    .limit(1);
  return rows[0] ? rowToConcept(rows[0]) : null;
}

/** List one layer: userId null = team, non-null = that member's personal. */
export async function listLibraryConcepts(input: {
  orgId: string;
  userId: string | null;
  status?: LibraryConceptStatus;
  /** null is reserved for complete server-side export/materialization. */
  limit?: number | null;
}): Promise<LibraryConcept[]> {
  const conditions = [
    eq(library_concept.org_id, input.orgId),
    userLayerCondition(library_concept.user_id, input.userId),
    isNull(library_concept.archived_at),
  ];
  if (input.status) conditions.push(eq(library_concept.status, input.status));
  const query = db()
    .select()
    .from(library_concept)
    .where(and(...conditions))
    .orderBy(desc(library_concept.updated_at), asc(library_concept.id));
  const rows = await (input.limit === null ? query : query.limit(clampLimit(input.limit ?? 200, 500)));
  return rows.map(rowToConcept);
}

/** Query words as lowercase letters and digits only, so codes match however the text punctuates them. */
export function libraryQueryTerms(query: string): string[] {
  const terms = query.split(/\s+/).map((word) => word.toLowerCase().replace(/[^a-z0-9]/g, "")).filter((term) => term.length >= 2);
  return [...new Set(terms)].slice(0, 12);
}

export type LibraryConceptSearchResult = {
  concept: LibraryConcept;
  layer: "team" | "personal";
  score: number;
};

/**
 * Semantic search over the layered library: team concepts plus the
 * viewing user's personal concepts. userId comes from the caller's own
 * auth context (web) or is resolved from the run binding (agent path,
 * see searchLibraryForRun) — never from agent-supplied input.
 */
export async function searchLibraryByContext(input: {
  orgId: string;
  userId: string | null;
  query: string;
  limit?: number;
  access?: LibraryAccess;
}): Promise<LibraryConceptSearchResult[]> {
  const queryVec = await tryEmbed(input.query);
  const terms = libraryQueryTerms(input.query);
  if (!queryVec && terms.length === 0) return [];
  const limit = clampLimit(input.limit ?? 5, 20);
  const team = sql`(${library_concept.user_id} IS NULL AND ${heldTeamConcepts(input.access)})`;
  const layerVisible = input.userId
    ? sql`(${team} OR ${library_concept.user_id} = ${input.userId})`
    : team;
  // Codes and model names ("671N", "MT40") match text that spells them with
  // other punctuation ("GIA-671(N)", "I-Tera MT40").
  const squashed = sql`regexp_replace(lower(concat_ws(' ', ${library_concept.title}, ${library_concept.description}, ${library_concept.path}, ${library_concept.body})), '[^a-z0-9]', '', 'g')`;
  const keywordShare = terms.length
    ? sql`((${sql.join(terms.map((term) => sql`(case when ${squashed} like ${`%${term}%`} then 1 else 0 end)`), sql` + `)})::float / ${terms.length})`
    : sql`0`;
  const similarity = queryVec
    ? sql`coalesce(1 - (${library_concept.embedding} <=> ${queryVec}::vector), 0)`
    : sql`0`;
  const score = sql<number>`(${similarity} + ${keywordShare})`;
  const rows = await db()
    .select({ row: library_concept, score })
    .from(library_concept)
    .where(
      and(
        eq(library_concept.org_id, input.orgId),
        layerVisible,
        isNull(library_concept.archived_at),
        queryVec ? sql`${library_concept.embedding} IS NOT NULL` : sql`${keywordShare} > 0`,
        sql`${library_concept.status} <> 'deprecated'`,
      ),
    )
    .orderBy(desc(keywordShare), desc(similarity), asc(library_concept.id))
    .limit(limit);
  return rows.map((r) => ({
    concept: rowToConcept(r.row),
    layer: r.row.user_id === null ? ("team" as const) : ("personal" as const),
    score: r.score,
  }));
}

/**
 * Agent-facing search: the personal layer is the run's thread owner,
 * resolved server-side from the run id (the broker strips any
 * agent-supplied userId before this is called).
 */
export async function searchLibraryForRun(input: {
  orgId: string;
  runId?: string | null;
  query: string;
  limit?: number;
}): Promise<LibraryConceptSearchResult[]> {
  const userId = input.runId
    ? await resolveRunOwnerUserId(input.orgId, input.runId)
    : null;
  const actor = input.runId ? await entitlementActorForRun(input.orgId, input.runId) : null;
  if (input.runId && !actor) return [];
  return searchLibraryByContext({
    orgId: input.orgId,
    userId,
    query: input.query,
    limit: input.limit,
    ...(actor ? { access: await libraryAccessFor(actor) } : {}),
  });
}

async function resolveRunOwnerUserId(
  orgId: string,
  runId: string,
): Promise<string | null> {
  const rows = await db()
    .select({ owner: work_thread.created_by_user_id })
    .from(work_run)
    .innerJoin(work_thread, eq(work_run.thread_id, work_thread.id))
    .where(and(eq(work_run.org_id, orgId), eq(work_run.id, runId)))
    .limit(1);
  return rows[0]?.owner ?? null;
}

/**
 * Share a personal concept into the team layer as a draft awaiting
 * admin approval. If an active team concept already holds the path, it
 * is revised in place and downgraded to draft — re-approval is the
 * gate, matching the promote-with-lineage flow on memories.
 */
export async function shareLibraryConceptToTeam(input: {
  orgId: string;
  id: string;
  sharedBy: string | null;
}): Promise<LibraryConcept> {
  const personal = await getLibraryConcept(input.orgId, input.id);
  if (!personal || personal.archivedAt) {
    throw new Error(`Library concept not found: ${input.id}`);
  }
  if (personal.userId === null) {
    throw new Error("Concept is already in the team layer.");
  }
  const now = new Date();
  const { concept } = await upsertLibraryConcept({
    orgId: input.orgId,
    userId: null,
    path: personal.path,
    type: personal.type,
    title: personal.title,
    description: personal.description,
    tags: personal.tags,
    body: personal.body,
    sources: personal.sources,
    generatedBy: personal.generatedBy,
    sourceDocumentId: personal.sourceDocumentId,
    status: "draft",
  });
  const rows = await db()
    .update(library_concept)
    .set({
      promoted_from_id: personal.id,
      promoted_by: input.sharedBy,
      promoted_at: now,
      updated_at: now,
    })
    .where(and(eq(library_concept.org_id, input.orgId), eq(library_concept.id, concept.id)))
    .returning();
  await insertLibraryEvent({
    orgId: input.orgId,
    conceptId: concept.id,
    userId: input.sharedBy,
    action: "shared",
    payload: { path: personal.path, fromConceptId: personal.id },
  });
  return rowToConcept(rows[0]);
}

/**
 * Admin decision on a team concept. Approve stamps a human verification
 * (OKF actor convention "human:<id>") and flips a draft to stable;
 * decline archives a draft; deprecate retires a stable concept from
 * agent search and the materialized bundle. Idempotency guards mirror
 * acceptPendingWorkMemory.
 */
export async function decideLibraryConcept(input: {
  orgId: string;
  id: string;
  action: "approve" | "decline" | "deprecate";
  decidedBy: string | null;
}): Promise<LibraryConcept> {
  const concept = await getLibraryConcept(input.orgId, input.id);
  if (!concept || concept.archivedAt) {
    throw new Error(`Library concept not found: ${input.id}`);
  }
  if (concept.userId !== null) {
    throw new Error("Only team-layer concepts go through approval.");
  }
  const now = new Date();
  const actor = input.decidedBy ?? "admin";
  const finish = async (
    set: Partial<typeof library_concept.$inferInsert>,
    action: string,
  ): Promise<LibraryConcept> => {
    const rows = await db()
      .update(library_concept)
      .set({ ...set, updated_at: now })
      .where(
        and(eq(library_concept.org_id, input.orgId), eq(library_concept.id, input.id)),
      )
      .returning();
    await insertLibraryEvent({
      orgId: input.orgId,
      conceptId: input.id,
      userId: input.decidedBy,
      action,
      payload: { path: concept.path },
    });
    return rowToConcept(rows[0]);
  };

  if (input.action === "deprecate") {
    if (concept.status !== "stable") {
      throw new Error(`Only stable concepts can be deprecated (is ${concept.status})`);
    }
    return finish({ status: "deprecated" }, "deprecated");
  }
  if (concept.status !== "draft") {
    throw new Error(`Library concept ${input.id} already ${concept.status}`);
  }
  if (input.action === "decline") {
    return finish({ archived_at: now }, "declined");
  }
  const verified: OkfActorStamp[] = [
    ...concept.verified,
    { by: `human:${actor}`, at: now.toISOString() },
  ];
  return finish({ status: "stable", verified }, "approved");
}

/** Owner archives one of their personal concepts (team rows go through decide). */
export async function archiveLibraryConcept(input: {
  orgId: string;
  id: string;
  userId: string | null;
}): Promise<LibraryConcept> {
  const concept = await getLibraryConcept(input.orgId, input.id);
  if (!concept || concept.archivedAt || concept.userId !== input.userId) {
    throw new Error(`Library concept not found: ${input.id}`);
  }
  const now = new Date();
  const rows = await db()
    .update(library_concept)
    .set({ archived_at: now, updated_at: now })
    .where(and(eq(library_concept.org_id, input.orgId), eq(library_concept.id, input.id)))
    .returning();
  await insertLibraryEvent({
    orgId: input.orgId,
    conceptId: input.id,
    userId: input.userId,
    action: "archived",
    payload: { path: concept.path },
  });
  return rowToConcept(rows[0]);
}

/**
 * Owner removes a document from their library: the tracking row is
 * deleted and every same-layer concept sourced from it is archived.
 * The raw file is the caller's to clean up (thread uploads belong to
 * the thread and are left alone; library-direct uploads get deleted).
 */
export async function removeLibraryDocument(input: {
  orgId: string;
  id: string;
  userId: string | null;
}): Promise<LibraryDocument> {
  const document = await getLibraryDocument(input.orgId, input.id);
  if (!document || document.userId !== input.userId) {
    throw new Error(`Library document not found: ${input.id}`);
  }
  const now = new Date();
  await db()
    .update(library_concept)
    .set({ archived_at: now, updated_at: now })
    .where(
      and(
        eq(library_concept.org_id, input.orgId),
        eq(library_concept.source_document_id, input.id),
        userLayerCondition(library_concept.user_id, input.userId),
        isNull(library_concept.archived_at),
      ),
    );
  await db()
    .delete(library_document)
    .where(
      and(eq(library_document.org_id, input.orgId), eq(library_document.id, input.id)),
    );
  await insertLibraryEvent({
    orgId: input.orgId,
    userId: input.userId,
    action: "document_removed",
    payload: { filename: document.filename },
  });
  return document;
}

// Best-effort audit insert, mirroring insertWorkMemoryEvent — a logging
// failure never fails the write it describes.
export async function insertLibraryEvent(input: {
  orgId: string;
  documentId?: string | null;
  conceptId?: string | null;
  userId?: string | null;
  action: string;
  payload?: Record<string, unknown>;
}): Promise<void> {
  try {
    await db().insert(library_event).values({
      org_id: input.orgId,
      document_id: input.documentId ?? null,
      concept_id: input.conceptId ?? null,
      user_id: input.userId ?? null,
      action: input.action,
      payload: input.payload ?? {},
    });
  } catch (err) {
    console.warn(
      `[library] event insert failed (write succeeded): ${err instanceof Error ? err.message : err}`,
    );
  }
}

/**
 * Nightly staleness sweep: stable concepts past their stale_after date
 * become deprecated — still stored and searchable in the UI, but
 * excluded from agent search and the materialized team bundle. Returns
 * the org ids that changed so callers can re-materialize their bundles.
 */
export async function sweepStaleLibraryConcepts(): Promise<{
  deprecated: number;
  orgIds: string[];
}> {
  const rows = await db()
    .update(library_concept)
    .set({ status: "deprecated", updated_at: new Date() })
    .where(
      and(
        eq(library_concept.status, "stable"),
        isNull(library_concept.archived_at),
        sql`${library_concept.stale_after} IS NOT NULL`,
        sql`${library_concept.stale_after} < CURRENT_DATE`,
      ),
    )
    .returning({ id: library_concept.id, org_id: library_concept.org_id });
  for (const row of rows) {
    await insertLibraryEvent({
      orgId: row.org_id,
      conceptId: row.id,
      action: "deprecated",
      payload: { reason: "stale_after elapsed" },
    });
  }
  return {
    deprecated: rows.length,
    orgIds: [...new Set(rows.map((r) => r.org_id))],
  };
}

async function findActiveConceptByPath(
  orgId: string,
  userId: string | null,
  path: string,
): Promise<LibraryConcept | null> {
  const rows = await db()
    .select()
    .from(library_concept)
    .where(
      and(
        eq(library_concept.org_id, orgId),
        userLayerCondition(library_concept.user_id, userId),
        eq(library_concept.path, path),
        isNull(library_concept.archived_at),
      ),
    )
    .limit(1);
  return rows[0] ? rowToConcept(rows[0]) : null;
}

function userLayerCondition(
  column: typeof library_concept.user_id | typeof library_document.user_id,
  userId: string | null,
) {
  return userId === null ? isNull(column) : eq(column, userId);
}

function clampLimit(value: number, max: number): number {
  return Math.max(1, Math.min(max, Math.floor(value)));
}

function mergeSources(existing: OkfSource[], incoming: OkfSource[]): OkfSource[] {
  const byResource = new Map<string, OkfSource>();
  for (const source of [...existing, ...incoming]) {
    if (source?.resource) byResource.set(source.resource, source);
  }
  return Array.from(byResource.values());
}

async function tryEmbed(text: string): Promise<string | null> {
  try {
    return vectorLiteral(await embedText(text));
  } catch (err) {
    console.error(
      "[library] query embedding unavailable:",
      err instanceof Error ? err.message : err,
    );
    return null;
  }
}

function rowToDocument(row: DocumentRow): LibraryDocument {
  return {
    id: row.id,
    orgId: row.org_id,
    userId: row.user_id,
    sourceThreadId: row.source_thread_id,
    filename: row.filename,
    relativePath: row.relative_path,
    contentHash: row.content_hash,
    sizeBytes: row.size_bytes,
    status: row.status as LibraryDocumentStatus,
    skipReason: row.skip_reason,
    error: row.error,
    extractCheckpoint:
      row.extract_checkpoint && typeof row.extract_checkpoint === "object"
        ? (row.extract_checkpoint as Record<string, unknown>)
        : null,
    extractedRelativePath: row.extracted_relative_path,
    extractedContentHash: row.extracted_content_hash,
    extractorFingerprint: row.extractor_fingerprint,
    extractedAt: row.extracted_at ? row.extracted_at.toISOString() : null,
    distilledAt: row.distilled_at ? row.distilled_at.toISOString() : null,
    createdAt: row.created_at.toISOString(),
    updatedAt: row.updated_at.toISOString(),
  };
}

function rowToConcept(row: ConceptRow): LibraryConcept {
  return {
    id: row.id,
    orgId: row.org_id,
    userId: row.user_id,
    path: row.path,
    type: row.type,
    title: row.title,
    description: row.description,
    tags: Array.isArray(row.tags) ? (row.tags as string[]) : [],
    body: row.body,
    status: row.status as LibraryConceptStatus,
    sources: Array.isArray(row.sources) ? (row.sources as OkfSource[]) : [],
    generatedBy: row.generated_by,
    generatedAt: row.generated_at ? row.generated_at.toISOString() : null,
    verified: Array.isArray(row.verified) ? (row.verified as OkfActorStamp[]) : [],
    staleAfter: row.stale_after,
    sourceDocumentId: row.source_document_id,
    promotedFromId: row.promoted_from_id,
    promotedBy: row.promoted_by,
    promotedAt: row.promoted_at ? row.promoted_at.toISOString() : null,
    archivedAt: row.archived_at ? row.archived_at.toISOString() : null,
    createdAt: row.created_at.toISOString(),
    updatedAt: row.updated_at.toISOString(),
  };
}
