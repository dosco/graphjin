import { mkdtemp, mkdir, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import type { AgentWorkspace } from "../src/agent-backend";
import { discoverRunArtifacts } from "../src/work/artifacts";

describe("discoverRunArtifacts", () => {
  it("publishes regular files under this run and ignores symlinks", async () => {
    const orgRoot = await mkdtemp(join(tmpdir(), "neko-artifacts-"));
    const runRoot = join(orgRoot, "runs", "run-1");
    const artifactRoot = join(runRoot, "artifacts");
    await mkdir(join(artifactRoot, "reports"), { recursive: true });
    await writeFile(join(artifactRoot, "reports", "quote.csv"), "sku,total\n1,10\n");
    await writeFile(join(orgRoot, "private.txt"), "not downloadable");
    await symlink(join(orgRoot, "private.txt"), join(artifactRoot, "private-link.txt"));
    const workspace: AgentWorkspace = {
      orgRoot,
      skillsRoot: join(orgRoot, "skills"),
      memoryRoot: join(orgRoot, "memory"),
      knowledgeRoot: join(orgRoot, "knowledge"),
      uploadsRoot: join(orgRoot, "uploads"),
      runsRoot: join(orgRoot, "runs"),
      threadUploadsRoot: join(orgRoot, "uploads", "thread-1"),
      runRoot,
      artifactRoot,
      binRoot: join(runRoot, "bin"),
    };

    await expect(discoverRunArtifacts(workspace)).resolves.toEqual([
      {
        path: "runs/run-1/artifacts/reports/quote.csv",
        label: "quote.csv",
      },
    ]);
  });
});
