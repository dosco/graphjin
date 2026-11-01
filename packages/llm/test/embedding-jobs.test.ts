// The embedding backlog job: switch gating, batching (≤ 64 per request),
// backoff on busy/unavailable, and isolation of a row the service rejects.
// Postgres is faked: rows live in memory and queries are matched by shape.

import { beforeEach, describe, expect, it, vi } from "vitest";

type Memory = { id: string; org_id: string; text: string; embedding: string | null };
type Skill = { id: string; org_id: string; skill_name: string; document: string; embedding: string | null };

const state = vi.hoisted(() => ({
  memories: [] as Memory[],
  skills: [] as Skill[],
  queries: [] as string[],
  sends: [] as Array<{ name: string; data: unknown }>,
  pending: false,
  enabled: true,
}));

function fakeQuery(text: string, values: unknown[] = []) {
  state.queries.push(text);
  if (/pg_try_advisory(_xact)?_lock/.test(text)) return { rows: [{ acquired: true }] };
  if (/pg_advisory_unlock|BEGIN|COMMIT|ROLLBACK/.test(text)) return { rows: [] };
  if (/FROM pgboss\.job/.test(text)) return { rows: state.pending ? [{}] : [] };
  if (/AS pending/.test(text)) {
    return { rows: [{ pending: state.memories.some((m) => !m.embedding) || state.skills.some((s) => !s.embedding) }] };
  }
  if (/AS waiting/.test(text)) {
    return { rows: [{ waiting: state.memories.filter((m) => !m.embedding).length + state.skills.filter((s) => !s.embedding).length }] };
  }
  const [limit, skipped] = values as [number, string[]];
  if (/^SELECT id, org_id, text FROM work_memory/.test(text.trim())) {
    return { rows: state.memories.filter((m) => !m.embedding && !skipped.includes(m.id)).slice(0, limit).map((m) => ({ ...m })) };
  }
  if (/^SELECT id, org_id, skill_name, document FROM skill_embedding/.test(text.trim())) {
    return { rows: state.skills.filter((s) => !s.embedding && !skipped.includes(s.id)).slice(0, limit).map((s) => ({ ...s })) };
  }
  if (/^UPDATE work_memory/.test(text.trim())) {
    const [, id, vector, content] = values as string[];
    const row = state.memories.find((m) => m.id === id && !m.embedding && m.text === content);
    if (row) row.embedding = vector!;
    return { rows: [] };
  }
  if (/^UPDATE skill_embedding/.test(text.trim())) {
    const [, id, vector, content] = values as string[];
    const row = state.skills.find((s) => s.id === id && !s.embedding && s.document === content);
    if (row) row.embedding = vector!;
    return { rows: [] };
  }
  throw new Error(`unexpected query: ${text}`);
}

const connect = vi.fn(async () => ({ query: vi.fn(async (t: string, v?: unknown[]) => fakeQuery(t, v)), release: vi.fn() }));
vi.mock("@neko/db", () => ({
  pool: () => ({ connect, query: vi.fn(async (t: string, v?: unknown[]) => fakeQuery(t, v)) }),
  readSemanticSearchEnabled: vi.fn(async () => state.enabled),
}));
vi.mock("@neko/db/jobs", () => ({
  QUEUE: { EMBEDDING_INDEX: "embedding_index" },
  boss: async () => ({
    createQueue: vi.fn(async () => {}),
    send: vi.fn(async (name: string, data: unknown) => {
      state.sends.push({ name, data });
      return "job";
    }),
  }),
}));

import { EmbeddingServiceError } from "../src/embedding";
import {
  EMBEDDING_BACKOFF_MS,
  dispatchEmbeddingJobs,
  requestEmbeddingBackfill,
  runEmbeddingIndexJob,
  withEmbeddingBackoff,
} from "../src/embedding-jobs";
import { setSemanticSearchEnabledForTests } from "../src/semantic-search";

const vector = (n: number) => Array(768).fill(n);
const busy = () => new EmbeddingServiceError("embedding service returned HTTP 429", 429, true);

function deps(overrides: Record<string, unknown> = {}) {
  return {
    enabled: vi.fn(async () => true),
    embedMemories: vi.fn(async (texts: string[]) => texts.map(() => vector(0.1))),
    embedSkills: vi.fn(async (skills: unknown[]) => skills.map(() => vector(0.2))),
    sleep: vi.fn(async () => {}),
    now: () => 0,
    ...overrides,
  };
}

beforeEach(() => {
  state.memories = Array.from({ length: 150 }, (_, i) => ({ id: `m${i}`, org_id: "o", text: `memory ${i}`, embedding: null }));
  state.skills = Array.from({ length: 3 }, (_, i) => ({ id: `s${i}`, org_id: "o", skill_name: `skill${i}`, document: `skill${i}\nDoes ${i}`, embedding: null }));
  state.queries = [];
  state.sends = [];
  state.pending = false;
  state.enabled = true;
  connect.mockClear();
  setSemanticSearchEnabledForTests(null);
});

