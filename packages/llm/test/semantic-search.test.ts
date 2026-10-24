// The admin "Semantic search" switch: cached reads, saving, and GraphJin.

import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { parse } from "yaml";

const state = vi.hoisted(() => ({ enabled: false, reads: 0, fail: false, endpoint: "http://graphjin:8080" as string | null }));
vi.mock("@neko/db", () => {
  const chain = {
    select: () => chain,
    from: () => chain,
    where: () => chain,
    orderBy: () => chain,
    limit: async () => (state.endpoint ? [{ graphql_url: state.endpoint }] : []),
  };
  return {
    readSemanticSearchEnabled: vi.fn(async () => {
      state.reads++;
      if (state.fail) throw new Error("db down");
      return state.enabled;
    }),
    writeSemanticSearchEnabled: vi.fn(async () => {}),
    pool: () => ({}),
    db: () => chain,
    data_source: { graphql_url: "graphql_url", org_id: "org_id", is_default: "is_default", created_at: "created_at" },
    desc: (v: unknown) => v,
    eq: () => true,
  };
});

import {
  SEMANTIC_SEARCH_CACHE_MS,
  invalidateSemanticSearchCache,
  semanticSearchEnabled,
  setSemanticSearchEnabledForTests,
} from "../src/semantic-search";
import { saveSemanticSearchSetting } from "../src/semantic-search-settings";
import { graphjinSemanticSearchUpdate, provisionGraphjinSemanticSearch } from "../src/graphjin/semantic-search";

beforeEach(() => {
  setSemanticSearchEnabledForTests(null);
  state.enabled = false;
  state.reads = 0;
  state.fail = false;
  state.endpoint = "http://graphjin:8080";
});

describe("semanticSearchEnabled", () => {
  it("is off by default and caches the database value briefly", async () => {
    expect(await semanticSearchEnabled(1_000)).toBe(false);
    state.enabled = true;
    expect(await semanticSearchEnabled(1_000 + 10)).toBe(false);
    expect(state.reads).toBe(1);
    expect(await semanticSearchEnabled(Date.now() + SEMANTIC_SEARCH_CACHE_MS + 1)).toBe(true);
    expect(state.reads).toBe(2);
  });

  it("re-reads at once after a save invalidates the cache", async () => {
    expect(await semanticSearchEnabled()).toBe(false);
    state.enabled = true;
    invalidateSemanticSearchCache();
    expect(await semanticSearchEnabled()).toBe(true);
  });

  it("treats an unreadable setting as off", async () => {
    state.fail = true;
    expect(await semanticSearchEnabled()).toBe(false);
  });
});

describe("saveSemanticSearchSetting", () => {
  const deps = () => ({
    write: vi.fn(async () => {}),
    backfill: vi.fn(async () => true),
    graphjin: vi.fn(async () => ({ status: "applied" as const })),
  });

  it("turning on saves, queues the backfill and applies GraphJin", async () => {
    const d = deps();
    expect(await saveSemanticSearchSetting("org", true, d)).toEqual({ enabled: true, graphjin: { status: "applied" } });
    expect(d.write).toHaveBeenCalledWith("org", true);
    expect(d.backfill).toHaveBeenCalledTimes(1);
    expect(d.graphjin).toHaveBeenCalledWith("org", true);
  });

  it("turning off schedules no embedding work", async () => {
    const d = deps();
    await saveSemanticSearchSetting("org", false, d);
    expect(d.backfill).not.toHaveBeenCalled();
    expect(d.graphjin).toHaveBeenCalledWith("org", false);
  });

  it("keeps the saved value when GraphJin or the queue is unavailable", async () => {
    const d = deps();
    d.backfill.mockRejectedValueOnce(new Error("pg-boss down"));
    d.graphjin.mockRejectedValueOnce(new Error("GraphJin config preview failed: nope"));
    expect(await saveSemanticSearchSetting("org", true, d)).toEqual({
      enabled: true,
      graphjin: { status: "failed", reason: "GraphJin config preview failed: nope" },
    });
    expect(d.write).toHaveBeenCalled();
  });
});

describe("provisionGraphjinSemanticSearch", () => {
  async function configFile(content = "sources: []\n") {
    const dir = await mkdtemp(join(tmpdir(), "neko-semantic-"));
    const file = join(dir, "agentic.yml");
    await writeFile(file, content);
    return file;
  }

  it("writes the block to the file and applies enabled live", async () => {
    const file = await configFile();
    const apply = vi.fn(async () => "rev");
    expect(await provisionGraphjinSemanticSearch("org", { enabled: true, configFile: file, apply })).toEqual({ status: "applied" });
    expect(parse(await readFile(file, "utf8")).catalog_search.semantic).toMatchObject({
      enabled: true,
      embedding_model: "google/embeddinggemma-2",
      base_url: "http://embedding:5003/v1",
    });
    expect(apply).toHaveBeenCalledWith("http://graphjin:8080", { serv: { catalog_search: { semantic: { enabled: true } } } });
  });

  it("still applies live when the file is already current, and reads the switch when not given", async () => {
    const file = await configFile();
    const apply = vi.fn(async () => "rev");
    await provisionGraphjinSemanticSearch("org", { enabled: false, configFile: file, apply });
    const before = await readFile(file, "utf8");
    await provisionGraphjinSemanticSearch("org", { configFile: file, apply });
    expect(await readFile(file, "utf8")).toBe(before);
    expect(apply).toHaveBeenLastCalledWith("http://graphjin:8080", graphjinSemanticSearchUpdate(false));
    expect(state.reads).toBe(1);
  });

  it("skips without a config file or a GraphJin endpoint", async () => {
    const apply = vi.fn();
    expect((await provisionGraphjinSemanticSearch("org", { enabled: true, configFile: "", apply })).status).toBe("skipped");
    state.endpoint = null;
    expect((await provisionGraphjinSemanticSearch("org", { enabled: true, configFile: await configFile(), apply })).status).toBe("skipped");
    expect(apply).not.toHaveBeenCalled();
  });
});
