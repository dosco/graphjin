import { createWriteStream } from "node:fs";
import { mkdir, mkdtemp, readFile, readdir, rm, stat } from "node:fs/promises";
import { dirname, extname, join } from "node:path";
import { isUtf8 } from "node:buffer";
import { crc32 } from "node:zlib";
import { canonicalHash, sha256 } from "./canonicalize.js";
import { Transform } from "node:stream";
import { pipeline } from "node:stream/promises";
import { fromBuffer, type Entry, type ZipFile } from "yauzl";
import { loadSolutionPack, type SolutionPackBundle } from "./bundle.js";

export const PACK_ARCHIVE_LIMITS = {
  compressedBytes: 16 * 1024 * 1024,
  extractedBytes: 64 * 1024 * 1024,
  fileBytes: 8 * 1024 * 1024,
  entries: 1000,
  depth: 16,
  ratio: 100,
} as const;

/** Extract only into a new private staging directory. The caller owns promotion. */
export async function stagePackArchive(
  bytes: Buffer,
  stagingParent: string,
  options: { signal?: AbortSignal; reservedIds?: string[] } = {},
): Promise<{ bundle: SolutionPackBundle; cleanup: () => Promise<void> }> {
  if (bytes.length > PACK_ARCHIVE_LIMITS.compressedBytes) throw new Error("pack ZIP exceeds 16 MiB");
  options.signal?.throwIfAborted();
  const zip = await new Promise<ZipFile>((resolve, reject) => {
    fromBuffer(bytes, { lazyEntries: true, strictFileNames: true, validateEntrySizes: true },
      (error, value) => error ? reject(error) : resolve(value!));
  });
  const temporary = await mkdir(stagingParent, { recursive: true, mode: 0o700 })
    .then(() => mkdtemp(join(stagingParent, ".upload-")))
    .catch(error => { zip.close(); throw error; });
  const cleanup = () => rm(temporary, { recursive: true, force: true });
  let root: string | undefined;
  let total = 0;
  let count = 0;
  const paths = new Set<string>();
  const spellings = new Map<string, string>();
  try {
    while (true) {
      const entry = await new Promise<Entry | null>((resolve, reject) => {
        const clean = () => { zip.off("entry", onEntry); zip.off("end", onEnd); zip.off("error", onError); };
        const onEntry = (entry: Entry) => { clean(); resolve(entry); };
        const onEnd = () => { clean(); resolve(null); };
        const onError = (error: Error) => { clean(); reject(error); };
        zip.once("entry", onEntry).once("end", onEnd).once("error", onError);
        zip.readEntry();
      });
      if (!entry) break;
      options.signal?.throwIfAborted();
      if (++count > PACK_ARCHIVE_LIMITS.entries) throw new Error("pack ZIP has too many entries");
      const directory = entry.fileName.endsWith("/");
      const name = directory ? entry.fileName.slice(0, -1) : entry.fileName;
      const parts = name.split("/");
      if (parts.length > PACK_ARCHIVE_LIMITS.depth || parts.some(p => !/^[A-Za-z0-9_.-]+$/.test(p) || p === "." || p === "..")) {
        throw new Error("pack ZIP contains an invalid or overly deep path");
      }
      if (!/^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(parts[0]!)) throw new Error("invalid pack directory name");
      root ??= parts[0];
      if (parts[0] !== root || (!directory && parts.length < 2)) throw new Error("pack ZIP must contain one top-level directory");
      if (options.reservedIds?.includes(root!)) throw new Error("pack id is reserved for a first-party pack");
      const normalized = name.toLowerCase();
      if (paths.has(normalized)) throw new Error("pack ZIP contains duplicate paths");
      paths.add(normalized);
      for (let depth = 1; depth <= parts.length; depth++) {
        const prefix = parts.slice(0, depth).join("/");
        const prior = spellings.get(prefix.toLowerCase());
        if (prior && prior !== prefix) throw new Error("pack ZIP contains case-ambiguous paths");
        spellings.set(prefix.toLowerCase(), prefix);
      }
      const mode = entry.externalFileAttributes >>> 16;
      const kind = mode & 0o170000;
      if ((kind && kind !== (directory ? 0o040000 : 0o100000)) || (!directory && (mode & 0o111))) {
        throw new Error("pack ZIP may contain only non-executable regular files and directories");
      }
      if (entry.extraFields.some(field => (field.id === 0x000d && field.data.length > 12) || field.id === 0x756e)) throw new Error("pack ZIP cannot contain links");
      if (entry.isEncrypted()) throw new Error("encrypted pack ZIP entries are unsupported");
      if (entry.uncompressedSize > PACK_ARCHIVE_LIMITS.fileBytes || entry.uncompressedSize > Math.max(1, entry.compressedSize) * PACK_ARCHIVE_LIMITS.ratio) {
        throw new Error("pack ZIP entry exceeds size or compression ratio limits");
      }
      total += entry.uncompressedSize;
      if (total > PACK_ARCHIVE_LIMITS.extractedBytes) throw new Error("pack ZIP extracted size exceeds 64 MiB");
      const target = join(temporary, name);
      if (directory) {
        if (entry.uncompressedSize !== 0) throw new Error("pack ZIP directory has data");
        await mkdir(target, { recursive: true, mode: 0o700 });
      } else {
        if (![".yaml", ".yml", ".json", ".md", ".gql", ".graphql"].includes(extname(name).toLowerCase())) {
          throw new Error("pack ZIP may contain declarative YAML, JSON, GraphQL and Markdown files only");
        }
        await mkdir(dirname(target), { recursive: true, mode: 0o700 });
        const stream = await new Promise<import("node:stream").Readable>((resolve, reject) => {
          zip.openReadStream(entry, (error, stream) => error ? reject(error) : resolve(stream!));
        });
        let actual = 0;
        let checksum = 0;
        const bound = new Transform({ transform(chunk: Buffer, _encoding, callback) {
          actual += chunk.length;
          checksum = crc32(chunk, checksum);
          callback(actual > entry.uncompressedSize || actual > PACK_ARCHIVE_LIMITS.fileBytes
            ? new Error("pack ZIP entry exceeds declared size") : null, chunk);
        } });
        await pipeline(stream, bound, createWriteStream(target, { flags: "wx", mode: 0o600 }), { signal: options.signal });
        if (actual !== entry.uncompressedSize || checksum !== entry.crc32) throw new Error("pack ZIP entry is corrupt");
        const data = await readFile(target);
        if (!isUtf8(data) || data.includes(0) || data.subarray(0, 2).equals(Buffer.from("PK")) || data.subarray(0, 2).equals(Buffer.from("#!"))) {
          throw new Error("pack ZIP cannot contain binary, executable, or nested archive content");
        }
      }
    }
    if (!root) throw new Error("pack ZIP is empty");
    options.signal?.throwIfAborted();
    const bundle = await loadSolutionPack(join(temporary, root));
    if (bundle.manifest.metadata.id !== root) throw new Error("pack directory and metadata.id must match");
    return { bundle, cleanup };
  } catch (error) {
    await cleanup();
    throw error;
  } finally {
    zip.close();
  }
}

