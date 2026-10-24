import { describe, expect, it } from "vitest";
import { extractSurfaceMessages } from "../src/agent-backends/surface";
import { coerceGeneratedSurfaceMessages, extractCardFences } from "../src/agent-backends/surface";
import { outsideFenceText } from "../src/agent-backends/output";

describe("extractSurfaceMessages", () => {
  it("returns text and empty messages when no fence is present", () => {
    const result = extractSurfaceMessages("Just some prose, no cards.");
    expect(result.text).toBe("Just some prose, no cards.");
    expect(result.messages).toEqual([]);
  });

  it("trims surrounding whitespace when no fence is present", () => {
    const result = extractSurfaceMessages("\n\n  hello  \n\n");
    expect(result.text).toBe("hello");
  });

  it("extracts messages and strips the fence from text", () => {
    const raw = [
      "Here are the KPIs:",
      "```neko_a2ui",
      JSON.stringify([
        {
          version: "v0.9",
          createSurface: { surfaceId: "kpis", catalogId: "urn:app:catalog:briefing:v1" },
        },
      ]),
      "```",
      "Anything else?",
    ].join("\n");
    const result = extractSurfaceMessages(raw);
    expect(result.messages).toHaveLength(1);
    expect(result.messages[0].version).toBe("v0.9");
    expect(result.text).toContain("Here are the KPIs:");
    expect(result.text).toContain("Anything else?");
    expect(result.text).not.toContain("neko_a2ui");
    expect(result.text).not.toContain("v0.9");
  });

  it("wraps malformed fence body as a synthetic Markdown surface", () => {
    const raw = "Prose. ```neko_a2ui\n<Markdown>hello there</Markdown>\n``` more prose.";
    const result = extractSurfaceMessages(raw);
    expect(result.text).toContain("Prose.");
    expect(result.text).toContain("more prose.");
    expect(result.messages).toHaveLength(1);
    expect(result.messages[0].version).toBe("v1.0");
    const components = (result.messages[0] as unknown as {
      createSurface: { components: Array<{ component: string; text?: string }> };
    }).createSurface.components;
    expect(components[0].component).toBe("Markdown");
    expect(components[0].text).toBe("hello there");
  });

  it("wraps non-array JSON as a synthetic Markdown surface (preserves the body)", () => {
    const raw = '```neko_a2ui\n{"version": "v0.9"}\n```';
    const result = extractSurfaceMessages(raw);
    expect(result.messages).toHaveLength(1);
    const components = (result.messages[0] as unknown as {
      createSurface: { components: Array<{ component: string; text?: string }> };
    }).createSurface.components;
    expect(components[0].component).toBe("Markdown");
    expect(components[0].text).toContain("v0.9");
  });

  it("returns empty messages when fence body is empty after JSX strip", () => {
    const raw = "```neko_a2ui\n<Markdown></Markdown>\n```";
    const result = extractSurfaceMessages(raw);
    expect(result.messages).toEqual([]);
  });

  it("matches the fence regardless of casing", () => {
    const raw =
      '```NEKO_A2UI\n[{"version":"v0.9","createSurface":{"surfaceId":"s1","catalogId":"urn:app:catalog:briefing:v1"}}]\n```';
    const result = extractSurfaceMessages(raw);
    expect(result.messages).toHaveLength(1);
  });

  it("accepts A2UI v1.0 surface messages", () => {
    const raw =
      '```neko_a2ui\n[{"version":"v1.0","createSurface":{"surfaceId":"s2","catalogId":"urn:openneko:catalog:work:v2","components":[{"id":"root","component":"Text","text":"Ready"}]}}]\n```';
    const result = extractSurfaceMessages(raw);
    expect(result.messages).toHaveLength(1);
    expect(result.messages[0].version).toBe("v1.0");
  });
});

describe("coerceGeneratedSurfaceMessages", () => {
  it("accepts well-formed v1.0 components", () => {
    expect(
      coerceGeneratedSurfaceMessages([
        {
          version: "v1.0",
          createSurface: {
            surfaceId: "s1",
            catalogId: "urn:openneko:catalog:work:v2",
            components: [{ id: "root", component: "Text", text: "Ready" }],
          },
        },
      ]),
    ).toHaveLength(1);
  });

  it("rejects reader-only v0.9 and invalid component shorthand", () => {
    expect(
      coerceGeneratedSurfaceMessages([
        { version: "v0.9", createSurface: { surfaceId: "old", catalogId: "legacy" } },
        {
          version: "v1.0",
          createSurface: {
            surfaceId: "bad",
            catalogId: "urn:openneko:catalog:work:v2",
            components: [1, 2, 3],
          },
        },
      ]),
    ).toEqual([]);
  });
});

describe("extractCardFences", () => {
  const fence = '```neko_cards\n{"title": "Shortfall", "table": "Material | Short\\nFoil | 40\\nPrepreg | 31", "chart": "bar", "chartColumn": "Short"}\n```';

  it("turns a neko_cards fence into a surface and removes it from the text", () => {
    const out = extractCardFences(`Two materials are short.\n\n${fence}\n\nFoil is never swapped.`);
    expect(out.issues).toEqual([]);
    expect(out.messages.length).toBeGreaterThan(0);
    expect(out.text).toBe("Two materials are short.\n\nFoil is never swapped.");
  });

  it("drops a broken fence and reports why", () => {
    const out = extractCardFences("Answer.\n```neko_cards\n{not json}\n```");
    expect(out.messages).toEqual([]);
    expect(out.issues).toEqual(["neko_cards fence is not JSON"]);
    expect(out.text).toBe("Answer.");
  });

  it("leaves text without a fence untouched", () => {
    expect(extractCardFences("Plain answer.\n").text).toBe("Plain answer.\n");
  });

  it("keeps the fence out of the streamed text", () => {
    expect(outsideFenceText(`Two materials are short.\n${fence}\nDone.`)).toBe("Two materials are short.\n\nDone.");
    expect(outsideFenceText("Two materials are short.\n```neko_ca")).toBe("Two materials are short.\n");
  });
});
