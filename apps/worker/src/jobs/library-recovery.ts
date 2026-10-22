import { readFile, rm } from "node:fs/promises";
import { join, resolve, sep } from "node:path";
import {
  getOrgAgentRoot,
  listLibraryDocumentsOutsideSource,
  markLibraryDocumentInSource,
  markLibraryDocumentFailed,
} from "@neko/llm";
import { saveLibrarySourceFile } from "@neko/llm/library/source-files";

/** An upload's old place in the org workspace, refusing paths that leave it. */
function workspaceUploadPath(orgId: string, relativePath: string): string {
  const orgRoot = resolve(getOrgAgentRoot(orgId));
  const absolute = resolve(join(orgRoot, relativePath.replace(/\\/g, "/").replace(/^\/+/, "")));
  if (!absolute.startsWith(orgRoot + sep)) throw new Error("library upload path leaves the org workspace");
  return absolute;
}

type MoveDependencies = {
  list: typeof listLibraryDocumentsOutsideSource;
  read: (orgId: string, relativePath: string) => Promise<Buffer>;
  save: typeof saveLibrarySourceFile;
  moved: typeof markLibraryDocumentInSource;
  failed: typeof markLibraryDocumentFailed;
  removeOriginal: (orgId: string, relativePath: string) => Promise<void>;
};

const defaultDependencies: MoveDependencies = {
  list: listLibraryDocumentsOutsideSource,
  read: (orgId, relativePath) => readFile(workspaceUploadPath(orgId, relativePath)),
  save: saveLibrarySourceFile,
  moved: markLibraryDocumentInSource,
  failed: markLibraryDocumentFailed,
  removeOriginal: (orgId, relativePath) => rm(workspaceUploadPath(orgId, relativePath), { force: true }),
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
