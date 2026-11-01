import { readdir } from "node:fs/promises";

/** Empty directories need no transfer; include links and partial output. */
export async function hasArtifacts(root: string): Promise<boolean> {
  try {
    const entries = await readdir(root, { recursive: true, withFileTypes: true });
    return entries.some((entry) => !entry.isDirectory());
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return false;
    throw error;
  }
}
