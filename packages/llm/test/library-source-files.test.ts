import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  libraryUserSegment,
  librarySourceFilePath,
  librarySourceRoot,
  saveLibrarySourceFile,
} from "../src/library/source-files";

const roots: string[] = [];
afterEach(async () => {
  await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true })));
});

async function root(): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "neko-library-source-"));
  roots.push(dir);
  return dir;
}

describe("Library source files", () => {
  it("stores an upload in the user's folder, content-addressed", async () => {
    const dir = await root();
    const data = Buffer.from("durable library knowledge");
    const saved = await saveLibrarySourceFile({ userId: "user-1", filename: "knowledge.pdf", data, root: dir });
    expect(saved.contentHash).toBe("88b05ad9db9ac17bb7271644404856607de0883623c626fab14fcc61d59f5eb7");
    expect(saved.relativePath).toBe(`neko_library/users/user-1/${saved.contentHash}/knowledge.pdf`);
    await expect(readFile(librarySourceFilePath(saved.relativePath, dir))).resolves.toEqual(data);
  });

  it("does not overwrite an earlier file with the same name", async () => {
    const dir = await root();
    const first = await saveLibrarySourceFile({ userId: "u", filename: "a.pdf", data: Buffer.from("first"), root: dir });
    const second = await saveLibrarySourceFile({ userId: "u", filename: "a.pdf", data: Buffer.from("second"), root: dir });
    expect(first.relativePath).not.toBe(second.relativePath);
    await expect(readFile(librarySourceFilePath(first.relativePath, dir), "utf8")).resolves.toBe("first");
  });

  it("files a run without a user under GraphJin's service subject", async () => {
    const saved = await saveLibrarySourceFile({ userId: null, filename: "a.txt", data: Buffer.from("x"), root: await root() });
    expect(saved.relativePath.startsWith("neko_library/users/service/")).toBe(true);
  });

  it("refuses paths that leave the Library or user ids that name folders", () => {
    expect(() => librarySourceFilePath("neko_library/../secrets.yml", "/data/files/library")).toThrow();
    expect(() => librarySourceFilePath("library/uploads/a.pdf", "/data/files/library")).toThrow();
    expect(() => libraryUserSegment("../bob")).toThrow();
    expect(() => libraryUserSegment("a/b")).toThrow();
  });

  it("lives beside the customer GraphJin config", () => {
    expect(librarySourceRoot("/config/graphjin/agentic.yml")).toBe("/config/graphjin/files/library");
  });
});
