import { describe, expect, it, vi } from "vitest";

vi.mock("@neko/db", () => ({}));
vi.mock("@/lib/work-store", () => ({ createWorkThread: vi.fn() }));
vi.mock("@neko/llm/work", () => ({ BOT_INSTRUCTIONS_MAX: 2000 }));

const { plainSummary, stepTexts } = await import("@/lib/bots");

describe("stepTexts", () => {
  it("reads each step description and skips empty ones", () => {
    expect(stepTexts([{ id: "a", description: " Fetch sales. " }, { id: "b" }, { id: "c", description: "Post in chat." }])).toEqual([
      "Fetch sales.",
      "Post in chat.",
    ]);
  });

  it("returns no steps for a value that is not a list", () => {
    expect(stepTexts(null)).toEqual([]);
  });
});

describe("plainSummary", () => {
  it("removes markdown marks and links", () => {
    expect(plainSummary("## Stock\n**12** units left. See [the report](https://x.test).")).toBe("Stock 12 units left. See the report.");
  });

  it("keeps a summary to 400 characters", () => {
    expect(plainSummary("a ".repeat(400))).toHaveLength(400);
  });
});
