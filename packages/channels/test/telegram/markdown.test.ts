import { describe, it, expect } from "vitest";
import { escapeHtml, inlineBlock, mdToTelegramHtml } from "../../src/telegram/markdown";

describe("mdToTelegramHtml", () => {
  it("converts bold, italic, inline code, strikethrough, and links", () => {
    expect(
      mdToTelegramHtml("**bold** and *italic* and `x=1` and ~~no~~ and [docs](https://e.com)"),
    ).toBe(
      '<b>bold</b> and <i>italic</i> and <code>x=1</code> and <s>no</s> and <a href="https://e.com">docs</a>',
    );
  });

  it("preserves numbers in the text — regression: '$4.7M' is not a stash index", () => {
    expect(mdToTelegramHtml("Revenue was **$4.7M** in Q3 across 12 regions.")).toBe(
      "Revenue was <b>$4.7M</b> in Q3 across 12 regions.",
    );
  });

  it("escapes < > & and never emits &apos;", () => {
    const html = mdToTelegramHtml("status < 5 && id > 0 won't break");
    expect(html).toContain("status &lt; 5 &amp;&amp; id &gt; 0");
    expect(html).not.toContain("&apos;");
  });

  it("does not italicize snake_case identifiers", () => {
    expect(mdToTelegramHtml("the sales_order_header table")).toBe("the sales_order_header table");
  });

  it("degrades headings to bold and list items to bullets", () => {
    const html = mdToTelegramHtml("## Summary\n- first\n- second\n1. third");
    expect(html).toContain("<b>Summary</b>");
    expect(html).toContain("• first");
    expect(html).toContain("• third");
    expect(html).not.toContain("##");
  });

  it("renders blockquotes and fenced code blocks, escaping HTML inside", () => {
    const html = mdToTelegramHtml("> a quote\n\n```sql\nselect 1 < 2\n```");
    expect(html).toContain("<blockquote>a quote</blockquote>");
    expect(html).toContain('<pre><code class="language-sql">select 1 &lt; 2</code></pre>');
  });

  it("escapes HTML inside an inline code span", () => {
    expect(mdToTelegramHtml("`a < b`")).toBe("<code>a &lt; b</code>");
  });
});

describe("escapeHtml", () => {
  it("escapes only the three structural chars", () => {
    expect(escapeHtml("a < b & c > d 'e' \"f\"")).toBe("a &lt; b &amp; c &gt; d 'e' \"f\"");
  });
});

describe("inlineBlock — never emits a block tag (safe inside a blockquote)", () => {
  it("flattens code fences, keeps headings/lists as bold/bullets", () => {
    const html = inlineBlock("# Head\n```\ncode\n```\n- item\n> q");
    expect(html).not.toContain("<pre");
    expect(html).not.toContain("<blockquote");
    expect(html).toContain("<b>Head</b>");
    expect(html).toContain("• item");
  });
});
