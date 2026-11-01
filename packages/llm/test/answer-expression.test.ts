import { describe, expect, it } from "vitest";
import { evaluateExpression, expressionNames, parseExpression } from "../src/work/answer-expression";

const run = (source: string, scope: Record<string, number> = {}) => evaluateExpression(parseExpression(source), scope);

describe("answer expressions", () => {
  it("follows arithmetic precedence", () => {
    expect(run("2 + 3 * 4")).toBe(14);
    expect(run("(2 + 3) * 4")).toBe(20);
    expect(run("2 ^ 3 ^ 2")).toBe(512);
    expect(run("-2 ^ 2")).toBe(-4);
    expect(run("10 / 4 - 1.5")).toBe(1);
  });

  it("reads names and functions", () => {
    const scope = { road_rev: 1_000_000, price_change: 5 };
    expect(run("road_rev * (1 + price_change / 100)", scope)).toBe(1_050_000);
    expect(run("max(price_change, 0) + min(1, 2, 3)", scope)).toBe(6);
    expect(run("round(2.3456, 2)")).toBe(2.35);
    expect([...expressionNames(parseExpression("a + max(b, c * 2)"))]).toEqual(["a", "b", "c"]);
  });

  it("treats names case-insensitively", () => {
    expect(run("Price_Change * 2", { price_change: 3 })).toBe(6);
  });

  it("rejects text that is not a formula", () => {
    expect(() => parseExpression("")).toThrow("empty");
    expect(() => parseExpression("2 +")).toThrow("ends early");
    expect(() => parseExpression("alert(1)")).toThrow("unknown function alert");
    expect(() => parseExpression("a; b")).toThrow('unexpected ";"');
    expect(() => parseExpression("round(1, 2, 3)")).toThrow("round takes 1 to 2 values");
    expect(() => parseExpression("(1 + 2")).toThrow("close ( with )");
  });

  it("gives NaN for a name the scope does not hold", () => {
    expect(run("missing + 1")).toBeNaN();
  });
});
