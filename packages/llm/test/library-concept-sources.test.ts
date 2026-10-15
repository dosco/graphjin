import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { copyTeamConceptSources } from "../src/library/staging";

const roots: string[] = [];
afterEach(async () => {
  for (const root of roots.splice(0)) await rm(root, { recursive: true, force: true });
});

const concept = (resource: string) =>
  `---\ntype: Report\ntitle: "Price list"\nsources:\n  - resource: ${resource}\n---\n\nBody.\n`;

describe("copyTeamConceptSources", () => {
  it("copies the uploads that staged concepts name as sources, and nothing else", async () => {
    const base = await mkdtemp(join(tmpdir(), "concept-sources-"));
    roots.push(base);
    const org = join(base, "org");
    const stage = join(base, "stage");
    await mkdir(join(org, "library/uploads/u1/abc"), { recursive: true });
    await writeFile(join(org, "library/uploads/u1/abc/prices.xlsx"), "sheet");
    await writeFile(join(org, "secret.txt"), "secret");
    await mkdir(join(stage, "library/okf/sales"), { recursive: true });
    await writeFile(join(stage, "library/okf/sales/prices.md"), concept("/library/uploads/u1/abc/prices.xlsx"));
    await writeFile(join(stage, "library/okf/sales/escape.md"), concept("/library/uploads/../../secret.txt"));
    await writeFile(join(stage, "library/okf/sales/other.md"), concept("/memory/notes.md"));

    const copied = await copyTeamConceptSources(join(stage, "library/okf"), org, stage);

    expect(copied).toEqual(["library/uploads/u1/abc/prices.xlsx"]);
    expect(await readFile(join(stage, "library/uploads/u1/abc/prices.xlsx"), "utf8")).toBe("sheet");
    await expect(stat(join(stage, "secret.txt"))).rejects.toThrow();
  });
});
