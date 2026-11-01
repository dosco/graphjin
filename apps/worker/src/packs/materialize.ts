import { randomUUID } from "node:crypto";
import { cp, lstat, mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { basename, dirname, join, relative, resolve, sep } from "node:path";
import type { SolutionPackBundle } from "@neko/packs";
import { ensureOrgWorkspace } from "@neko/llm/work";

export async function pathExists(path: string): Promise<boolean> {
  return lstat(path).then(() => true).catch(() => false);
}

function renderTemplate(value: string, inputs: Record<string, unknown>): string {
  return value.replace(/\{\{([^}]+)}}/g, (_match, key: string) => {
    const resolved = inputs[key.trim()];
    if (resolved === undefined) throw new Error(`missing template input ${key.trim()}`);
    return String(resolved);
  });
}

async function writeAtomic(path: string, content: string, alreadyOwned: boolean): Promise<() => Promise<void>> {
  await mkdir(dirname(path), { recursive: true });
  const existed = await pathExists(path);
  if (existed && !alreadyOwned) throw new Error(`pack file target ${path} already exists and is not owned by this pack`);
  const previous = existed ? await readFile(path) : null;
  const temporary = `${path}.${randomUUID()}.pack-stage`;
  await writeFile(temporary, content, { mode: 0o644 });
  await rename(temporary, path);
  return async () => {
    if (previous) {
      const rollback = `${path}.${randomUUID()}.pack-rollback`;
      await writeFile(rollback, previous, { mode: 0o644 });
      await rename(rollback, path);
    } else await rm(path, { force: true });
  };
}

export async function stageOwnedRemoval(path: string): Promise<{ restore: () => Promise<void>; commit: () => Promise<void> }> {
  let target;
  try {
    target = await lstat(path);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return { restore: async () => {}, commit: async () => {} };
    throw error;
  }
  if (target.isSymbolicLink()) throw new Error(`pack-owned removal target is a symlink: ${path}`);
  const backup = `${path}.${randomUUID()}.pack-remove-backup`;
  await rename(path, backup);
  return {
    restore: async () => { if (await pathExists(backup)) await rename(backup, path); },
    commit: async () => { await rm(backup, { recursive: target.isDirectory(), force: true }); },
  };
}

export function assertOwnedPath(root: string, target: string, label: string): string {
  const absoluteRoot = resolve(root);
  const absoluteTarget = resolve(target);
  const path = relative(absoluteRoot, absoluteTarget);
  if (!path || path === ".." || path.startsWith(`..${sep}`) || path.startsWith(sep)) {
    throw new Error(`${label} target escapes its managed root: ${target}`);
  }
  return absoluteTarget;
}

export async function installGraphjinFiles(input: {
  bundle: SolutionPackBundle;
  configFile: string;
  values: Record<string, unknown>;
  ownedTargets: Set<string>;
}): Promise<{ restore: () => Promise<void>; targets: Map<string, string> }> {
  const configRoot = dirname(input.configFile);
  const restorers: Array<() => Promise<void>> = [];
  const targets = new Map<string, string>();
  try {
    for (const artifact of input.bundle.artifacts) {
      let destination: string | null = null;
      let content: string | null = null;
      if (artifact.kind === "spec") {
        destination = join(configRoot, "specs", basename(artifact.path));
        content = renderTemplate(await readFile(join(input.bundle.root, artifact.path), "utf8"), input.values);
      } else if (artifact.kind === "saved_query") {
        destination = join(configRoot, "queries", `${input.bundle.manifest.metadata.id.replaceAll("-", "_")}_${basename(artifact.path)}`);
        content = String(artifact.content);
      }
      if (destination && content !== null) {
        restorers.push(await writeAtomic(destination, content, input.ownedTargets.has(destination)));
        targets.set(`${artifact.kind}:${artifact.key}`, destination);
      }
    }
    return { targets, restore: async () => { for (const restore of restorers.reverse()) await restore(); } };
  } catch (error) {
    for (const restore of restorers.reverse()) await restore().catch(() => {});
    throw error;
  }
}

export async function installSkills(input: {
  orgId: string;
  bundle: SolutionPackBundle;
  ownedTargets: Set<string>;
}): Promise<{ targets: Map<string, string>; restore: () => Promise<void>; commit: () => Promise<void> }> {
  const workspace = await ensureOrgWorkspace(input.orgId);
  const restorers: Array<() => Promise<void>> = [];
  const committers: Array<() => Promise<void>> = [];
  const targets = new Map<string, string>();
  try {
    for (const artifact of input.bundle.artifacts.filter((value) => value.kind === "skill")) {
      const source = join(input.bundle.root, dirname(artifact.path));
      const target = join(workspace.skillsRoot, artifact.targetRef);
      const backup = `${target}.${randomUUID()}.pack-backup`;
      const stage = `${target}.${randomUUID()}.pack-stage`;
      const existed = await pathExists(target);
      if (existed && !input.ownedTargets.has(target)) throw new Error(`skill target ${basename(target)} already exists and is not owned by this pack`);
      await cp(source, stage, { recursive: true, force: false, errorOnExist: true });
      if (existed) await rename(target, backup);
      await rename(stage, target);
      targets.set(`${artifact.kind}:${artifact.key}`, target);
      restorers.push(async () => { await rm(target, { recursive: true, force: true }); if (existed) await rename(backup, target); });
      committers.push(async () => { await rm(backup, { recursive: true, force: true }); });
    }
    return {
      targets,
      restore: async () => { for (const restore of restorers.reverse()) await restore(); },
      commit: async () => { for (const commit of committers) await commit(); },
    };
  } catch (error) {
    for (const restore of restorers.reverse()) await restore().catch(() => {});
    throw error;
  }
}

/**
 * A pack's GraphJin files, delivered through gj_config instead of a shared
 * folder. Specs travel as inline OpenAPI documents and saved queries as
 * update_saved_queries, so any GraphJin that keeps its own config can take
 * them. Targets are graphjin:<kind>:<name> references, not paths.
 */
export async function graphjinApiArtifacts(input: {
  bundle: SolutionPackBundle;
  values: Record<string, unknown>;
}): Promise<{
  specDocuments: Map<string, string>;
  savedQueries: Array<{ name: string; query: string }>;
  targets: Map<string, string>;
}> {
  const specDocuments = new Map<string, string>();
  const savedQueries: Array<{ name: string; query: string }> = [];
  const targets = new Map<string, string>();
  for (const artifact of input.bundle.artifacts) {
    if (artifact.kind === "spec") {
      specDocuments.set(artifact.path, renderTemplate(await readFile(join(input.bundle.root, artifact.path), "utf8"), input.values));
      targets.set(`${artifact.kind}:${artifact.key}`, `graphjin:spec:${artifact.path}`);
    } else if (artifact.kind === "saved_query") {
      const name = packSavedQueryName(input.bundle.manifest.metadata.id, artifact.path);
      savedQueries.push({ name, query: String(artifact.content) });
      targets.set(`${artifact.kind}:${artifact.key}`, `graphjin:saved_query:${name}`);
    }
  }
  return { specDocuments, savedQueries, targets };
}

/** The saved query name GraphJin serves for a pack query file. */
export function packSavedQueryName(packId: string, path: string): string {
  return `${packId.replaceAll("-", "_")}_${basename(path).replace(/\.(gql|graphql)$/i, "")}`;
}
