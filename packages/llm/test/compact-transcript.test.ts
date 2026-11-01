import { describe, expect, it, vi } from "vitest";
import {
  COMPACT_AFTER,
  KEEP_LAST,
  compactor,
  compactIfNeeded,
} from "../src/work/compact-transcript";

const mk = (n: number) =>
  Array.from({ length: n }, (_, i) => ({
    id: `m${i}`,
    role: (i % 2 === 0 ? "user" : "assistant") as "user" | "assistant",
    content: `turn ${i}`,
  }));

const NOW = "2026-06-15T00:00:00.000Z";

describe("compactIfNeeded", () => {
  it("is a no-op at or under the threshold (no summarize call)", async () => {
    const summarize = vi.fn(async () => "S");
    const out = await compactIfNeeded({ messages: mk(COMPACT_AFTER), now: NOW, summarize });
    expect(summarize).not.toHaveBeenCalled();
    expect(out.summary).toBeUndefined();
    expect(out.newCompaction).toBeUndefined();
    expect(out.kept).toHaveLength(COMPACT_AFTER);
  });

  it("folds the oldest and keeps the last KEEP_LAST when over threshold", async () => {
    const n = COMPACT_AFTER + 10;
    const summarize = vi.fn(async () => "SUMMARY");
    const out = await compactIfNeeded({ messages: mk(n), now: NOW, summarize });
    expect(summarize).toHaveBeenCalledTimes(1);
    expect(summarize.mock.calls[0][0]).toBe(""); // no prior summary folded in
    expect(out.summary).toBe("SUMMARY");
    expect(out.kept).toHaveLength(KEEP_LAST);
    expect(out.kept[0].id).toBe(`m${n - KEEP_LAST}`);
    expect(out.newCompaction).toMatchObject({
      summary: "SUMMARY",
      throughMessageId: `m${n - KEEP_LAST - 1}`,
      version: 1,
    });
  });

  it("only folds messages after a prior watermark", async () => {
    const summarize = vi.fn(async () => "S2");
    const prior = { summary: "PRIOR", throughMessageId: "m20", updatedAt: NOW, version: 1 as const };
    const out = await compactIfNeeded({ messages: mk(50), prior, now: NOW, summarize });
    expect(summarize).not.toHaveBeenCalled(); // 29 pending ≤ threshold
    expect(out.summary).toBe("PRIOR");
    expect(out.kept[0].id).toBe("m21");
    expect(out.kept).toHaveLength(29);
    expect(out.newCompaction).toBeUndefined();
  });

  it("re-compacts, folding the prior summary plus the new chunk", async () => {
    const summarize = vi.fn(async () => "S3");
    const prior = { summary: "PRIOR", throughMessageId: "m5", updatedAt: NOW, version: 1 as const };
    const out = await compactIfNeeded({ messages: mk(60), prior, now: NOW, summarize });
    expect(summarize).toHaveBeenCalledTimes(1);
    expect(summarize.mock.calls[0][0]).toBe("PRIOR");
    expect(out.kept).toHaveLength(KEEP_LAST);
    expect(out.kept[0].id).toBe(`m${60 - KEEP_LAST}`);
    expect(out.newCompaction?.throughMessageId).toBe(`m${60 - KEEP_LAST - 1}`);
  });

  it("ignores a stale prior watermark (id absent) and refolds from scratch", async () => {
    const summarize = vi.fn(async () => "S4");
    const prior = { summary: "PRIOR", throughMessageId: "gone", updatedAt: NOW, version: 1 as const };
    const out = await compactIfNeeded({ messages: mk(50), prior, now: NOW, summarize });
    expect(summarize).toHaveBeenCalledTimes(1);
    expect(summarize.mock.calls[0][0]).toBe(""); // stale prior dropped
    expect(out.kept).toHaveLength(KEEP_LAST);
  });
});

describe("compactor signature", () => {
  it("accepts a first fold with no prior summary", () => {
    const fields = compactor.getSignature().getInputFields();
    expect(fields.find((f) => f.name === "priorSummary")?.isOptional).toBe(true);
    expect(fields.find((f) => f.name === "transcript")?.isOptional).toBeFalsy();
  });
});
