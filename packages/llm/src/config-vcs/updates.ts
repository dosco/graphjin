import { mkdtemp, readdir, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { hashPackFiles, stagePackArchive } from "@neko/packs";
import { context_remote, db, eq } from "@neko/db";
import { git } from "./git-shell";
import { withRepoLock } from "./lock";
import { readRow, remoteAccess } from "./remote-settings";
import {
  ContextRemoteError,
  fetchRemote,
  folderTrees,
  parseRemoteUrl,
  remotePackArchive,
  remoteTransport,
  skillBase,
  skillFiles,
  skillTreeFiles,
  type PackUpdate,
  type RemoteAccess,
  type RemoteUpdates,
  type SkillBase,
  type SkillUpdate,
} from "./remote";

/**
 * Checking a repository for pack updates validates packs with @neko/packs.
 * That package is not part of the web bundle, so only the worker loads this
 * module. The web app reads the result through the worker's admin API.
 */

/**
 * Compare the remote branch with OpenNeko's skills and packs.
 * `skillBases` holds each skill's tree at its last sync with this remote.
 * `uploadedPacks` maps a pack id to its uploaded versions and their content hashes.
 */
export async function checkRemoteUpdates(opts: {
  orgRoot: string;
  access: RemoteAccess;
  branch: string;
  skillBases: Record<string, string | SkillBase>;
  uploadedPacks: Map<string, Map<string, string>>;
  reservedPackIds?: string[];
}): Promise<RemoteUpdates & { inSync: Record<string, SkillBase> }> {
  const root = resolve(opts.orgRoot);
  return withRepoLock(root, async () => {
    const tip = await fetchRemote(root, opts.access, opts.branch);
    if (!tip) return { tip: null, skills: [], packs: [], inSync: {} };
    const head = (await git(root, ["rev-parse", "--verify", "--quiet", "HEAD"]).catch(() => ({ stdout: "" }))).stdout.trim();
    const local = head ? await skillFiles(root, head) : new Map<string, Map<string, string>>();
    const remote = await skillFiles(root, tip);
    const remoteTrees = await folderTrees(root, tip, "skills");
    const localTrees = head ? await folderTrees(root, head, "skills") : new Map<string, string>();
    const fingerprint = (files: Map<string, string>) => [...files].sort(([a], [b]) => a.localeCompare(b)).map(([path, sha]) => `${path} ${sha}`).join("\n");

    const skills: SkillUpdate[] = [];
    const inSync: Record<string, SkillBase> = {};
    for (const [name, remoteFiles] of [...remote].sort(([a], [b]) => a.localeCompare(b))) {
      const remoteTree = remoteTrees.get(name)!;
      const localFiles = local.get(name);
      if (!localFiles) {
        skills.push({ name, remoteTree, status: "update" });
        continue;
      }
      // Repository-only files (such as a runtime-specific copy) are not a
      // difference, because OpenNeko never publishes or removes them.
      const onlyShared = (files: Map<string, string>) => new Map([...files].filter(([path]) => localFiles.has(path)));
      const sync = skillBase(opts.skillBases[name]);
      const inAgreement = fingerprint(localFiles) === fingerprint(onlyShared(remoteFiles));
      const record = () => { inSync[name] = { remote: remoteTree, local: localTrees.get(name)! }; };
      if (!sync) {
        if (inAgreement) record();
        else skills.push({ name, remoteTree, status: "both_changed" });
        continue;
      }
      // Each side is compared with its own tree at the last sync.
      const remoteChanged = fingerprint(remoteFiles) !== fingerprint(await skillTreeFiles(root, sync.remote));
      const localBase = sync.local
        ? await skillTreeFiles(root, sync.local)
        : onlyShared(await skillTreeFiles(root, sync.remote));
      const localChanged = fingerprint(localFiles) !== fingerprint(localBase);
      if (!remoteChanged) continue;
      if (!localChanged) {
        skills.push({ name, remoteTree, status: "update" });
        continue;
      }
      if (inAgreement) record();
      else skills.push({ name, remoteTree, status: "both_changed" });
    }

    const packs: PackUpdate[] = [];
    const scratch = await mkdtemp(join(tmpdir(), "neko-remote-pack-"));
    try {
      for (const id of [...(await folderTrees(root, tip, "packs")).keys()].sort()) {
        try {
          const staged = await stagePackArchive(await remotePackArchive(root, tip, id), scratch, { reservedIds: opts.reservedPackIds ?? [] });
          try {
            const version = staged.bundle.manifest.metadata.version;
            const contentHash = await hashPackFiles(staged.bundle.root);
            const versions = opts.uploadedPacks.get(id);
            const existing = versions?.get(version);
            if (existing === contentHash) continue;
            packs.push(existing
              ? { id, version, status: "invalid", detail: `Version ${version} changed in the repository. Raise the version number in pack.yaml.` }
              : { id, version, status: versions ? "new_version" : "new_pack", detail: versions ? `Version ${version} is available.` : `New pack, version ${version}.` });
          } finally {
            await staged.cleanup();
          }
        } catch (error) {
          packs.push({ id, version: null, status: "invalid", detail: (error as Error).message });
        }
      }
    } finally {
      await rm(scratch, { recursive: true, force: true });
    }
    return { tip, skills, packs, inSync };
  });
}


async function uploadedPackVersions(orgRoot: string): Promise<Map<string, Map<string, string>>> {
  const packs = new Map<string, Map<string, string>>();
  for (const id of await readdir(join(orgRoot, "packs")).catch(() => [] as string[])) {
    const versions = new Map<string, string>();
    for (const version of await readdir(join(orgRoot, "packs", id, "versions")).catch(() => [] as string[])) {
      const upload = await readFile(join(orgRoot, "packs", id, "versions", version, "upload.json"), "utf8")
        .then((text) => JSON.parse(text) as { contentHash?: string })
        .catch(() => null);
      if (upload?.contentHash) versions.set(version, upload.contentHash);
    }
    if (versions.size) packs.set(id, versions);
  }
  return packs;
}

/** Skills and packs that changed in the remote repository. */
export async function checkOrgUpdates(orgId: string, orgRoot: string): Promise<RemoteUpdates> {
  const row = await readRow(orgId);
  if (!row) throw new ContextRemoteError("Connect a remote repository first.");
  const { inSync, ...updates } = await checkRemoteUpdates({
    orgRoot,
    access: remoteAccess(row),
    branch: row.branch,
    skillBases: row.skill_bases ?? {},
    uploadedPacks: await uploadedPackVersions(orgRoot),
  });
  const bases = { ...row.skill_bases, ...inSync };
  if (Object.entries(inSync).some(([name, base]) => JSON.stringify(row.skill_bases?.[name]) !== JSON.stringify(base))) {
    await db().update(context_remote).set({ skill_bases: bases }).where(eq(context_remote.org_id, orgId));
  }
  return updates;
}

