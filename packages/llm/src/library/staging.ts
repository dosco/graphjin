import { cp, mkdir, readFile, readdir, stat } from "node:fs/promises";
import { dirname, join, normalize, relative, sep } from "node:path";
import { parseConceptDoc } from "./okf";
import { rebuildIndexes } from "./tree";

/** Library files a run may read: concept paths and collection prefixes. */
export type AllowedLibrary = { prefixes: readonly string[]; paths: readonly string[] };

function held(allowed: AllowedLibrary, conceptPath: string): boolean {
  return allowed.paths.includes(conceptPath) || allowed.prefixes.some((prefix) => conceptPath.startsWith(prefix));
}

/**
 * Copies only the concept documents the run holds, then rebuilds the index
 * files so they list nothing the run cannot read. Returns the copied paths.
 */
export async function copyAllowedTeamLibrary(
  sourceRoot: string,
  destinationRoot: string,
  allowed: AllowedLibrary,
): Promise<string[]> {
  const copied: string[] = [];
  const visit = async (dir: string): Promise<void> => {
    let entries;
    try {
      entries = await readdir(dir, { withFileTypes: true });
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") return;
      throw error;
    }
    for (const entry of entries) {
      const absolute = join(dir, entry.name);
      if (entry.isDirectory()) {
        await visit(absolute);
        continue;
      }
      if (!entry.isFile() || entry.name === "index.md" || entry.name === "log.md") continue;
      const conceptPath = relative(sourceRoot, absolute).split(sep).join("/");
      if (!held(allowed, conceptPath)) continue;
      const target = join(destinationRoot, conceptPath);
      await mkdir(dirname(target), { recursive: true });
      await cp(absolute, target);
      copied.push(conceptPath);
    }
  };
  await visit(sourceRoot);
  if (copied.length > 0) await rebuildIndexes(destinationRoot);
  return copied.sort();
}

const SOURCE_PREFIX = "library/uploads/";
const MAX_SOURCE_BYTES = 25 * 1024 * 1024;

/**
 * Copies the uploaded files that staged team concepts name as their sources,
 * at the same path, so a run can read the original data behind a concept it
 * holds (for example the spreadsheet a set of concepts was distilled from).
 * Returns the copied paths.
 */
export async function copyTeamConceptSources(
  stagedOkfRoot: string,
  orgRoot: string,
  stageOrgRoot: string,
): Promise<string[]> {
  const wanted = new Set<string>();
  const visit = async (dir: string): Promise<void> => {
    let entries;
    try {
      entries = await readdir(dir, { withFileTypes: true });
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") return;
      throw error;
    }
    for (const entry of entries) {
      const absolute = join(dir, entry.name);
      if (entry.isDirectory()) await visit(absolute);
      else if (entry.isFile() && entry.name.endsWith(".md")) {
        const doc = parseConceptDoc(await readFile(absolute, "utf8"));
        for (const source of doc?.sources ?? []) {
          const path = normalize(String(source.resource ?? "").replace(/^\/+/, "")).split(sep).join("/");
          if (path.startsWith(SOURCE_PREFIX) && !path.split("/").includes("..")) wanted.add(path);
        }
      }
    }
  };
  await visit(stagedOkfRoot);
  const copied: string[] = [];
  for (const path of [...wanted].sort()) {
    const from = join(orgRoot, path);
    const meta = await stat(from).catch(() => null);
    if (!meta?.isFile() || meta.size > MAX_SOURCE_BYTES) continue;
    const target = join(stageOrgRoot, path);
    await mkdir(dirname(target), { recursive: true });
    await cp(from, target);
    copied.push(path);
  }
  return copied;
}
