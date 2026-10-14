import { describe, expect, it } from "vitest";
import { cappedToolResult } from "../src/work/store";

describe("cappedToolResult", () => {
  it("keeps a small tool result as it is", () => {
    const event = { type: "tool_end" as const, id: "t1", result: { ok: true } };
    expect(cappedToolResult(event)).toBe(event);
  });

  it("cuts a large tool result and says how much it left out", () => {
    const event = { type: "tool_end" as const, id: "t1", result: { cards: "x".repeat(50_000) } };
    const capped = cappedToolResult(event);
    expect(capped.type).toBe("tool_end");
    const result = (capped as { result: string }).result;
    expect(result.startsWith('{"cards":"xxx')).toBe(true);
    expect(result).toMatch(/… 18,012 more characters not shown$/);
  });

  it("leaves other events alone", () => {
    const event = { type: "message" as const, role: "assistant" as const, content: "x".repeat(50_000) };
    expect(cappedToolResult(event as never)).toBe(event);
  });
});
