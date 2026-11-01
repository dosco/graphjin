import { afterEach, describe, expect, it, vi } from "vitest";

async function freshSlots() {
  vi.resetModules();
  return import("../src/work/agent-slots");
}

describe("agent slots", () => {
  afterEach(() => vi.unstubAllEnvs());

  it("lets two Ask questions run on the web and queues the third in order", async () => {
    vi.stubEnv("OPENNEKO_SANDBOX_OWNER", "web");
    vi.stubEnv("OPENNEKO_AGENT_CONCURRENCY", "");
    const slots = await freshSlots();
    const first = await slots.acquireAgentSlot();
    const second = await slots.acquireAgentSlot();
    const onWait = vi.fn();
    const order: string[] = [];
    const third = slots.acquireAgentSlot({ onWait }).then((release) => { order.push("third"); return release; });
    const fourth = slots.acquireAgentSlot().then((release) => { order.push("fourth"); return release; });
    await Promise.resolve();
    expect(onWait).toHaveBeenCalledOnce();
    expect(slots.agentSlotUsage()).toEqual({ active: 2, waiting: 2, limit: 2 });
    first();
    (await third)();
    second();
    (await fourth)();
    expect(order).toEqual(["third", "fourth"]);
    expect(slots.agentSlotUsage()).toEqual({ active: 0, waiting: 0, limit: 2 });
  });

  it("uses the worker cap and a configured override", async () => {
    vi.stubEnv("OPENNEKO_SANDBOX_OWNER", "openneko-worker");
    let slots = await freshSlots();
    expect(slots.agentSlotUsage().limit).toBe(3);
    slots.setAgentSlotLimit(5);
    expect(slots.agentSlotUsage().limit).toBe(5);
    vi.stubEnv("OPENNEKO_AGENT_CONCURRENCY", "1");
    slots = await freshSlots();
    expect(slots.agentSlotUsage().limit).toBe(1);
  });

  it("drops a waiting run when it is cancelled", async () => {
    vi.stubEnv("OPENNEKO_AGENT_CONCURRENCY", "1");
    const slots = await freshSlots();
    const held = await slots.acquireAgentSlot();
    const controller = new AbortController();
    const waiting = slots.acquireAgentSlot({ signal: controller.signal });
    controller.abort(new Error("stopped"));
    await expect(waiting).rejects.toThrow("stopped");
    held();
    expect(slots.agentSlotUsage()).toEqual({ active: 0, waiting: 0, limit: 1 });
  });

  it("refreshes a waiting run so the stale-run sweep keeps it", async () => {
    vi.useFakeTimers();
    try {
      vi.stubEnv("OPENNEKO_AGENT_CONCURRENCY", "1");
      const slots = await freshSlots();
      const held = await slots.acquireAgentSlot();
      const heartbeat = vi.fn().mockResolvedValue(undefined);
      const waiting = slots.acquireAgentSlot({ heartbeat });
      await vi.advanceTimersByTimeAsync(125_000);
      expect(heartbeat).toHaveBeenCalledTimes(2);
      held();
      (await waiting)();
      await vi.advanceTimersByTimeAsync(120_000);
      expect(heartbeat).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });
});
