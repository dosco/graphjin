import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import {
  formatElapsed,
  progressPreview,
  ProviderProgress,
  WorkTrail,
} from "../src/app/(work)/work/work-progress";

const run = {
  id: "run-1",
  backend: "hermes" as const,
  status: "completed",
  error: null,
  createdAt: "2026-10-05T09:00:00Z",
  finishedAt: "2026-10-05T09:03:12Z",
};

describe("work reasoning", () => {
  it("previews a note by its first line without markdown markers", () => {
    expect(progressPreview("**Locating the view**\n\nI check the catalog.")).toBe("Locating the view");
    expect(progressPreview("I should load the skill first.")).toBe("I should load the skill first.");
    expect(progressPreview(`# ${"x".repeat(200)}`)).toHaveLength(140);
  });

  it("formats elapsed time in seconds, minutes and hours", () => {
    expect(formatElapsed(45_000)).toBe("45 s");
    expect(formatElapsed(192_000)).toBe("3 min 12 s");
    expect(formatElapsed(180_000)).toBe("3 min");
    expect(formatElapsed(3_900_000)).toBe("1 h 5 min");
  });

  it("renders a Gemini note with headings as one note, the same as a plain note", () => {
    const gemini = renderToStaticMarkup(createElement(ProviderProgress, { content: "**Locating the view**\n\nI check the catalog.\n\n**Checking rows**\n\nTwo rows.", live: false }));
    const plain = renderToStaticMarkup(createElement(ProviderProgress, { content: "I check the catalog.", live: false }));
    for (const html of [gemini, plain]) {
      expect(html).toContain("work-progress-summary");
      expect(html).not.toContain("<details");
    }
    expect(gemini).toContain("Checking rows");
  });

  it("collapses an earlier note to its preview", () => {
    const html = renderToStaticMarkup(createElement(ProviderProgress, { content: "**Locating the view**\n\nDetail.", live: false, collapsed: true }));
    expect(html).toContain("<details");
    expect(html).not.toMatch(/<details[^>]*\sopen[\s>=]/);
    expect(html).toContain("Locating the view");
  });

  it("folds a finished trail into one row with its duration and counts", () => {
    const html = renderToStaticMarkup(createElement(WorkTrail, {
      run,
      items: [
        { kind: "progress", id: "p1", content: "a" },
        { kind: "interim", id: "i1", content: "b" },
        { kind: "tools", tools: [{}, {}, {}] as never },
      ],
    }, createElement("p", null, "trail")));
    expect(html).toContain("Worked for 3 min 12 s");
    expect(html).toContain("2 notes, 3 tool calls");
  });
});
