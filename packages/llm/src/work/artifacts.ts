import { readdir, realpath } from "node:fs/promises";
import { basename, isAbsolute, join, relative, resolve, sep } from "node:path";
import type { AgentArtifact, AgentWorkspace } from "../agent-backend";

function portable(path: string): string {
  return path.split(sep).join("/");
}

function isWithin(root: string, candidate: string): boolean {
  const rel = relative(root, candidate);
  return rel === "" || (!rel.startsWith("..") && !isAbsolute(rel));
}

/**
 * Discover only regular files physically contained by this run's artifact
 * directory. Symlinks are deliberately ignored: a generated link must never
 * turn a different workspace file into a downloadable artifact.
 */
export async function discoverRunArtifacts(
  workspace: AgentWorkspace,
): Promise<AgentArtifact[]> {
  const root = await realpath(workspace.artifactRoot).catch(() => null);
  if (!root) return [];
  const orgRoot = await realpath(resolve(workspace.orgRoot)).catch(() => null);
  if (!orgRoot) return [];
  const physicalArtifactRoot = root;
  const physicalOrgRoot = orgRoot;
  const artifacts: AgentArtifact[] = [];

  async function walk(directory: string): Promise<void> {
    const entries = await readdir(directory, { withFileTypes: true }).catch(
      () => [],
    );
    for (const entry of entries) {
      const candidate = join(directory, entry.name);
      if (entry.isSymbolicLink()) continue;
      if (entry.isDirectory()) {
        await walk(candidate);
        continue;
      }
      if (!entry.isFile()) continue;
      const physical = await realpath(candidate).catch(() => null);
      if (
        !physical ||
        !isWithin(physicalArtifactRoot, physical) ||
        !isWithin(physicalOrgRoot, physical)
      ) {
        continue;
      }
      artifacts.push({
        path: portable(relative(physicalOrgRoot, physical)),
        label: basename(physical),
      });
    }
  }

  await walk(physicalArtifactRoot);
  return artifacts.sort((a, b) => a.path.localeCompare(b.path));
}
