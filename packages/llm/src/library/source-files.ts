import { createHash } from "node:crypto";
import { chown, mkdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { dirname, join, resolve, sep } from "node:path";
import {
  LIBRARY_SOURCE_NAME,
  LIBRARY_SOURCE_ROOT,
  LIBRARY_USER_PREFIX,
} from "../graphjin/sources-config";

/**
 * Library files live in the customer GraphJin's neko_library file source:
 * <GraphJin config dir>/files/library/users/<user id>/<sha256>/<filename>.
 * GraphJin keeps each users/<id>/ folder private to that user and searches,
 * reads and queries the files there; OpenNeko only writes and removes them.
 *
 * library_document.relative_path stores "neko_library/<key>".
 */
export const LIBRARY_SOURCE_PATH_PREFIX = `${LIBRARY_SOURCE_NAME}/`;

/** GraphJin's subject for a run without a user; see graphjin/token.ts. */
const NO_USER_SUBJECT = "service";

export function librarySourceRoot(configFile = process.env.OPENNEKO_GRAPHJIN_CONFIG): string {
  const file = configFile?.trim();
  if (!file) {
    throw new Error("Library storage needs OPENNEKO_GRAPHJIN_CONFIG, the customer GraphJin config file");
  }
  return join(dirname(file), LIBRARY_SOURCE_ROOT);
}

/** The user's folder segment: the GraphJin token subject, exactly. */
export function libraryUserSegment(userId: string | null): string {
  const segment = userId ?? NO_USER_SUBJECT;
  if (!segment || segment.includes("/") || segment.includes("\\") || segment === "." || segment === "..") {
    throw new Error("Library: user id cannot name a folder");
  }
  return segment;
}

export function isLibrarySourcePath(relativePath: string): boolean {
  return relativePath.startsWith(LIBRARY_SOURCE_PATH_PREFIX);
}

/** Absolute path of a stored library file, refusing anything outside the source. */
export function librarySourceFilePath(relativePath: string, root = librarySourceRoot()): string {
  if (!isLibrarySourcePath(relativePath)) throw new Error("Not a Library file");
  const key = relativePath.slice(LIBRARY_SOURCE_PATH_PREFIX.length);
  const absolute = resolve(root, key);
  if (!absolute.startsWith(resolve(root) + sep)) throw new Error("Library file path escapes the Library");
  return absolute;
}

/**
 * Store an upload in the user's Library folder. Content-addressed, so a later
 * upload with the same cleaned name never overwrites an earlier file.
 */
export async function saveLibrarySourceFile(input: {
  userId: string | null;
  filename: string;
  data: Buffer;
  root?: string;
}): Promise<{ relativePath: string; contentHash: string; size: number }> {
  const root = input.root ?? librarySourceRoot();
  const contentHash = createHash("sha256").update(input.data).digest("hex");
  const key = `${LIBRARY_USER_PREFIX}${libraryUserSegment(input.userId)}/${contentHash}/${input.filename}`;
  const relativePath = LIBRARY_SOURCE_PATH_PREFIX + key;
  const absolute = librarySourceFilePath(relativePath, root);
  await mkdir(dirname(absolute), { recursive: true, mode: 0o700 });
  // Write then rename, so GraphJin never indexes a half-written file.
  const partial = `${absolute}.partial`;
  await writeFile(partial, input.data, { mode: 0o600 });
  await handToLibraryOwner(root, absolute, partial);
  await rename(partial, absolute);
  return { relativePath, contentHash, size: input.data.byteLength };
}

/**
 * GraphJin reads the Library as the owner of its config directory, two
 * levels above the Library root. A writer running as root (a dev host)
 * hands what it created to that owner; elsewhere the writer already is the
 * owner and this does nothing.
 */
async function handToLibraryOwner(root: string, absolute: string, file: string): Promise<void> {
  if (process.getuid?.() !== 0) return;
  const owner = await stat(dirname(dirname(root)));
  const paths = [file];
  for (let dir = dirname(absolute); dir.startsWith(root + sep); dir = dirname(dir)) paths.push(dir);
  for (const path of paths) await chown(path, owner.uid, owner.gid);
}

export async function readLibrarySourceFile(relativePath: string): Promise<Buffer> {
  return readFile(librarySourceFilePath(relativePath));
}

/** Remove a stored file and its now-empty content folder. */
export async function removeLibrarySourceFile(relativePath: string): Promise<void> {
  const absolute = librarySourceFilePath(relativePath);
  await rm(dirname(absolute), { recursive: true, force: true });
}
