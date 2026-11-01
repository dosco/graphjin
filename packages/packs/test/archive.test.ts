import { mkdtemp, readdir, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { randomBytes } from "node:crypto";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { hashPackFiles, PACK_ARCHIVE_LIMITS, stagePackArchive } from "../src/archive.js";
import { packZipEntries, zipFixture, type ZipEntry } from "./zip-fixture.js";

describe("pack ZIP staging", () => {
  let root: string, entries: ZipEntry[];
  beforeAll(async () => {
    root = await mkdtemp(join(tmpdir(), "pack-archive-"));
    entries = await packZipEntries(fileURLToPath(new URL("../../../apps/worker/test/fixtures/service-health", import.meta.url)));
  });
  afterAll(async () => { await rm(root, { recursive: true, force: true }); });
  it("stages a real declarative pack privately with stable content identity", async () => {
    const first = await stagePackArchive(zipFixture(entries), root);
    const compressed = await stagePackArchive(zipFixture(entries.map(entry => ({ ...entry, deflate: !entry.name.endsWith("/") }))), root);
    try {
      expect(first.bundle.manifest.metadata.id).toBe("service-health");
      expect(first.bundle.bundleHash).toBe(compressed.bundle.bundleHash);
      expect(await hashPackFiles(first.bundle.root)).toBe(await hashPackFiles(compressed.bundle.root));
      expect((await stat(join(first.bundle.root, "pack.yaml"))).mode & 0o777).toBe(0o600);
    } finally { await first.cleanup(); await compressed.cleanup(); }
    expect(await readdir(root)).toEqual([]);
  });
  it("rejects unsafe and corrupt entries and removes partial extraction", async () => {
    const bad: ZipEntry[][] = [
      [{ name: "../outside.md", data: "no" }], [{ name: "/absolute.md" }], [{ name: "service-health/../outside.md" }],
      [{ name: "service-health\\outside.md" }], [{ name: "other/file.md" }],
      [{ name: "service-health/link.md", mode: 0o120777, data: "../../outside" }],
      [{ name: "service-health/fifo.md", mode: 0o010600 }],
      [{ name: "service-health/run.md", mode: 0o100755, data: "executable" }],
      [{ name: "service-health/run.md", data: "#!/bin/sh\nexit 0" }],
      [{ name: "service-health/nested.zip", data: "PK" }], [{ name: "service-health/nested.md", data: "PK archive" }],
      [{ name: "service-health/binary.md", data: Buffer.from([0, 255]) }],
      [{ name: "service-health/duplicate.md" }, { name: "service-health/DUPLICATE.md" }],
      [{ name: "service-health/Folder/a.md" }, { name: "service-health/folder/b.md" }],
      [{ name: "service-health/file.md" }, { name: "service-health/file.md/child.md" }],
      [{ name: "service-health/encrypted.md", flags: 1 }],
      [{ name: "service-health/corrupt.md", data: "hello", crc: 1 }],
      [{ name: "service-health/huge.md", data: "x", size: PACK_ARCHIVE_LIMITS.fileBytes + 1 }],
      [{ name: "service-health/bomb.md", data: "x".repeat(100_000), deflate: true }],
      [{ name: `service-health/${"deep/".repeat(17)}file.md` }],
    ];
    for (const additions of bad) {
      await expect(stagePackArchive(zipFixture([...entries, ...additions]), root)).rejects.toThrow();
      expect(await readdir(root)).toEqual([]);
    }
    await expect(stagePackArchive(Buffer.from("not a zip"), root)).rejects.toThrow();
    await expect(stagePackArchive(zipFixture([]), root)).rejects.toThrow(/empty/);
    await expect(stagePackArchive(zipFixture(entries), root, { reservedIds: ["service-health"] })).rejects.toThrow(/reserved/);
    await expect(stagePackArchive(zipFixture(entries), root, { signal: AbortSignal.abort() })).rejects.toThrow();
    await expect(stagePackArchive(zipFixture(entries.filter(entry => !entry.name.endsWith("pack.yaml"))), root)).rejects.toThrow();
    await expect(stagePackArchive(zipFixture([...entries, ...Array.from({ length: 1001 }, (_, i) => ({ name: `service-health/${i}.md` }))]), root)).rejects.toThrow(/too many entries/);
    expect(await readdir(root)).toEqual([]);
  });
  it("enforces whole-archive and aggregate expansion budgets", async () => {
    await expect(stagePackArchive(Buffer.alloc(PACK_ARCHIVE_LIMITS.compressedBytes + 1), root)).rejects.toThrow(/16 MiB/);
    // Text that compresses moderately: each entry passes the ratio/size bounds,
    // while their combined expansion exceeds the archive-wide budget.
    const data = randomBytes(4 * 1024 * 1024).map(byte => 65 + (byte & 1));
    const archive = zipFixture(Array.from({ length: 17 }, (_, i) => ({ name: `service-health/${i}.md`, data, deflate: true })));
    expect(archive.length).toBeLessThan(PACK_ARCHIVE_LIMITS.compressedBytes);
    await expect(stagePackArchive(archive, root)).rejects.toThrow(/64 MiB/);
    expect(await readdir(root)).toEqual([]);
  }, 30_000);
});