/** Hash all uploaded bytes, including assets not referenced by the manifest. */
export async function hashPackFiles(root: string): Promise<string> {
  const files: Array<[string, string]> = [];
  let bytes = 0;
  const walk = async (path: string, depth: number): Promise<void> => {
    if (depth > PACK_ARCHIVE_LIMITS.depth) throw new Error("pack tree is too deep");
    for (const entry of await readdir(join(root, path), { withFileTypes: true })) {
      if (files.length >= PACK_ARCHIVE_LIMITS.entries) throw new Error("pack tree has too many entries");
      const name = path ? `${path}/${entry.name}` : entry.name;
      if (entry.isDirectory()) { files.push([name, "directory"]); await walk(name, depth + 1); }
      else if (entry.isFile()) {
        const size = (await stat(join(root, name))).size;
        if (size > PACK_ARCHIVE_LIMITS.fileBytes || bytes + size > PACK_ARCHIVE_LIMITS.extractedBytes) throw new Error("pack tree exceeds size limits");
        const content = await readFile(join(root, name));
        bytes += content.length;
        if (content.length > PACK_ARCHIVE_LIMITS.fileBytes || bytes > PACK_ARCHIVE_LIMITS.extractedBytes) throw new Error("pack tree exceeds size limits");
        files.push([name, sha256(content)]);
      } else throw new Error("pack tree contains a link or special file");
    }
  };
  await walk("", 0);
  return canonicalHash(files.sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0));
}
