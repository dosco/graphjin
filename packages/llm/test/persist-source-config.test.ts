import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { parse } from "yaml";
import {
  acquireGraphjinConfigLock,
  graphjinConfigHasSource,
  persistGraphjinSourceConfigUpdate,
} from "../src/graphjin/persist-source-config";

const roots: string[] = [];

async function configFile(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "openneko-gj-config-"));
  roots.push(root);
  const file = join(root, "agentic.yml");
  await writeFile(
    file,
    [
      "# keep this operator note",
      "production: true",
      "system:",
      "  capabilities:",
      "    config.write: true",
      "sources:",
      "  - name: warehouse",
      "    kind: database",
      "    access:",
      "      read: authenticated",
      "      write: blocked",
      "",
    ].join("\n"),
    { mode: 0o640 },
  );
  return file;
}

afterEach(async () => {
  await Promise.all(
    roots.splice(0).map((root) => rm(root, { recursive: true, force: true })),
  );
});

describe("persistGraphjinSourceConfigUpdate", () => {
  it("detects source names in the durable config", async () => {
    const file = await configFile();
    await expect(
      graphjinConfigHasSource({ configFile: file, sourceName: "warehouse" }),
    ).resolves.toBe(true);
    await expect(
      graphjinConfigHasSource({ configFile: file, sourceName: "documents" }),
    ).resolves.toBe(false);
  });

  it("atomically adds a managed API source while retaining existing config", async () => {
    const file = await configFile();
    await persistGraphjinSourceConfigUpdate({
      configFile: file,
      update: {
        update_sources: [
          {
            name: "pets",
            kind: "api",
            specs_dir: "/config/specs",
            access: {
              read: "authenticated",
              write: "blocked",
              delete: "blocked",
            },
          },
        ],
      },
    });

    const raw = await readFile(file, "utf8");
    const parsed = parse(raw) as { sources: Array<Record<string, unknown>> };
    expect(raw).toContain("# keep this operator note");
    expect(parsed.sources.map((source) => source.name)).toEqual(["warehouse", "pets"]);
    expect(parsed.sources[1]).toMatchObject({
      kind: "api",
      specs_dir: "/config/specs",
    });
    expect((await stat(file)).mode & 0o777).toBe(0o640);
  });

  it("merges access patches into the named durable source", async () => {
    const file = await configFile();
    await persistGraphjinSourceConfigUpdate({
      configFile: file,
      update: {
        source_patches: [
          {
            name: "warehouse",
            access: { read: "admin", delete: "blocked" },
          },
        ],
      },
    });
    const parsed = parse(await readFile(file, "utf8")) as {
      sources: Array<{ name: string; access?: Record<string, string> }>;
    };
    expect(parsed.sources[0]?.access).toEqual({
      read: "admin",
      write: "blocked",
      delete: "blocked",
    });
  });

  it("removes only explicitly named managed sources", async () => {
    const file = await configFile();
    await persistGraphjinSourceConfigUpdate({
      configFile: file,
      update: { remove_sources: ["warehouse"] },
    });
    const parsed = parse(await readFile(file, "utf8")) as {
      sources: Array<{ name: string }>;
    };
    expect(parsed.sources).toEqual([]);
  });

  it("persists GraphJin's deterministic secret reference, not a password", async () => {
    const file = await configFile();
    await persistGraphjinSourceConfigUpdate({
      configFile: file,
      update: {
        update_sources: [
          {
            name: "billing/db",
            kind: "database",
            password: "plain-secret",
          },
        ],
      },
    });
    const raw = await readFile(file, "utf8");
    expect(raw).not.toContain("plain-secret");
    expect(raw).toContain("gjsecret://sources/billing_db/password");
  });

  it("persists nested API credentials using GraphJin's deterministic secret paths", async () => {
    const file = await configFile();
    await persistGraphjinSourceConfigUpdate({
      configFile: file,
      update: {
        update_sources: [
          {
            name: "magento_operator",
            kind: "api",
            specs: {
              "magento-operator-v1": {
                auth: {
                  scheme: "bearer",
                  token: "plain-integration-token",
                },
              },
            },
          },
        ],
      },
    });
    const raw = await readFile(file, "utf8");
    expect(raw).not.toContain("plain-integration-token");
    expect(raw).toContain(
      "gjsecret://sources/magento_operator/specs/magento-operator-v1/auth/token",
    );
  });

  it("serializes writers of the shared durable config", async () => {
    const file = await configFile();
    const release = await acquireGraphjinConfigLock({ configFile: file });
    await expect(
      acquireGraphjinConfigLock({ configFile: file }),
    ).rejects.toThrow(/another approved change/i);
    await release();
    const releaseAgain = await acquireGraphjinConfigLock({ configFile: file });
    await releaseAgain();
  });

});
