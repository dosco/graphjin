import { describe, it, expect } from "vitest";
import { TELEGRAM_PROFILE, type SurfaceMessage } from "@neko/interaction";
import { renderSurface } from "../../src/telegram/surface";

const surfaceOf = (components: Array<Record<string, unknown>>): SurfaceMessage[] => [
  { version: "v0.9", createSurface: { surfaceId: "s1", catalogId: "urn:app:catalog:briefing:v1" } },
  { version: "v0.9", updateComponents: { surfaceId: "s1", components } },
];

describe("renderSurface — A2UI surface → Telegram HTML", () => {
  it("renders an Answer with title, markdown body, metric card, and a monospace table", () => {
    const html = renderSurface(
      surfaceOf([
        { id: "root", component: "Answer", eyebrow: "SALES", title: "Top territories", subtitle: "TTM", children: ["intro", "c1", "tbl"] },
        { id: "intro", component: "Markdown", text: "**Southwest** leads." },
        { id: "c1", component: "MetricCard", mood: "good", metric: "$4.7M", label: "Revenue", detail: "Up 12%." },
        {
          id: "tbl",
          component: "Table",
          columns: [
            { key: "r", label: "Region" },
            { key: "v", label: "Rev", align: "right" },
          ],
          rows: [
            { r: "SW", v: "$4.7M" },
            { r: "NW", v: "$4.0M" },
          ],
        },
      ]),
      TELEGRAM_PROFILE,
    ).html;
    expect(html).toContain("<i>SALES</i>");
    expect(html).toContain("<b>Top territories</b>");
    expect(html).toContain("TTM");
    expect(html).toContain("<b>Southwest</b> leads.");
    expect(html).toContain("✅ <b>$4.7M</b> — Revenue");
    expect(html).toContain("Up 12%.");
    expect(html).toContain("<pre>");
    expect(html).toContain("Region");
    expect(html).toContain("─");
  });

  it("renders a Callout as a mood blockquote with no nested block tags", () => {
    const html = renderSurface(
      surfaceOf([
        { id: "root", component: "Answer", title: "T", children: ["ca"] },
        { id: "ca", component: "Callout", mood: "act", title: "Takeaway", text: "**Critical**: act now." },
      ]),
      TELEGRAM_PROFILE,
    ).html;
    expect(html).toContain("<blockquote>");
    expect(html).toContain("🚨 <b>Takeaway</b>");
    expect(html).toContain("<b>Critical</b>: act now.");
    const inside = html.slice(html.indexOf("<blockquote>"), html.indexOf("</blockquote>"));
    expect(inside).not.toContain("<pre");
  });

  it("surfaces Choice options as follow-ups, never as broken buttons", () => {
    const { html, followups } = renderSurface(
      surfaceOf([
        { id: "root", component: "Answer", title: "T", children: ["ch"] },
        {
          id: "ch",
          component: "Choice",
          options: [
            { label: "Drill in", prompt: "Break down X" },
            { label: "Trend", prompt: "Show the trend" },
          ],
        },
      ]),
      TELEGRAM_PROFILE,
    );
    expect(followups).toEqual(["Drill in", "Trend"]);
    expect(html).not.toContain("callback");
  });

  it("falls back to all components when the Answer root omits children", () => {
    const html = renderSurface(
      surfaceOf([
        { id: "root", component: "Answer", title: "T" },
        { id: "m1", component: "Markdown", text: "First." },
        { id: "m2", component: "Markdown", text: "Second." },
      ]),
      TELEGRAM_PROFILE,
    ).html;
    expect(html).toContain("First.");
    expect(html).toContain("Second.");
  });

  it("renders a labeled Divider", () => {
    const html = renderSurface(
      surfaceOf([
        { id: "root", component: "Answer", title: "T", children: ["d"] },
        { id: "d", component: "Divider", label: "Next steps" },
      ]),
      TELEGRAM_PROFILE,
    ).html;
    expect(html).toContain("— Next steps —");
  });
});
