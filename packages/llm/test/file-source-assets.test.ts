import { mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  acquireManagedFileSourceLock,
  assertManagedFileSourceMutable,
  managedFileSourceDirectoryName,
  managedFileSourceLocalDirectory,
  managedFileSourceRuntimeRoot,
  markManagedFileSourceApproved,
  parseManagedFileSourceManifest,
  stageManagedFileSourceFiles,
  verifyManagedFileSourceManifest,
} from "../src/graphjin";

const roots: string[] = [];

afterEach(async () => {
  await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true })));
});

async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "openneko-file-source-"));
  roots.push(root);
  const configFile = join(root, "agentic.yml");
  await writeFile(configFile, "production: true\n");
  return { root, configFile };
}

describe("managed GraphJin file sources", () => {
  it("derives an org-scoped runtime root without exposing the org id", () => {
    const directory = managedFileSourceDirectoryName("private/customer", "documents");
    expect(directory).toMatch(/^org_[a-f0-9]{12}_documents$/);
    expect(directory).not.toContain("private");
    expect(
      managedFileSourceRuntimeRoot({
        orgId: "private/customer",
        sourceName: "documents",
      }),
    ).toBe(`/config/files/${directory}`);
  });

  it("stages bounded files in the generated source directory", async () => {
    const { configFile } = await fixture();
    const result = await stageManagedFileSourceFiles({
      configFile,
      orgId: "acme",
      sourceName: "documents",
      files: [
        {
          name: "quarterly report.csv",
          content: new TextEncoder().encode("quarter,revenue\nQ1,42\n"),
          contentType: "text/csv",
        },
      ],
    });
    expect(result[0]).toMatchObject({
      name: "quarterly report.csv",
      contentType: "text/csv",
      size: 22,
    });
    expect(result[0]?.checksumSha256).toMatch(/^[a-f0-9]{64}$/);
    const directory = managedFileSourceLocalDirectory({
      configFile,
      orgId: "acme",
      sourceName: "documents",
    });
    expect(await readFile(join(directory, "quarterly report.csv"), "utf8")).toContain(
      "Q1,42",
    );
    await expect(
      verifyManagedFileSourceManifest({
        configFile,
        orgId: "acme",
        sourceName: "documents",
        files: result,
      }),
    ).resolves.toBeUndefined();
    await expect(
      verifyManagedFileSourceManifest({
        configFile,
        orgId: "acme",
        sourceName: "documents",
        files: [{ ...result[0]!, checksumSha256: "0".repeat(64) }],
      }),
    ).rejects.toThrow("integrity check");
  });

  it("binds a manifest to its source name, exact total, and safe entries", () => {
    const manifest = {
      sourceName: "documents",
      ready: true,
      files: [
        {
          name: "report.csv",
          size: 12,
          contentType: "text/csv",
          checksumSha256: "a".repeat(64),
        },
      ],
      totalSize: 12,
    } as const;
    expect(parseManagedFileSourceManifest(manifest, "documents")).toEqual(
      manifest,
    );
    expect(() =>
      parseManagedFileSourceManifest(manifest, "other_source"),
    ).toThrow("match the selected source name");
    expect(() =>
      parseManagedFileSourceManifest({ ...manifest, totalSize: 11 }, "documents"),
    ).toThrow("total does not match");
  });

  it("requires the approved manifest to cover every directory entry", async () => {
    const { configFile } = await fixture();
    const files = await stageManagedFileSourceFiles({
      configFile,
      orgId: "acme",
      sourceName: "documents",
      files: [{ name: "report.csv", content: new Uint8Array([1]) }],
    });
    const directory = managedFileSourceLocalDirectory({
      configFile,
      orgId: "acme",
      sourceName: "documents",
    });
    await writeFile(join(directory, "unapproved.csv"), "hidden");
    await expect(
      verifyManagedFileSourceManifest({
        configFile,
        orgId: "acme",
        sourceName: "documents",
        files,
        totalSize: 1,
      }),
    ).rejects.toThrow("approved manifest");
  });

  it("serializes uploads and freezes content after approval", async () => {
    const { configFile } = await fixture();
    const input = {
      configFile,
      orgId: "acme",
      sourceName: "documents",
    };
    const release = await acquireManagedFileSourceLock(input);
    await expect(acquireManagedFileSourceLock(input)).rejects.toThrow(
      "operation in progress",
    );
    await release();
    const secondRelease = await acquireManagedFileSourceLock(input);
    await markManagedFileSourceApproved(input);
    await expect(assertManagedFileSourceMutable(input)).rejects.toThrow(
      "source is active",
    );
    await secondRelease();
  });

  it("rejects paths, duplicate names, and overwrites", async () => {
    const { configFile } = await fixture();
    await expect(
      stageManagedFileSourceFiles({
        configFile,
        orgId: "acme",
        sourceName: "documents",
        files: [{ name: "../agentic.yml", content: new Uint8Array([1]) }],
      }),
    ).rejects.toThrow("may not contain a path");
    await expect(
      stageManagedFileSourceFiles({
        configFile,
        orgId: "acme",
        sourceName: "documents",
        files: [
          { name: "report.csv", content: new Uint8Array([1]) },
          { name: "report.csv", content: new Uint8Array([2]) },
        ],
      }),
    ).rejects.toThrow("duplicate");
    await stageManagedFileSourceFiles({
      configFile,
      orgId: "acme",
      sourceName: "documents",
      files: [{ name: "report.csv", content: new Uint8Array([1]) }],
    });
    await expect(
      stageManagedFileSourceFiles({
        configFile,
        orgId: "acme",
        sourceName: "documents",
        files: [{ name: "report.csv", content: new Uint8Array([2]) }],
      }),
    ).rejects.toThrow("already exists");
  });

  it("rejects a symlink in place of the managed directory", async () => {
    const { root, configFile } = await fixture();
    const localBase = join(root, "files");
    const directory = managedFileSourceLocalDirectory({
      configFile,
      localBase,
      orgId: "acme",
      sourceName: "documents",
    });
    await writeFile(join(root, "outside"), "sensitive");
    await rm(dirname(directory), { recursive: true, force: true });
    await mkdir(dirname(directory), { recursive: true });
    await symlink(join(root, "outside"), directory);
    await expect(
      stageManagedFileSourceFiles({
        configFile,
        localBase,
        orgId: "acme",
        sourceName: "documents",
        files: [{ name: "report.csv", content: new Uint8Array([1]) }],
      }),
    ).rejects.toThrow("regular directory");
  });
});
