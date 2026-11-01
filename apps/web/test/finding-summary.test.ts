import { describe, expect, it } from "vitest";
import { summarizeFinding } from "@/lib/finding-summary";

describe("summarizeFinding", () => {
  it("picks the summary section and caps it at three points", () => {
    const md = "# Report\n\n## Executive Summary\n- a\n- b\n- c\n- d\n\n## Detail\nlong text";
    expect(summarizeFinding(md)).toEqual({ excerpt: "- a\n- b\n- c", truncated: true });
  });

  it("uses the first paragraph when no summary heading exists", () => {
    expect(summarizeFinding("Stock is low on 3 SKUs.")).toEqual({
      excerpt: "Stock is low on 3 SKUs.",
      truncated: false,
    });
  });

  it("shortens a long point at a sentence and drops inline marks", () => {
    const long = `**Status:** ${"word ".repeat(30)}end. ${"more ".repeat(40)}`;
    const { excerpt, truncated } = summarizeFinding(`- ${long}`);
    expect(truncated).toBe(true);
    expect(excerpt).not.toContain("**");
    expect(excerpt.endsWith("…")).toBe(true);
  });
});
