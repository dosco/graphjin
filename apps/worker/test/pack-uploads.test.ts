import { cp, mkdir, mkdtemp, readFile, readdir, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { parse, stringify } from "yaml";
import { listUploadedPacks, loadUploadedPack, snapshotUploadedPack, storePackUpload } from "../src/packs/uploads.js";
import { packZipEntries, zipFixture, type ZipEntry } from "../../../packages/packs/test/zip-fixture.js";

describe("immutable uploaded pack storage", () => {
  let root: string, entries: ZipEntry[];
  beforeAll(async () => {
    root = await mkdtemp(join(tmpdir(), "pack-uploads-"));
    entries = await packZipEntries(resolve("test/fixtures/service-health"));
  });
  afterAll(async () => { await rm(root, { recursive: true, force: true }); });
  it("publishes complete versions atomically and preserves identity across duplicate uploads", async () => {
    const directory = join(root, "org-one");
    const input = { root: directory, orgId: "org-one", reservedIds: ["magento"], bytes: zipFixture(entries), actorUserId: "operator" };
    const [first, duplicate] = await Promise.all([storePackUpload(input), storePackUpload(input)]);
    expect(duplicate).toEqual(first);
    const compressed = await storePackUpload({ ...input, bytes: zipFixture(entries.map(entry => ({ ...entry, deflate: !entry.name.endsWith("/") }))) });
    expect(compressed).toEqual(first);
    expect((await listUploadedPacks(directory, "org-one")).map(bundle => bundle.upload)).toEqual([first]);
    expect(await readdir(join(directory, ".staging"))).toEqual([]);
    await expect(loadUploadedPack(directory, "another-org", "service-health")).rejects.toThrow(/organization/);
    await expect(storePackUpload({ ...input, bytes: zipFixture([...entries, { name: "service-health/extra.md", data: "new bytes" }]) })).rejects.toThrow(/immutable/);
    const versionTwo = entries.map(entry => {
      if (!entry.name.endsWith("pack.yaml")) return entry;
      const manifest = parse(String(entry.data)); manifest.metadata.version = "0.2.0";
      return { ...entry, data: stringify(manifest) };
    });
    await storePackUpload({ ...input, bytes: zipFixture(versionTwo) });
    await storePackUpload(input);
    expect((await loadUploadedPack(directory, "org-one", "service-health")).manifest.metadata.version).toBe("0.2.0");
    expect((await loadUploadedPack(directory, "org-one", "service-health", "0.1.0")).upload).toEqual(first);
  });
  it("ignores interrupted staging and recovers a complete orphan on identical retry", async () => {
    const directory = join(root, "interrupted");
    await mkdir(join(directory, ".staging/.upload-interrupted/service-health"), { recursive: true });
    await writeFile(join(directory, ".staging/.upload-interrupted/service-health/pack.yaml"), "partial");
    expect(await listUploadedPacks(directory, "interrupted")).toEqual([]);
    const input = { root: directory, orgId: "interrupted", reservedIds: [], bytes: zipFixture(entries) };
    const uploaded = await storePackUpload(input);
    await rm(join(directory, "service-health/candidate.json"));
    expect(await listUploadedPacks(directory, "interrupted")).toEqual([]);
    expect(await storePackUpload(input)).toEqual(uploaded);
    expect((await listUploadedPacks(directory, "interrupted"))).toHaveLength(1);
  });
  it("revalidates disk contents and installs from a private verified snapshot", async () => {
    const directory = join(root, "org-one");
    const bundle = await loadUploadedPack(directory, "org-one", "service-health");
    const snapshot = await snapshotUploadedPack(directory, bundle);
    try {
      const path = join(bundle.root, "skills/service-health-review/SKILL.md");
      const original = await readFile(path, "utf8");
      await writeFile(path, original + "\nchanged after review");
      expect(await readFile(join(snapshot.bundle.root, "skills/service-health-review/SKILL.md"), "utf8")).toBe(original);
      await expect(loadUploadedPack(directory, "org-one", "service-health")).rejects.toThrow(/contents changed/);
      await expect(snapshotUploadedPack(directory, bundle)).rejects.toThrow(/changed while/);
      await writeFile(path, original);
      await symlink(path, join(bundle.root, "injected.md"));
      await expect(loadUploadedPack(directory, "org-one", "service-health")).rejects.toThrow(/link/);
      await rm(join(bundle.root, "injected.md"));
      const restored = join(root, "copied-config");
      await cp(directory, restored, { recursive: true });
      expect((await loadUploadedPack(restored, "org-one", "service-health")).upload).toEqual(bundle.upload);
    } finally { await snapshot.cleanup(); }
    expect((await readdir(directory)).filter(name => name.startsWith(".apply-"))).toEqual([]);
  });
});
