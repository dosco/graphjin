import { describe, expect, it, vi } from "vitest";

vi.mock("@neko/db", () => ({}));
vi.mock("@/lib/work-store", () => ({ createWorkThread: vi.fn() }));
vi.mock("@neko/llm/work", () => ({ BOT_INSTRUCTIONS_MAX: 2000 }));

const { cleanBotPatch } = await import("@/lib/bots");

describe("cleanBotPatch", () => {
  it("accepts a new name and trims it to 40 characters", () => {
    expect(cleanBotPatch({ name: "  Stock watcher  " })).toEqual({ name: "Stock watcher" });
    expect((cleanBotPatch({ name: "x".repeat(60) }) as { name: string }).name).toHaveLength(40);
  });

  it("refuses an empty name", () => {
    expect(cleanBotPatch({ name: "   " })).toBe("Give the bot a name.");
  });

  it("changes the look without touching the name", () => {
    expect(cleanBotPatch({ coat: "mint", pattern: "tabby" })).toEqual({ coat: "mint", pattern: "tabby" });
  });

  it("refuses an unknown coat or pattern", () => {
    expect(cleanBotPatch({ coat: "plaid" })).toBe("Pick a coat from the list.");
    expect(cleanBotPatch({ pattern: "spots" })).toBe("Pick a pattern from the list.");
  });

  it("refuses an empty patch", () => {
    expect(cleanBotPatch({})).toBe("Nothing to change.");
  });
});