describe("runEmbeddingIndexJob", () => {
  it("does nothing while semantic search is off and keeps the backlog", async () => {
    const d = deps({ enabled: vi.fn(async () => false) });
    expect(await runEmbeddingIndexJob(undefined, d)).toEqual({ embedded: 0, remaining: false });
    expect(d.embedMemories).not.toHaveBeenCalled();
    expect(d.embedSkills).not.toHaveBeenCalled();
    expect(connect).not.toHaveBeenCalled();
    expect(state.memories.every((m) => m.embedding === null)).toBe(true);
  });

  it("embeds memories and skills in batches of at most 64", async () => {
    const d = deps();
    expect(await runEmbeddingIndexJob(undefined, d)).toEqual({ embedded: 153, remaining: false });
    expect(d.embedMemories.mock.calls.map(([texts]) => texts.length)).toEqual([64, 64, 22]);
    expect(d.embedSkills.mock.calls.map(([skills]) => skills.length)).toEqual([3]);
    expect(d.embedSkills.mock.calls[0]![0][0]).toEqual({ name: "skill0", document: "skill0\nDoes 0" });
    expect(state.memories.every((m) => m.embedding?.startsWith("[0.1,"))).toBe(true);
    expect(state.skills.every((s) => s.embedding?.startsWith("[0.2,"))).toBe(true);
    expect(state.queries.some((q) => q.includes("pg_advisory_unlock"))).toBe(true);
  });

  it("backs off on 429/503 and then succeeds", async () => {
    state.memories = state.memories.slice(0, 2);
    const d = deps();
    d.embedMemories
      .mockRejectedValueOnce(busy())
      .mockRejectedValueOnce(new EmbeddingServiceError("embedding service returned HTTP 503", 503, true));
    expect((await runEmbeddingIndexJob(undefined, d)).embedded).toBe(5);
    expect(d.sleep.mock.calls.map(([ms]) => ms)).toEqual([EMBEDDING_BACKOFF_MS[0], EMBEDDING_BACKOFF_MS[1]]);
  });

  it("gives up after the backoff schedule so pg-boss retries the job, keeping the rows pending", async () => {
    state.memories = state.memories.slice(0, 2);
    const d = deps({ embedMemories: vi.fn(async () => { throw busy(); }) });
    await expect(runEmbeddingIndexJob(undefined, d)).rejects.toThrow(/HTTP 429/);
    expect(d.sleep).toHaveBeenCalledTimes(EMBEDDING_BACKOFF_MS.length);
    expect(state.memories.every((m) => m.embedding === null)).toBe(true);
  });

  it("stops when semantic search is turned off during backoff", async () => {
    state.memories = state.memories.slice(0, 2);
    const enabled = vi.fn(async () => true).mockResolvedValueOnce(true).mockResolvedValueOnce(true).mockResolvedValue(false);
    const d = deps({ enabled, embedMemories: vi.fn(async () => { throw busy(); }) });
    expect(await runEmbeddingIndexJob(undefined, d)).toMatchObject({ remaining: true });
    expect(d.sleep).toHaveBeenCalledTimes(1);
  });

  it("isolates a row the service rejects so the rest of the batch is indexed", async () => {
    state.memories = state.memories.slice(0, 3);
    state.skills = [];
    const d = deps({
      embedMemories: vi.fn(async (texts: string[]) => {
        if (texts.includes("memory 1")) throw new EmbeddingServiceError("embedding service returned HTTP 400", 400, false);
        return texts.map(() => vector(0.1));
      }),
    });
    expect(await runEmbeddingIndexJob(undefined, d)).toEqual({ embedded: 2, remaining: false });
    expect(state.memories.map((m) => m.embedding !== null)).toEqual([true, false, true]);
  });

  it("never writes a vector for content that changed while it was embedded", async () => {
    state.memories = state.memories.slice(0, 1);
    state.skills = [];
    const d = deps({
      embedMemories: vi.fn(async (texts: string[]) => {
        state.memories[0]!.text = "edited";
        return texts.map(() => vector(0.1));
      }),
    });
    let calls = 0;
    d.enabled = vi.fn(async () => ++calls < 4);
    await runEmbeddingIndexJob(undefined, d);
    expect(d.embedMemories.mock.calls[0]![0]).toEqual(["memory 0"]);
    expect(d.embedMemories.mock.calls[1]![0]).toEqual(["edited"]);
  });

  it("stops at the time budget and leaves the rest to the next job", async () => {
    let t = 0;
    const d = deps({ now: () => (t += 200_000), budgetMs: 300_000 });
    expect(await runEmbeddingIndexJob(undefined, d)).toEqual({ embedded: 67, remaining: true });
  });
});

describe("withEmbeddingBackoff", () => {
  it("does not retry a request the service rejected", async () => {
    const sleep = vi.fn(async () => {});
    const call = vi.fn(async () => { throw new EmbeddingServiceError("HTTP 400", 400, false); });
    await expect(withEmbeddingBackoff(call, { sleep, enabled: async () => true })).rejects.toThrow("HTTP 400");
    expect(call).toHaveBeenCalledTimes(1);
    expect(sleep).not.toHaveBeenCalled();
  });
});

describe("dispatchEmbeddingJobs", () => {
  it("schedules nothing while semantic search is off", async () => {
    setSemanticSearchEnabledForTests(false);
    await dispatchEmbeddingJobs();
    expect(state.sends).toEqual([]);
    expect(state.queries).toEqual([]);
  });

  it("queues one backlog job when on and none is pending", async () => {
    setSemanticSearchEnabledForTests(true);
    await dispatchEmbeddingJobs();
    expect(state.sends).toEqual([{ name: "embedding_index", data: { reason: "sweep" } }]);
    state.pending = true;
    await dispatchEmbeddingJobs();
    expect(state.sends).toHaveLength(1);
  });

  it("queues nothing when the backlog is empty", async () => {
    setSemanticSearchEnabledForTests(true);
    state.memories = [];
    state.skills = [];
    await dispatchEmbeddingJobs();
    expect(state.sends).toEqual([]);
  });

  it("requestEmbeddingBackfill reports whether it queued", async () => {
    expect(await requestEmbeddingBackfill("enabled")).toBe(true);
    state.pending = true;
    expect(await requestEmbeddingBackfill("enabled")).toBe(false);
  });
});
