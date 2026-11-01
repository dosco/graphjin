import "server-only";

import { createWriteStream } from "node:fs";
import { mkdir, mkdtemp, readFile, rename, rm } from "node:fs/promises";
import { dirname, join } from "node:path";
import { Transform } from "node:stream";
import { pipeline } from "node:stream/promises";
import { crc32 } from "node:zlib";
import { ensureOrgWorkspace } from "@neko/llm/work";
import { recordConfigChange } from "@neko/llm/config-vcs";
import { parseDocument } from "yaml";
import { fromBuffer, type Entry, type ZipFile } from "yauzl";

const LIMITS = {
  archive: 16 * 1024 * 1024,
  extracted: 64 * 1024 * 1024,
  file: 8 * 1024 * 1024,
  entries: 500,
  depth: 12,
  ratio: 200,
} as const;
const NAME = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

export class SkillArchiveError extends Error {
  constructor(message: string, readonly status = 400) {
    super(message);
  }
}

function invalid(message: string): never {
  throw new SkillArchiveError(message);
}

function openZip(bytes: Buffer): Promise<ZipFile> {
  return new Promise((resolve, reject) => {
    fromBuffer(bytes, { lazyEntries: true, strictFileNames: true, validateEntrySizes: true },
      (error, zip) => error ? reject(error) : resolve(zip!));
  });
}

function nextEntry(zip: ZipFile): Promise<Entry | null> {
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      zip.off("entry", onEntry);
      zip.off("end", onEnd);
      zip.off("error", onError);
    };
    const onEntry = (entry: Entry) => { cleanup(); resolve(entry); };
    const onEnd = () => { cleanup(); resolve(null); };
    const onError = (error: Error) => { cleanup(); reject(error); };
    zip.once("entry", onEntry).once("end", onEnd).once("error", onError);
    zip.readEntry();
  });
}

function readEntry(zip: ZipFile, entry: Entry) {
  return new Promise<import("node:stream").Readable>((resolve, reject) => {
    zip.openReadStream(entry, (error, stream) => error ? reject(error) : resolve(stream!));
  });
}

