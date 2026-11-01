import { mkdtemp, readdir, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { KNOWLEDGE_FILES, readKnowledgeSnapshot, refreshKnowledgeSnapshot } from "../src/knowledge-cache";
import { knowledgePackPaths, readKnowledgePack } from "../src/knowledge-pack";

let root: string;
beforeEach(async () => { root = await mkdtemp(join(tmpdir(), "knowledge-cache-test-")); });
afterEach(async () => { await rm(root, { recursive: true, force: true }); });

function fixture() {
  const state = { revision: "r1", fail: false };
  const revision = vi.fn(async () => state.revision);
  const build = vi.fn(async (directory: string) => {
    if (state.fail) return { ok: false, files: [], error: "unreachable" };
    await Promise.all(KNOWLEDGE_FILES.map(file => writeFile(join(directory, file),
      file === "INDEX.md" ? "index" : file === "mode.json" ? '{"mode":"agentic"}' : JSON.stringify({ revision: state.revision }),
    )));
    return { ok: true, files: [] };
  });
  return { state, args: { root, source: "org/source/service/v1", mode: "agentic", revision, build } };
}

it("shares cold preparation, serves warm snapshots without GraphJin, and replaces only changed revisions", async () => {
  const { args, state } = fixture();
  expect((await Promise.all([refreshKnowledgeSnapshot(args), refreshKnowledgeSnapshot(args)])).every(r => r.ok)).toBe(true);
  expect(args.build).toHaveBeenCalledTimes(1);
  args.revision.mockClear();
  await refreshKnowledgeSnapshot(args);
  expect(args.revision).not.toHaveBeenCalled();
  await refreshKnowledgeSnapshot({ ...args, refresh: true });
  expect(args.build).toHaveBeenCalledTimes(1);
  state.revision = "r2";
  await refreshKnowledgeSnapshot({ ...args, refresh: true });
  expect(args.build).toHaveBeenCalledTimes(2);
  const pack = await readKnowledgePack(knowledgePackPaths(root));
  expect(pack.mode).toBe("agentic");
  expect(pack.tables).toContain("r2");
  expect(await readdir(root)).toEqual([".knowledge-snapshot.json"]);
});

it("serves the last complete snapshot while a background replacement is in flight", async () => {
  const { args, state } = fixture();
  await refreshKnowledgeSnapshot(args);
  state.revision = "r2";
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  let started!: () => void;
  const starting = new Promise<void>(resolve => { started = resolve; });
  const build = args.build;
  const refresh = refreshKnowledgeSnapshot({ ...args, refresh: true,
    build: async dir => { started(); await gate; return build(dir); },
  });
  await starting;
  expect((await refreshKnowledgeSnapshot(args)).ok).toBe(true);
  expect((await readKnowledgeSnapshot(root))?.revision).toBe("r1");
  release();
  expect((await refresh).ok).toBe(true);
  expect((await readKnowledgeSnapshot(root))?.revision).toBe("r2");
});

it("keeps a good snapshot on refresh failure but does not reuse another source's knowledge", async () => {
  const { args, state } = fixture();
  await refreshKnowledgeSnapshot(args);
  state.revision = "r2";
  state.fail = true;
  expect((await refreshKnowledgeSnapshot({ ...args, refresh: true })).ok).toBe(false);
  expect((await readKnowledgeSnapshot(root))?.revision).toBe("r1");
  expect((await refreshKnowledgeSnapshot({ ...args, source: "different-source" })).ok).toBe(false);
  expect((await readKnowledgePack(knowledgePackPaths(root))).tables).toBe("{}");
});

it("discards a build when the graph recompiles during fetching and retries the new revision", async () => {
  const { args, state } = fixture();
  const build = args.build;
  let first = true;
  expect((await refreshKnowledgeSnapshot({ ...args, build: async dir => {
    const result = await build(dir);
    if (first) { first = false; state.revision = "r2"; }
    return result;
  } })).ok).toBe(true);
  expect(build).toHaveBeenCalledTimes(2);
  expect((await readKnowledgeSnapshot(root))?.revision).toBe("r2");
  expect((await readKnowledgePack(knowledgePackPaths(root))).tables).toContain("r2");
});
