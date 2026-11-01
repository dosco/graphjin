import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentEvent } from "../src/agent-backend";

// output-server only needs emitWorkflowOutput from ./store for this seam; mock
// it so the hook is exercised without a DB.
vi.mock("../src/workflows/store", () => ({
  emitWorkflowOutput: vi.fn(async (a: { kind: string; title: string; body: string; mood?: string | null }) => ({
    id: "out-1",
    kind: a.kind,
    title: a.title,
    body: a.body,
    mood: a.mood ?? null,
    payload: {},
  })),
}));

import {
  handleWorkflowOutput,
  setWorkflowOutputDeliveryHook,
} from "../src/workflows/output-server";

afterEach(() => setWorkflowOutputDeliveryHook(null));

const ctx = (emit: (e: AgentEvent) => void) => ({
  orgId: "org-1",
  workflowRunId: "wr-1",
  workRunId: "run-1",
  emit,
});

describe("workflow output delivery hook", () => {
  it("fires the registered hook after an output is emitted", async () => {
    const calls: Array<{ orgId: string; outputId: string }> = [];
    setWorkflowOutputDeliveryHook((orgId, output) => {
      calls.push({ orgId, outputId: output.id });
    });
    const events: AgentEvent[] = [];
    await handleWorkflowOutput(ctx((e) => events.push(e)), {
      kind: "finding",
      title: "Active customers up",
      body: "B",
      payload: {},
      mood: "good",
    } as never);
    // hook is fire-and-forget — flush microtasks
    await new Promise((r) => setTimeout(r, 0));
    expect(calls).toEqual([{ orgId: "org-1", outputId: "out-1" }]);
    expect(events.some((e) => e.type === "output_emit")).toBe(true);
  });

  it("does not fail the run if the hook throws", async () => {
    setWorkflowOutputDeliveryHook(() => {
      throw new Error("delivery boom");
    });
    await expect(
      handleWorkflowOutput(ctx(() => {}), {
        kind: "finding",
        title: "T",
        body: "B",
        payload: {},
      } as never),
    ).resolves.toMatchObject({ id: "out-1" });
  });

  it("no hook registered → emit still works", async () => {
    setWorkflowOutputDeliveryHook(null);
    const events: AgentEvent[] = [];
    await handleWorkflowOutput(ctx((e) => events.push(e)), {
      kind: "finding",
      title: "T",
      body: "B",
      payload: {},
    } as never);
    expect(events.some((e) => e.type === "output_emit")).toBe(true);
  });
});
