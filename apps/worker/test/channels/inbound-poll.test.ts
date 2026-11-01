import { beforeEach, describe, expect, it, vi } from "vitest";

// runPollIteration owns the restart-safety invariant: advance + persist the
// cursor only when the whole batch settled, so a still-retrying batch is
// re-polled (the ledger skips what already settled) and the caller backs off.
// Mock the dispatch + cursor seams so that decision is tested in isolation.
vi.mock("../../src/channels/delivery.js", () => ({
  processInboundUpdate: vi.fn(async () => true),
}));
vi.mock("../../src/channels/inbound-store.js", () => ({
  savePollCursor: vi.fn(async () => {}),
  loadPollCursor: vi.fn(async () => undefined),
  pruneInboundDedup: vi.fn(async () => 0),
}));
vi.mock("../../src/channels/runtime.js", () => ({
  getChannelProviders: vi.fn(async () => []),
  pollChannelInbound: vi.fn(),
}));

import { processInboundUpdate } from "../../src/channels/delivery.js";
import { savePollCursor } from "../../src/channels/inbound-store.js";
import { runPollIteration } from "../../src/channels/inbound-poll";

const ORG = "org-1";
const PLUGIN = "telegram";

function poller(updates: unknown[], next?: string) {
  return { pollInbound: vi.fn(async () => ({ updates, ...(next ? { cursor: next } : {}) })) };
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(processInboundUpdate).mockResolvedValue(true);
});

describe("runPollIteration — cursor discipline", () => {
  it("advances + persists the cursor when every update settles cleanly", async () => {
    const p = poller([{ update_id: 1 }, { update_id: 2 }], "C2");
    const res = await runPollIteration(ORG, PLUGIN, "C1", p);
    expect(processInboundUpdate).toHaveBeenCalledTimes(2);
    expect(savePollCursor).toHaveBeenCalledWith(ORG, PLUGIN, "C2");
    expect(res).toEqual({ cursor: "C2", held: false });
  });

  it("HOLDS the cursor (held=true) when an update is still retrying", async () => {
    vi.mocked(processInboundUpdate)
      .mockResolvedValueOnce(true)
      .mockResolvedValueOnce(false); // retrying dispatch failure on update 2
    const p = poller([{ update_id: 1 }, { update_id: 2 }], "C2");
    const res = await runPollIteration(ORG, PLUGIN, "C1", p);
    expect(savePollCursor).not.toHaveBeenCalled();
    expect(res).toEqual({ cursor: "C1", held: true }); // re-poll from here, back off
  });

  it("advances past a dead-lettered update (processInboundUpdate returns true)", async () => {
    // A dead-letter is 'consumed' from the cursor's view — processInboundUpdate
    // returns true even though dispatch ultimately failed, so the cursor moves on.
    vi.mocked(processInboundUpdate).mockResolvedValue(true);
    const p = poller([{ update_id: 1 }], "C2");
    const res = await runPollIteration(ORG, PLUGIN, "C1", p);
    expect(savePollCursor).toHaveBeenCalledWith(ORG, PLUGIN, "C2");
    expect(res).toEqual({ cursor: "C2", held: false });
  });

  it("does not persist when the cursor is unchanged", async () => {
    const p = poller([{ update_id: 1 }], "C1");
    const res = await runPollIteration(ORG, PLUGIN, "C1", p);
    expect(savePollCursor).not.toHaveBeenCalled();
    expect(res).toEqual({ cursor: "C1", held: false });
  });

  it("does not persist on an empty batch with no new cursor", async () => {
    const p = poller([]);
    const res = await runPollIteration(ORG, PLUGIN, "C1", p);
    expect(processInboundUpdate).not.toHaveBeenCalled();
    expect(savePollCursor).not.toHaveBeenCalled();
    expect(res).toEqual({ cursor: "C1", held: false });
  });

  it("resumes from an undefined cursor on first poll and adopts the provider's", async () => {
    const p = poller([{ update_id: 1 }], "C1");
    const res = await runPollIteration(ORG, PLUGIN, undefined, p);
    expect(p.pollInbound).toHaveBeenCalledWith(PLUGIN, undefined);
    expect(savePollCursor).toHaveBeenCalledWith(ORG, PLUGIN, "C1");
    expect(res).toEqual({ cursor: "C1", held: false });
  });
});
