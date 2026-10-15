import { createHash } from 'node:crypto';
import { copyAllowedTeamLibrary, copyTeamConceptSources, type AllowedLibrary } from "../library/staging";
import { cp, lstat, mkdir, mkdtemp, readdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import type { AgentWorkspace } from '../agent-backend';
import { KNOWLEDGE_FILES, readKnowledgeSnapshot } from '../knowledge-cache';
import { copySkillOverrides } from './workspace';
import { prepareSandboxDirectory, type PreparedDirectory } from './sandbox-sync';

export type StableWorkspace = Pick<AgentWorkspace, 'orgRoot' | 'knowledgeRoot' | 'skillsRoot'>;
type Snapshot = { root: string; revision: string; manifest: PreparedDirectory; skillOverrides: string[] };
type Entry = { revision: string; value: Promise<Snapshot>; references: number; retired: boolean };
// Shared across the independently bundled Next instrumentation and Work route.
const host = globalThis as typeof globalThis & { __opennekoStagingCache?: Map<string, Entry> };
const cache = host.__opennekoStagingCache ??= new Map<string, Entry>();

/** Host-owned trees only. Include ctime/inode so replacing or restoring mtime invalidates the cache. */
async function treeRevision(root: string): Promise<string> {
  const hash = createHash('sha256');
  async function walk(file: string): Promise<void> {
    let st;
    try { st = await lstat(file, { bigint: true }); }
    catch (error) { if ((error as NodeJS.ErrnoException).code === 'ENOENT') return; throw error; }
    if (st.isSymbolicLink() || (!st.isFile() && !st.isDirectory())) throw new Error('Unsupported stable sandbox input');
    hash.update(JSON.stringify([file, st.ino.toString(), st.size.toString(), st.mtimeNs.toString(), st.ctimeNs.toString(), st.mode.toString()]));
    if (st.isDirectory()) for (const name of (await readdir(file)).sort()) await walk(path.join(file, name));
  }
  await walk(root);
  return hash.digest('hex');
}

async function knowledgeRevision(root: string): Promise<string> {
  const snapshot = await readKnowledgeSnapshot(root);
  // The publisher builds temporary trees beside the atomic snapshot. Their
  // creation/deletion must not invalidate a complete, unchanged generation.
  return snapshot ? createHash('sha256').update(JSON.stringify(snapshot)).digest('hex') : treeRevision(root);
}

async function dispose(entry: Entry): Promise<void> {
  if (entry.retired && entry.references === 0) {
    const snapshot = await entry.value.catch(() => undefined);
    if (snapshot) await rm(path.dirname(snapshot.root), { recursive: true, force: true });
  }
}

class InputsChanged extends Error {}

export function acquireStableSandboxInputs(workspace: StableWorkspace, requiredSkills: readonly string[] = [], allowedSkills?: readonly string[], allowedLibrary?: AllowedLibrary) {
  return acquireInputs(workspace, requiredSkills, allowedSkills, allowedLibrary, 0);
}

async function acquireInputs(workspace: StableWorkspace, requiredSkills: readonly string[], allowedSkills: readonly string[] | undefined, allowedLibrary: AllowedLibrary | undefined, attempt: number): Promise<Snapshot & { hit: boolean; release: () => Promise<void> }> {
  const library = path.join(workspace.orgRoot, 'library', 'okf');
  const overlay = path.join(workspace.orgRoot, 'skill-overlays');
  const key = JSON.stringify([workspace.orgRoot, [...requiredSkills].sort(), allowedSkills ? [...allowedSkills].sort() : null, allowedLibrary ? [[...allowedLibrary.prefixes].sort(), [...allowedLibrary.paths].sort()] : null]);
  const revision = await Promise.all([knowledgeRevision(workspace.knowledgeRoot), ...[workspace.skillsRoot, library, overlay].map(treeRevision)]).then(parts => parts.join(':'));
  let entry = cache.get(key);
  let retired: Entry | undefined;
  const hit = entry?.revision === revision;
  if (!hit) {
    if (entry) { entry.retired = true; retired = entry; }
    entry = { revision, references: 0, retired: false, value: (async (): Promise<Snapshot> => {
      const temp = await mkdtemp(path.join(tmpdir(), 'oss-inputs-'));
      const root = path.join(temp, path.basename(workspace.orgRoot));
      try {
        await mkdir(root);
        const knowledge = await readKnowledgeSnapshot(workspace.knowledgeRoot);
        if (knowledge) {
          await mkdir(path.join(root, 'knowledge'));
          await Promise.all(KNOWLEDGE_FILES.map(file => writeFile(path.join(root, 'knowledge', file), knowledge.files[file])));
        } else {
          await cp(workspace.knowledgeRoot, path.join(root, 'knowledge'), { recursive: true }).catch(error => { if (error.code !== 'ENOENT') throw error; });
        }
        if (allowedLibrary) await copyAllowedTeamLibrary(library, path.join(root, 'library', 'okf'), allowedLibrary);
        else await cp(library, path.join(root, 'library', 'okf'), { recursive: true }).catch(error => { if (error.code !== 'ENOENT') throw error; });
        await copyTeamConceptSources(path.join(root, 'library', 'okf'), workspace.orgRoot, root);
        const skillOverrides = await copySkillOverrides(workspace.skillsRoot, path.join(root, 'skills'), requiredSkills, allowedSkills);
        // A publisher may have changed inputs during construction. Never cache a mixed generation.
        const after = await Promise.all([knowledgeRevision(workspace.knowledgeRoot), ...[workspace.skillsRoot, library, overlay].map(treeRevision)]).then(parts => parts.join(':'));
        if (after !== revision) throw new InputsChanged('Stable sandbox inputs changed during staging; retry the turn');
        return { root, revision, skillOverrides, manifest: await prepareSandboxDirectory(root) };
      } catch (error) { await rm(temp, { recursive: true, force: true }); throw error; }
    })() };
    cache.set(key, entry);
  }
  const held = entry!;
  held.references++;
  let released = false;
  const release = async () => {
    if (released) return;
    released = true;
    held.references--;
    await dispose(held);
  };
  try {
    if (retired) await dispose(retired);
    return { ...await held.value, hit, release };
  }
  catch (error) {
    if (cache.get(key) === held) cache.delete(key);
    held.retired = true;
    await release();
    if (error instanceof InputsChanged && attempt < 2) return acquireInputs(workspace, requiredSkills, allowedSkills, allowedLibrary, attempt + 1);
    throw error;
  }
}

export async function clearStableSandboxInputs(): Promise<void> {
  const entries = [...cache.values()]; cache.clear();
  await Promise.all(entries.map(entry => { entry.retired = true; return dispose(entry); }));
}
