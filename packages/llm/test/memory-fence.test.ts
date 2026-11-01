import { describe, expect, it } from "vitest";
import { extractMemoryFences } from "../src/agent-backends/memory-fence";

describe("extractMemoryFences", () => {
  it("returns empty ops + original text when no fence is present", () => {
    const out = extractMemoryFences("Just regular prose, no memory fence here.");
    expect(out.ops).toEqual([]);
    expect(out.text).toBe("Just regular prose, no memory fence here.");
  });

  it("extracts a single save op and strips the fence from output text", () => {
    const raw = [
      "Sure, I'll remember that.",
      "",
      "```neko_memory",
      '[{ "save": { "text": "Always cite sources", "scope": "global" } }]',
      "```",
      "",
      "Anything else?",
    ].join("\n");
    const out = extractMemoryFences(raw);
    expect(out.ops).toHaveLength(1);
    expect(out.ops[0]).toEqual({
      kind: "save",
      text: "Always cite sources",
      scope: "global",
      memoryKind: undefined,
      pinned: undefined,
    });
    expect(out.text).not.toContain("neko_memory");
    expect(out.text).toContain("Sure, I'll remember that.");
    expect(out.text).toContain("Anything else?");
  });

  it("extracts multiple saves from one fence", () => {
    const raw = [
      "```neko_memory",
      JSON.stringify([
        { save: { text: "Rule one", scope: "global" } },
        { save: { text: "Rule two", scope: "thread", pinned: false } },
        { save: { text: "Rule three", kind: "preference" } },
      ]),
      "```",
    ].join("\n");
    const out = extractMemoryFences(raw);
    expect(out.ops).toHaveLength(3);
    expect(out.ops[0].text).toBe("Rule one");
    expect(out.ops[1].scope).toBe("thread");
    expect(out.ops[1].pinned).toBe(false);
    expect(out.ops[2].memoryKind).toBe("preference");
  });

  it("collects ops across multiple fences", () => {
    const raw = [
      "First note.",
      "```neko_memory",
      '[{ "save": { "text": "Memory one" } }]',
      "```",
      "Mid prose.",
      "```neko_memory",
      '[{ "save": { "text": "Memory two" } }, { "save": { "text": "Memory three" } }]',
      "```",
      "End.",
    ].join("\n");
    const out = extractMemoryFences(raw);
    expect(out.ops.map((o) => o.text)).toEqual([
      "Memory one",
      "Memory two",
      "Memory three",
    ]);
    expect(out.text).not.toContain("neko_memory");
  });

  it("silently drops malformed JSON inside the fence and still strips it", () => {
    const raw = [
      "Heads up.",
      "```neko_memory",
      "this is not valid JSON {",
      "```",
      "carry on",
    ].join("\n");
    const out = extractMemoryFences(raw);
    expect(out.ops).toEqual([]);
    expect(out.text).not.toContain("neko_memory");
    expect(out.text).toContain("Heads up.");
    expect(out.text).toContain("carry on");
  });

  it("rejects save items missing required text or shorter than the min length", () => {
    const raw = [
      "```neko_memory",
      JSON.stringify([
        { save: { scope: "global" } }, // no text at all
        { save: { text: "real memory text" } }, // valid
        { save: { text: "" } }, // empty
        { save: { text: "ab" } }, // shorter than min (3 after trim)
      ]),
      "```",
    ].join("\n");
    const out = extractMemoryFences(raw);
    expect(out.ops.map((o) => o.text)).toEqual(["real memory text"]);
  });

  it("ignores items that aren't `{ save: {...} }`", () => {
    const raw = [
      "```neko_memory",
      JSON.stringify([
        { remember: { text: "wrong wrapper" } },
        { save: "not an object" },
        { save: { text: "right shape" } },
        "string item",
        null,
      ]),
      "```",
    ].join("\n");
    const out = extractMemoryFences(raw);
    expect(out.ops).toHaveLength(1);
    expect(out.ops[0].text).toBe("right shape");
  });

  it("normalizes invalid scope to undefined (defaults applied at save site)", () => {
    const raw = [
      "```neko_memory",
      JSON.stringify([
        { save: { text: "foo", scope: "weird-scope" } },
      ]),
      "```",
    ].join("\n");
    const out = extractMemoryFences(raw);
    expect(out.ops[0].scope).toBeUndefined();
  });
});
