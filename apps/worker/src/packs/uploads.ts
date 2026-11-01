import { randomUUID } from "node:crypto";
import { cp, lstat, mkdir, mkdtemp, readFile, readdir, rename, rm, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { hashPackFiles, loadSolutionPack, sha256, stagePackArchive, type SolutionPackBundle } from "@neko/packs";

export type PackUpload = {
  orgId: string; packId: string; version: string;
  bundleHash: string; contentHash: string; archiveHash: string;
  uploadedAt: string; uploadedBy: string | null;
};
export type AvailablePack = SolutionPackBundle & { upload?: PackUpload };
const packIdPattern = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/;
const versionPattern = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/;

async function regular(path: string, directory = false): Promise<void> {
  const info = await lstat(path);
  if (directory ? !info.isDirectory() : !info.isFile()) throw new Error("uploaded pack storage contains a link or special file");
}

export async function loadUploadedPack(root: string, orgId: string, packId: string, version?: string): Promise<AvailablePack> {
  if (!packIdPattern.test(packId)) throw new Error("invalid uploaded pack id");
  const packRoot = join(root, packId);
  await regular(packRoot, true);
  if (!version) {
    await regular(join(packRoot, "candidate.json"));
    version = (JSON.parse(await readFile(join(packRoot, "candidate.json"), "utf8")) as { version: string }).version;
  }
  if (typeof version !== "string" || !versionPattern.test(version)) throw new Error("invalid uploaded pack version");
  const directory = join(packRoot, "versions", version);
  for (const path of [join(packRoot, "versions"), directory, join(directory, "bundle")]) await regular(path, true);
  await regular(join(directory, "upload.json"));
  const upload = JSON.parse(await readFile(join(directory, "upload.json"), "utf8")) as PackUpload;
  if (upload.orgId !== orgId || upload.packId !== packId || upload.version !== version) throw new Error("uploaded pack provenance does not match this organization/version");
  const contentRoot = join(directory, "bundle");
  if (await hashPackFiles(contentRoot) !== upload.contentHash) throw new Error("uploaded pack contents changed after upload");
  const bundle = await loadSolutionPack(contentRoot);
  if (bundle.manifest.metadata.id !== packId || bundle.manifest.metadata.version !== version || bundle.bundleHash !== upload.bundleHash) throw new Error("uploaded pack manifest does not match its provenance");
  return { ...bundle, upload };
}

export async function listUploadedPacks(root: string, orgId: string): Promise<AvailablePack[]> {
  const entries = await readdir(root, { withFileTypes: true }).catch((error: NodeJS.ErrnoException) => {
    if (error.code === "ENOENT") return [];
    throw error;
  });
  const bundles: AvailablePack[] = [];
  for (const entry of entries) {
    if (!entry.isDirectory() || !packIdPattern.test(entry.name)) continue;
    try { bundles.push(await loadUploadedPack(root, orgId, entry.name)); }
    catch (error) { if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error; }
  }
  return bundles;
}

export async function storePackUpload(input: {
  root: string; orgId: string; bytes: Buffer; reservedIds: string[];
  actorUserId?: string | null; signal?: AbortSignal;
}): Promise<PackUpload> {
  const staged = await stagePackArchive(input.bytes, join(input.root, ".staging"), { reservedIds: input.reservedIds, signal: input.signal });
  try {
    const { id: packId, version } = staged.bundle.manifest.metadata;
    const upload: PackUpload = { orgId: input.orgId, packId, version, bundleHash: staged.bundle.bundleHash,
      contentHash: await hashPackFiles(staged.bundle.root), archiveHash: sha256(input.bytes),
      uploadedAt: new Date().toISOString(), uploadedBy: input.actorUserId ?? null };
    const packRoot = join(input.root, packId), directory = join(packRoot, "versions", version);
    const temporary = dirname(staged.bundle.root);
    await writeFile(join(temporary, "upload.json"), JSON.stringify(upload), { mode: 0o600, flag: "wx" });
    await rename(staged.bundle.root, join(temporary, "bundle"));
    await mkdir(dirname(directory), { recursive: true, mode: 0o700 });
    input.signal?.throwIfAborted();
    let stored = upload;
    let duplicate = false;
    try { await rename(temporary, directory); }
    catch (error) {
      if (!["EEXIST", "ENOTEMPTY"].includes((error as NodeJS.ErrnoException).code ?? "")) throw error;
      const existing = await loadUploadedPack(input.root, input.orgId, packId, version);
      if (existing.upload!.contentHash !== upload.contentHash) throw new Error("uploaded pack versions are immutable; use a new version for changed contents");
      stored = existing.upload!;
      duplicate = true;
    }
    // Only a complete version is published. A crash before this atomic pointer
    // leaves an inactive orphan; retrying the same upload safely publishes it.
    const candidate = join(packRoot, "candidate.json");
    const hasCandidate = await lstat(candidate).then(() => true, error => {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") return false;
      throw error;
    });
    if (!duplicate || !hasCandidate) {
      const pointer = join(packRoot, `.candidate-${randomUUID()}.json`);
      await writeFile(pointer, JSON.stringify({ version }), { mode: 0o600, flag: "wx" });
      await rename(pointer, candidate);
    }
    return stored;
  } finally { await staged.cleanup(); }
}

/** Installation reads a verified private snapshot, never mutable catalog files. */
export async function snapshotUploadedPack(root: string, bundle: AvailablePack) {
  const directory = await mkdtemp(join(root, ".apply-"));
  const cleanup = () => rm(directory, { recursive: true, force: true });
  try {
    const target = join(directory, "bundle");
    await cp(bundle.root, target, { recursive: true, force: false, errorOnExist: true });
    if (!bundle.upload || await hashPackFiles(target) !== bundle.upload.contentHash) throw new Error("uploaded pack changed while preparing installation");
    return { bundle: { ...await loadSolutionPack(target), upload: bundle.upload } as AvailablePack, cleanup };
  } catch (error) { await cleanup(); throw error; }
}
