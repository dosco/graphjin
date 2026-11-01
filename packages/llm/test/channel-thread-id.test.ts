import { describe, expect, it } from "vitest";
import { channelThreadId } from "../src/work/store";

const UUID_V5 = /^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

describe("channelThreadId", () => {
  it("is deterministic for the same (org, channel, conversation)", () => {
    expect(channelThreadId("org-1", "slack", "D9")).toBe(
      channelThreadId("org-1", "slack", "D9"),
    );
  });

  it("differs across org, channel, or conversation key", () => {
    const base = channelThreadId("org-1", "slack", "D9");
    expect(channelThreadId("org-2", "slack", "D9")).not.toBe(base);
    expect(channelThreadId("org-1", "telegram", "D9")).not.toBe(base);
    expect(channelThreadId("org-1", "slack", "C9:171.5")).not.toBe(base);
  });

  it("produces a valid v5 UUID (accepted by a Postgres uuid column)", () => {
    expect(channelThreadId("org-1", "slack", "D9")).toMatch(UUID_V5);
  });
});