/** Import a complete Agent Skills directory from a .skill or ZIP archive. */
export async function importWorkSkillArchive(
  orgId: string,
  bytes: Buffer,
  actorUserId?: string | null,
): Promise<string> {
  if (bytes.length === 0 || bytes.length > LIMITS.archive) invalid("Skill archive must be at most 16 MB.");
  let zip: ZipFile;
  try {
    zip = await openZip(bytes);
  } catch {
    invalid("Choose a valid .skill or ZIP archive.");
  }
  let roots: Awaited<ReturnType<typeof ensureOrgWorkspace>>;
  let staging: string;
  try {
    roots = await ensureOrgWorkspace(orgId);
    staging = await mkdtemp(join(roots.orgRoot, ".skill-import-"));
  } catch (error) {
    zip.close();
    throw error;
  }
  let root: string | undefined;
  let count = 0;
  let total = 0;
  const paths = new Set<string>();
  const spellings = new Map<string, string>();

  try {
    while (true) {
      const entry = await nextEntry(zip);
      if (!entry) break;
      if (++count > LIMITS.entries) invalid("Skill archive has too many files.");
      const directory = entry.fileName.endsWith("/");
      const name = directory ? entry.fileName.slice(0, -1) : entry.fileName;
      const parts = name.split("/");
      if (name.length > 240 || parts.length > LIMITS.depth || parts.some(part =>
        !part || part === "." || part === ".." || /[\\\u0000-\u001f\u007f]/.test(part))) {
        invalid("Skill archive contains an invalid path.");
      }
      if (!NAME.test(parts[0]!) || parts[0]!.length > 64 || (!directory && parts.length < 2)) {
        invalid("Put one skill directory at the archive root.");
      }
      root ??= parts[0];
      if (parts[0] !== root) invalid("Skill archive must contain one skill directory.");
      const folded = name.toLowerCase();
      if (paths.has(folded)) invalid("Skill archive contains duplicate paths.");
      paths.add(folded);
      for (let depth = 1; depth <= parts.length; depth++) {
        const prefix = parts.slice(0, depth).join("/");
        const prior = spellings.get(prefix.toLowerCase());
        if (prior && prior !== prefix) invalid("Skill archive contains case-ambiguous paths.");
        spellings.set(prefix.toLowerCase(), prefix);
      }
      const mode = entry.externalFileAttributes >>> 16;
      const kind = mode & 0o170000;
      if (kind && kind !== (directory ? 0o040000 : 0o100000)) {
        invalid("Skill archive may contain only files and directories.");
      }
      if (entry.isEncrypted()) invalid("Encrypted skill archives are unsupported.");
      if (entry.uncompressedSize > LIMITS.file ||
          entry.uncompressedSize > Math.max(1, entry.compressedSize) * LIMITS.ratio) {
        invalid("Skill archive has a file over the size or compression limit.");
      }
      total += entry.uncompressedSize;
      if (total > LIMITS.extracted) invalid("Skill archive exceeds 64 MB when extracted.");
      const target = join(staging, name);
      if (directory) {
        if (entry.uncompressedSize !== 0) invalid("Skill archive contains an invalid directory.");
        await mkdir(target, { recursive: true, mode: 0o755 });
        continue;
      }
      await mkdir(dirname(target), { recursive: true, mode: 0o755 });
      let actual = 0;
      let checksum = 0;
      const bound = new Transform({ transform(chunk: Buffer, _encoding, callback) {
        actual += chunk.length;
        checksum = crc32(chunk, checksum);
        callback(actual > entry.uncompressedSize || actual > LIMITS.file
          ? new Error("Skill archive file exceeds its declared size.") : null, chunk);
      } });
      await pipeline(await readEntry(zip, entry), bound, createWriteStream(target, {
        flags: "wx", mode: mode & 0o111 ? 0o755 : 0o644,
      }));
      if (actual !== entry.uncompressedSize || checksum !== entry.crc32) {
        invalid("Skill archive contains a corrupt file.");
      }
    }
    if (!root) invalid("Skill archive is empty.");
    const skillMarkdown = await readFile(join(staging, root, "SKILL.md"), "utf8")
      .catch(() => invalid("Skill archive needs SKILL.md at the skill directory root."));
    const match = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/.exec(skillMarkdown);
    if (!match) invalid("SKILL.md needs YAML frontmatter with a name and description.");
    const doc = parseDocument(match[1], { uniqueKeys: true });
    if (doc.errors.length > 0) invalid("SKILL.md frontmatter is invalid YAML.");
    const frontmatter = doc.toJS() as Record<string, unknown> | null;
    if (!frontmatter || frontmatter.name !== root ||
        typeof frontmatter.description !== "string" ||
        !frontmatter.description.trim() || frontmatter.description.length > 1024) {
      invalid("SKILL.md name must match its folder, with a description of at most 1024 characters.");
    }

    const destination = join(roots.skillsRoot, root);
    try {
      await mkdir(destination);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "EEXIST") {
        throw new SkillArchiveError("A skill with this name already exists.", 409);
      }
      throw error;
    }
    try {
      await rename(join(staging, root), destination);
    } catch (error) {
      await rm(destination, { recursive: true, force: true });
      throw error;
    }
    await recordConfigChange({
      workspaceRoot: roots.orgRoot,
      orgId,
      paths: [`skills/${root}`],
      message: `Imported skill: ${root}`,
      artifactKind: "skill",
      artifactRef: root,
      actorUserId,
    });
    return root;
  } catch (error) {
    if (error instanceof SkillArchiveError) throw error;
    if (error instanceof Error && /(?:zip|invalid|size|CRC|file)/i.test(error.message)) {
      throw new SkillArchiveError("Skill archive is invalid or corrupt.");
    }
    throw error;
  } finally {
    zip.close();
    await rm(staging, { recursive: true, force: true });
  }
}
