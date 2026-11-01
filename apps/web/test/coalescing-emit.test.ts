import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentEvent } from "@neko/llm";
import {
  createCoalescingEmit,
  type CoalescingEmitDeps,
} from "@/lib/coalescing-emit";

type PersistedRow = {
  id: number;
  event: AgentEvent;
};

function makeHarness(flushIdleMs = 100) {
  const persisted: PersistedRow[] = [];
  const notified: Array<{ id: number; event: AgentEvent }> = [];
  let nextId = 0;
  const persistEvent = vi.fn(async (args: { event: AgentEvent }) => {
    nextId += 1;
    persisted.push({ id: nextId, event: args.event });
    return nextId;
  }) as unknown as CoalescingEmitDeps["persistEvent"];
  const notify = vi.fn((_runId: string, event: AgentEvent, id: number) => {
    notified.push({ id, event });
  });
  const { emit, finalize } = createCoalescingEmit(
    {
      orgId: "org_test",
      threadId: "thr_test",
      runId: "run_test",
      flushIdleMs,
    },
    { persistEvent, notify },
  );
  return { persisted, notified, emit, finalize };
}

describe("createCoalescingEmit", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("coalesces consecutive message deltas into one row on flush", async () => {
    const h = makeHarness();

    await h.emit({ type: "message", role: "assistant", content: "Hello " });
    await h.emit({ type: "message", role: "assistant", content: "there, " });
    await h.emit({ type: "message", role: "assistant", content: "Amit." });

    expect(h.persisted).toHaveLength(0);

    await h.finalize();

    expect(h.persisted).toHaveLength(1);
    expect(h.persisted[0].id).toBe(1);
    expect(h.persisted[0].event).toEqual({
      type: "message",
      role: "assistant",
      content: "Hello there, Amit.",
    });
  });

  it("flushes the message buffer before persisting a non-message event", async () => {
    const h = makeHarness();

    await h.emit({ type: "message", role: "assistant", content: "narrating" });
    await h.emit({ type: "tool_start", id: "t1", name: "Bash" });
    await h.emit({ type: "message", role: "assistant", content: "after tool" });
    await h.finalize();

    expect(h.persisted.map((r) => r.event.type)).toEqual([
      "message",
      "tool_start",
      "message",
    ]);
    expect(h.persisted[0].event).toMatchObject({ content: "narrating" });
    expect(h.persisted[2].event).toMatchObject({ content: "after tool" });
    expect(h.persisted.map((r) => r.id)).toEqual([1, 2, 3]);
  });

  it("flushes idle buffer when the timer fires", async () => {
    const h = makeHarness(100);

    await h.emit({ type: "message", role: "assistant", content: "first " });
    await h.emit({ type: "message", role: "assistant", content: "burst." });
    expect(h.persisted).toHaveLength(0);

    await vi.advanceTimersByTimeAsync(100);

    expect(h.persisted).toHaveLength(1);
    expect(h.persisted[0].event).toMatchObject({ content: "first burst." });

    // After flush, new deltas start a fresh buffer / row.
    await h.emit({ type: "message", role: "assistant", content: "second " });
    await h.emit({ type: "message", role: "assistant", content: "burst." });
    await h.finalize();

    expect(h.persisted).toHaveLength(2);
    expect(h.persisted[1].event).toMatchObject({ content: "second burst." });
  });

  it("does not let a later event overtake a slow idle flush", async () => {
    const persisted: AgentEvent[] = [];
    let releaseFirstPersist: (() => void) | undefined;
    let firstPersistStarted: (() => void) | undefined;
    const started = new Promise<void>((resolve) => {
      firstPersistStarted = resolve;
    });
    const gate = new Promise<void>((resolve) => {
      releaseFirstPersist = resolve;
    });
    const { emit, finalize } = createCoalescingEmit(
      {
        orgId: "org_test",
        threadId: "thr_test",
        runId: "run_test",
        flushIdleMs: 100,
      },
      {
        persistEvent: (async (args: { event: AgentEvent }) => {
          if (args.event.type === "message") {
            firstPersistStarted?.();
            await gate;
          }
          persisted.push(args.event);
          return persisted.length;
        }) as CoalescingEmitDeps["persistEvent"],
        notify: () => {},
      },
    );

    await emit({ type: "message", role: "assistant", content: "answer" });
    vi.advanceTimersByTime(100);
    await started;
    const laterEmit = emit({ type: "done", result: { status: "completed" } });
    await Promise.resolve();
    expect(persisted).toEqual([]);

    releaseFirstPersist?.();
    await laterEmit;
    await finalize();

    expect(persisted.map((event) => event.type)).toEqual(["message", "done"]);
  });

  it("flushes prior buffer when role flips", async () => {
    const h = makeHarness();

    await h.emit({ type: "message", role: "assistant", content: "agent says" });
    await h.emit({ type: "message", role: "user", content: "user says" });
    await h.finalize();

    expect(h.persisted).toHaveLength(2);
    expect(h.persisted[0].event).toEqual({
      type: "message",
      role: "assistant",
      content: "agent says",
    });
    expect(h.persisted[1].event).toEqual({
      type: "message",
      role: "user",
      content: "user says",
    });
  });

  it("persists non-message events without buffering", async () => {
    const h = makeHarness();

    await h.emit({ type: "status", message: "ready" });
    await h.emit({ type: "tool_start", id: "t1", name: "Bash" });
    await h.emit({ type: "tool_end", id: "t1", result: "ok" });

    expect(h.persisted).toHaveLength(3);
    expect(h.persisted.map((r) => r.event.type)).toEqual([
      "status",
      "tool_start",
      "tool_end",
    ]);
  });

  it("finalize is safe to call when buffer is empty", async () => {
    const h = makeHarness();
    await h.finalize();
    expect(h.persisted).toHaveLength(0);
  });

  it("notifies live subscribers with the same id as DB rows", async () => {
    const h = makeHarness();

    await h.emit({ type: "message", role: "assistant", content: "hi" });
    await h.emit({ type: "status", message: "thinking" });
    await h.finalize();

    expect(h.notified.map((n) => n.id)).toEqual(h.persisted.map((r) => r.id));
    expect(h.notified.map((n) => n.event.type)).toEqual([
      "message",
      "status",
    ]);
  });

  it("skips persisting an empty message buffer (no orphan rows)", async () => {
    const h = makeHarness();
    await h.emit({ type: "message", role: "assistant", content: "" });
    await h.finalize();
    expect(h.persisted).toHaveLength(0);
  });
});
