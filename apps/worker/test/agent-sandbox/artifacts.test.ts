import { mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, it } from "vitest";
import { hasArtifacts } from "../../src/agent-sandbox/artifacts";

it("skips empty trees but preserves nested, partial, and linked artifacts", async () => {
  const root = await mkdtemp(join(tmpdir(), "artifact-check-"));
  try {
    expect(await hasArtifacts(join(root, "missing"))).toBe(false);
    await mkdir(join(root, "nested"));
    expect(await hasArtifacts(root)).toBe(false);
    await writeFile(join(root, "nested", "partial.csv"), "");
    expect(await hasArtifacts(root)).toBe(true);
    await expect(hasArtifacts(join(root, "nested", "partial.csv"))).rejects.toThrow();
    await rm(join(root, "nested", "partial.csv"));
    await symlink("missing", join(root, "link"));
    expect(await hasArtifacts(root)).toBe(true);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
