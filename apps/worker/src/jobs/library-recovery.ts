import { readFile, rm } from "node:fs/promises";
import {
  listLibraryDocumentsOutsideSource,
  markLibraryDocumentInSource,
  markLibraryDocumentStatus,
  resolveLibrarySourcePath,
} from "@neko/llm";
import { saveLibrarySourceFile } from "@neko/llm/library/source-files";

type MoveDependencies = {
  list: typeof listLibraryDocumentsOutsideSource;
  read: (orgId: string, relativePath: string) => Promise<Buffer>;
  save: typeof saveLibrarySourceFile;
  moved: typeof markLibraryDocumentInSource;
  failed: typeof markLibraryDocumentStatus;
  removeOriginal: (orgId: string, relativePath: string) => Promise<void>;
};

const defaultDependencies: MoveDependencies = {
  list: listLibraryDocumentsOutsideSource,
  read: (orgId, relativePath) => readFile(resolveLibrarySourcePath(orgId, relativePath)),
  save: saveLibrarySourceFile,
  moved: markLibraryDocumentInSource,
  failed: markLibraryDocumentStatus,
  removeOriginal: (orgId, relativePath) => rm(resolveLibrarySourcePath(orgId, relativePath), { force: true }),
};

/**
 * Move documents uploaded before the Library became a GraphJin file source
 * into their owner's folder of it, so the agent can search and read them.
 * Library uploads leave the org workspace; thread attachments keep their
 * thread copy. Idempotent: a moved document is never listed again.
 */
export async function moveLibraryDocumentsToSource(
  overrides: Partial<MoveDependencies> = {},
): Promise<{ moved: number; missing: number }> {
  const deps = { ...defaultDependencies, ...overrides };
  let moved = 0;
  let missing = 0;
  for (const document of await deps.list()) {
    let data: Buffer;
    try {
      data = await deps.read(document.orgId, document.relativePath);
    } catch {
      missing += 1;
      await deps.failed({
        orgId: document.orgId,
        id: document.documentId,
        status: "failed",
        error: "The original file is missing. Remove this document and upload it again.",
      });
      continue;
    }
    const stored = await deps.save({ userId: document.userId, filename: document.filename, data });
    await deps.moved({ orgId: document.orgId, id: document.documentId, relativePath: stored.relativePath });
    if (document.relativePath.startsWith("library/uploads/")) {
      await deps.removeOriginal(document.orgId, document.relativePath).catch(() => {});
    }
    moved += 1;
  }
  return { moved, missing };
}
