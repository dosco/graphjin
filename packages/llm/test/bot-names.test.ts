import { describe, expect, it } from "vitest";
import { cleanBotName } from "../src/work/bot-names";

describe("cleanBotName", () => {
  it("strips quotes and end punctuation", () => {
    expect(cleanBotName(' "Stock watcher." ')).toBe("Stock watcher");
  });

  it("keeps an apostrophe inside the name", () => {
    expect(cleanBotName("'Sam's orders'")).toBe("Sam's orders");
  });

  it("keeps a name to 24 characters", () => {
    expect(cleanBotName("Weekly wholesale customer revenue checker")).toBe("Weekly wholesale custome");
  });

  it("returns empty text for an empty answer", () => {
    expect(cleanBotName("  ")).toBe("");
  });
});
