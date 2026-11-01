// Document library domain: the files each user put in their Library,
// tracked as library_document rows. The files themselves live in the
// customer GraphJin's neko_library file source (see library/source-files),
// which keeps each user's folder private to them and searches, reads and
// queries the files for the agent. A NULL user_id belongs to a solo install
// with no user ids and still behaves as that uploader's own document.

import { and, asc, db, desc, eq, isNull, library_document, sql } from "@neko/db";

export const LIBRARY_DOCUMENT_STATUSES = ["ready", "failed"] as const;
export type LibraryDocumentStatus = (typeof LIBRARY_DOCUMENT_STATUSES)[number];

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
  error: string | null;
  createdAt: string;
  updatedAt: string;
};

type DocumentRow = typeof library_document.$inferSelect;

export type LibraryBrowseOptions = {
  query: string;
  status: string;
  sort: "recent" | "name";
  page: number;
  pageSize: number;
};

type LibraryReader = { orgId: string; userId: string | null };

export function parseLibraryBrowseOptions(params: URLSearchParams): LibraryBrowseOptions {
  const query = (params.get("q") ?? "").trim();
  const sort = params.get("sort") ?? "recent";
  const status = params.get("status") ?? "";
  const page = Number(params.get("page") ?? 1);
  const pageSize = Number(params.get("pageSize") ?? 50);
  if (!["recent", "name"].includes(sort)
    || query.length > 200
    || !Number.isSafeInteger(page) || page < 1 || page > 1_000_000
    || !Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100
    || (status && !LIBRARY_DOCUMENT_STATUSES.some(value => value === status))) {
    throw new Error("Invalid library filters.");
  }
  return { query, status, sort, page, pageSize } as LibraryBrowseOptions;
}

/** The reader's own documents, filtered by file name words and status. */
export async function browseLibrary(reader: LibraryReader, options: LibraryBrowseOptions) {
  // Escape LIKE metacharacters; a user's '%' or '_' is text, not a wildcard.
  const words = options.query.split(/\s+/).filter(Boolean).map(word => `%${word.replace(/[\\%_]/g, "\\$&")}%`);
  const where = and(
    eq(library_document.org_id, reader.orgId),
    userLayerCondition(reader.userId),
    ...words.map(word => sql`${library_document.filename} ilike ${word}`),
    options.status ? eq(library_document.status, options.status) : undefined,
  );
  const count = sql<number>`count(*)::int`;
  const [{ total }] = await db().select({ total: count }).from(library_document).where(where);
  const page = Math.min(options.page, Math.max(1, Math.ceil(total / options.pageSize)));
  const documents = await db().select({
    id: library_document.id, filename: library_document.filename, relativePath: library_document.relative_path,
    sizeBytes: library_document.size_bytes, status: library_document.status, error: library_document.error,
    createdAt: library_document.created_at,
  }).from(library_document).where(where)
    .orderBy(options.sort === "name" ? asc(library_document.filename) : desc(library_document.created_at), asc(library_document.id))
    .limit(options.pageSize).offset((page - 1) * options.pageSize);
  return {
    total, page, pageSize: options.pageSize,
    documents: documents.map(row => ({ ...row, createdAt: row.createdAt.toISOString() })),
  };
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
  // existing row.
  const existing = await db()
    .select()
    .from(library_document)
    .where(
      and(
        eq(library_document.org_id, input.orgId),
        userLayerCondition(input.userId),
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
      status: "ready",
    })
    .returning();
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

/** Documents still stored in the org workspace, from before the Library moved to GraphJin. */
export async function listLibraryDocumentsOutsideSource(): Promise<
  Array<{ orgId: string; documentId: string; userId: string | null; filename: string; relativePath: string }>
> {
  return db()
    .select({
      orgId: library_document.org_id,
      documentId: library_document.id,
      userId: library_document.user_id,
      filename: library_document.filename,
      relativePath: library_document.relative_path,
    })
    .from(library_document)
    .where(sql`${library_document.relative_path} not like 'neko_library/%'`);
}

/** Point a document at its copy in the Library file source. */
export async function markLibraryDocumentInSource(input: {
  orgId: string;
  id: string;
  relativePath: string;
}): Promise<void> {
  await db()
    .update(library_document)
    .set({ relative_path: input.relativePath, status: "ready", error: null, updated_at: new Date() })
    .where(and(eq(library_document.org_id, input.orgId), eq(library_document.id, input.id)));
}

export async function markLibraryDocumentFailed(input: {
  orgId: string;
  id: string;
  error: string;
}): Promise<void> {
  await db()
    .update(library_document)
    .set({ status: "failed", error: input.error, updated_at: new Date() })
    .where(and(eq(library_document.org_id, input.orgId), eq(library_document.id, input.id)));
}

/** Delete the reader's document row; the caller removes the stored file. */
export async function removeLibraryDocument(input: {
  orgId: string;
  id: string;
  userId: string | null;
}): Promise<LibraryDocument> {
  const document = await getLibraryDocument(input.orgId, input.id);
  if (!document || document.userId !== input.userId) {
    throw new Error(`Library document not found: ${input.id}`);
  }
  await db()
    .delete(library_document)
    .where(and(eq(library_document.org_id, input.orgId), eq(library_document.id, input.id)));
  return document;
}

function userLayerCondition(userId: string | null) {
  return userId === null ? isNull(library_document.user_id) : eq(library_document.user_id, userId);
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
    error: row.error,
    createdAt: row.created_at.toISOString(),
    updatedAt: row.updated_at.toISOString(),
  };
}
